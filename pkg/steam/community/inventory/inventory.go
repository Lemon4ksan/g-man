// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package inventory

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/lemon4ksan/aoni"
	"github.com/lemon4ksan/miyako/generic"

	"github.com/lemon4ksan/g-man/pkg/steam/community"
	"github.com/lemon4ksan/g-man/pkg/steam/id"
)

var (
	rxAppContextData   = regexp.MustCompile(`(?s)var g_rgAppContextData\s*=\s*(.*?);`)
	rxHistoryInventory = regexp.MustCompile(`(?s)var g_rgHistoryInventory\s*=\s*(.*?);`)
	rxHoverScript      = regexp.MustCompile(
		`HistoryPageCreateItemHover\(\s*'\s*([^']+)\s*'\s*,\s*(\d+)\s*,\s*['"]([^'"]+)['"]\s*,\s*['"]([^'"]+)['"]\s*,\s*['"]([^'"]+)['"]\s*\)`,
	)
	rxTimestamp       = regexp.MustCompile(`(\d+):(\d+)\s*(am|pm|AM|PM)`)
	rxPaginationTime  = regexp.MustCompile(`after_time=(\d+)`)
	rxPaginationTrade = regexp.MustCompile(`after_trade=(\d+)`)
)

// GetUserInventoryContents recursively parses user inventory using community requester
// and returns the list of items and currencies with their total count.
//
// If language is empty, it automatically defaults to "english".
// It returns an error if the underlying WebAPI request fails or if Steam returns
// an unsuccessful status payload.
func GetUserInventoryContents(
	ctx context.Context,
	client community.Requester,
	steamID uint64,
	appID uint32,
	contextID int64,
	tradableOnly bool,
	language string,
) ([]CEconItem, []CEconItem, int, error) {
	language = generic.Coalesce(language, "english")

	var (
		inventory    []CEconItem
		currency     []CEconItem
		startAssetID string
		totalCount   int
	)

	pos := 1

	for {
		page, err := fetchInventoryPage(ctx, client, steamID, appID, contextID, startAssetID, language)
		if err != nil {
			return nil, nil, 0, err
		}

		if page.TotalCount == 0 || len(page.Assets) == 0 {
			return inventory, currency, page.TotalCount, nil
		}

		descMap := generic.IndexBy(page.Descriptions, func(d Description) string {
			return fmt.Sprintf("%s_%s", d.ClassID, d.InstanceID)
		})

		pageInventory, pageCurrency, newPos := processAssets(page.Assets, descMap, tradableOnly, pos)

		pos = newPos

		inventory = append(inventory, pageInventory...)
		currency = append(currency, pageCurrency...)
		totalCount = page.TotalCount

		if !page.MoreItems {
			break
		}

		startAssetID = page.LastAssetID
	}

	return inventory, currency, totalCount, nil
}

// GetUserInventoryContexts retrieves the application and context details for a user's inventory.
func GetUserInventoryContexts(
	ctx context.Context,
	client community.Requester,
	userID uint64,
) (map[string]*AppContext, error) {
	bodyBytes, err := fetchInventoryPageHTML(ctx, client, userID)
	if err != nil {
		return nil, err
	}

	if err := verifyInventoryPrivacy(bodyBytes); err != nil {
		return nil, err
	}

	cleanedJSON, err := extractAppContextJSON(bodyBytes)
	if err != nil {
		return nil, err
	}

	if bytes.Equal(cleanedJSON, []byte("[]")) {
		return make(map[string]*AppContext), nil
	}

	var data map[string]*AppContext
	if err := json.Unmarshal(cleanedJSON, &data); err != nil {
		return nil, fmt.Errorf("inventory: failed to parse context data JSON: %w", err)
	}

	return data, nil
}

// TradeDirection defines the navigation direction of pagination.
type TradeDirection string

// Direction constants define the valid directions for pagination.
const (
	DirectionPast   TradeDirection = "past"
	DirectionFuture TradeDirection = "future"
)

// HistoryOptions represents search parameters for fetching inventory history.
type HistoryOptions struct {
	StartTime  *time.Time
	StartTrade *uint64
	Direction  TradeDirection
}

// GetInventoryHistory fetches and parses the Steam inventory history for the specified user.
func GetInventoryHistory(
	ctx context.Context,
	client community.Requester,
	steamID id.ID,
	opts HistoryOptions,
) (*TradeHistoryResult, error) {
	params := struct {
		Language   string     `json:"l"`
		AfterTime  *time.Time `json:"after_time,omitempty"`
		AfterTrade *uint64    `json:"after_trade,omitempty"`
		Direction  int        `json:"prev"`
	}{"english", opts.StartTime, opts.StartTrade, 0}

	if opts.Direction == DirectionFuture {
		params.Direction = 1
	}

	html, err := community.GetHTML(ctx, client, "profiles/{steamID}/inventoryhistory",
		aoni.WithVar("steamID", steamID),
		aoni.WithQuery(params),
	)
	if err != nil {
		return nil, fmt.Errorf("history: failed to fetch inventory history page: %w", err)
	}
	defer html.Close()

	bodyBytes, err := io.ReadAll(html)
	if err != nil {
		return nil, err
	}

	parser, err := NewHistoryParser(bodyBytes)
	if err != nil {
		return nil, err
	}

	return parser.Parse()
}

// HistoryParser encapsulates all HTML/JS parsing logic for a Steam Trade History page.
type HistoryParser struct {
	rawHTML []byte
	doc     *goquery.Document
}

// NewHistoryParser initializes a HistoryParser with raw HTML content.
func NewHistoryParser(rawHTML []byte) (*HistoryParser, error) {
	doc, err := goquery.NewDocumentFromReader(bytes.NewReader(rawHTML))
	if err != nil {
		return nil, fmt.Errorf("history: failed to parse HTML document: %w", err)
	}

	return &HistoryParser{
		rawHTML: rawHTML,
		doc:     doc,
	}, nil
}

// Parse extracts trade records, asset descriptions, hover elements, and pagination links.
func (p *HistoryParser) Parse() (*TradeHistoryResult, error) {
	if p.doc.Find(".inventory_history_pagingrow").Length() == 0 {
		return nil, errors.New("history: malformed page (paging row not found)")
	}

	inventory, err := p.extractHistoryInventory()
	if err != nil {
		return nil, err
	}

	hovers := p.extractHovers()

	result := &TradeHistoryResult{
		Trades: p.parseRows(inventory, hovers),
	}

	p.parsePagination(result)

	return result, nil
}

func (p *HistoryParser) extractHistoryInventory() (map[string]map[string]map[string]EconItem, error) {
	match := rxHistoryInventory.FindSubmatch(p.rawHTML)
	if len(match) != 2 {
		return nil, errors.New("history: malformed page (g_rgHistoryInventory not found)")
	}

	var inventory map[string]map[string]map[string]EconItem
	if err := json.Unmarshal(match[1], &inventory); err != nil {
		return nil, fmt.Errorf("history: failed to parse history inventory JSON: %w", err)
	}

	return inventory, nil
}

func (p *HistoryParser) extractHovers() map[string]hoverInfo {
	hoverMap := make(map[string]hoverInfo)
	hovers := rxHoverScript.FindAllSubmatch(p.rawHTML, -1)

	for _, hover := range hovers {
		if len(hover) != 6 {
			continue
		}

		elementID := string(hover[1])
		amount, _ := strconv.Atoi(string(hover[5]))
		hoverMap[elementID] = hoverInfo{
			AppID:     string(hover[2]),
			ContextID: string(hover[3]),
			AssetID:   string(hover[4]),
			Amount:    amount,
		}
	}

	return hoverMap
}

func (p *HistoryParser) parsePagination(result *TradeHistoryResult) {
	p.doc.Find(".inventory_history_nextbtn .pagebtn:not(.disabled)").Each(func(_ int, buttonSel *goquery.Selection) {
		href, exists := buttonSel.Attr("href")
		if !exists {
			return
		}

		p.extractPaginationParams(href, result)
	})
}

func (p *HistoryParser) extractPaginationParams(href string, result *TradeHistoryResult) {
	timeMatch := rxPaginationTime.FindStringSubmatch(href)

	tradeMatch := rxPaginationTrade.FindStringSubmatch(href)
	if len(timeMatch) != 2 || len(tradeMatch) != 2 {
		return
	}

	unixTime, err := strconv.ParseInt(timeMatch[1], 10, 64)
	if err != nil {
		return
	}

	timestamp := time.Unix(unixTime, 0).UTC()

	tradeID, err := strconv.ParseUint(tradeMatch[1], 10, 64)
	if err != nil {
		return
	}

	if strings.Contains(href, "prev=1") {
		result.FirstTradeTime = &timestamp
		result.FirstTradeID = &tradeID
	} else {
		result.LastTradeTime = &timestamp
		result.LastTradeID = &tradeID
	}
}

func (p *HistoryParser) parseRows(
	historyInventory map[string]map[string]map[string]EconItem,
	hoverMap map[string]hoverInfo,
) []TradeHistoryRow {
	var trades []TradeHistoryRow

	p.doc.Find(".tradehistoryrow").Each(func(_ int, rowSel *goquery.Selection) {
		row := TradeHistoryRow{
			ItemsReceived: make([]EconItem, 0),
			ItemsGiven:    make([]EconItem, 0),
		}

		row.OnHold = p.parseRowHoldStatus(rowSel)
		row.Date = p.parseRowTimestamp(rowSel)

		partnerAnchor := rowSel.Find(".tradehistory_event_description a")
		row.PartnerName = partnerAnchor.Text()

		if profileLink, exists := partnerAnchor.Attr("href"); exists {
			p.parsePartnerProfile(profileLink, &row)
		}

		rowSel.Find(".history_item").Each(func(_ int, itemSel *goquery.Selection) {
			p.parseHistoryItem(itemSel, historyInventory, hoverMap, &row)
		})

		trades = append(trades, row)
	})

	return trades
}

func (p *HistoryParser) parseRowHoldStatus(rowSel *goquery.Selection) bool {
	holdText := rowSel.Find("span:nth-of-type(2)").Text()
	return strings.Contains(strings.ToLower(holdText), "trade on hold")
}

func (p *HistoryParser) parseRowTimestamp(rowSel *goquery.Selection) time.Time {
	timeText := rowSel.Find(".tradehistory_timestamp").Text()

	time24, err := convertTimeTo24h(timeText)
	if err != nil {
		return time.Time{}
	}

	dateText := rowSel.Find(".tradehistory_date").Text()

	parsedTime, err := parseTradeDate(dateText, time24)
	if err != nil {
		return time.Time{}
	}

	return parsedTime
}

func (p *HistoryParser) parseHistoryItem(
	itemSel *goquery.Selection,
	inventory map[string]map[string]map[string]EconItem,
	hoverMap map[string]hoverInfo,
	row *TradeHistoryRow,
) {
	elementID, exists := itemSel.Attr("id")
	if !exists {
		return
	}

	hover, exists := hoverMap[elementID]
	if !exists {
		return
	}

	itemDetail, exists := lookupInventoryItem(inventory, hover)
	if !exists {
		return
	}

	itemDetail.Amount = hover.Amount

	if strings.Contains(elementID, "received") {
		row.ItemsReceived = append(row.ItemsReceived, itemDetail)
	} else {
		row.ItemsGiven = append(row.ItemsGiven, itemDetail)
	}
}

func (p *HistoryParser) parsePartnerProfile(profileLink string, row *TradeHistoryRow) {
	parts := strings.Split(strings.TrimRight(profileLink, "/"), "/")
	if len(parts) == 0 {
		return
	}

	lastPart := parts[len(parts)-1]

	if strings.Contains(profileLink, "/profiles/") {
		sidVal, _ := strconv.ParseUint(lastPart, 10, 64)
		row.PartnerSteamID = id.ID(sidVal)
	} else {
		row.PartnerVanityURL = lastPart
	}
}

type inventoryPageRequest struct {
	Language     string `url:"l"`
	Count        int    `url:"count"`
	StartAssetID string `url:"start_assetid,omitempty"`
}

func fetchInventoryPage(
	ctx context.Context,
	client community.Requester,
	steamID uint64,
	appID uint32,
	contextID int64,
	startAssetID string,
	language string,
) (*inventoryResponse, error) {
	req := inventoryPageRequest{
		Language:     language,
		Count:        1000,
		StartAssetID: startAssetID,
	}

	resp, err := community.Get[inventoryResponse](
		ctx, client, "inventory/{steamID}/{appID}/{contextID}", req,
		aoni.WithVars("steamID", steamID, "appID", appID, "contextID", contextID),
		aoni.WithHeader("Referer", fmt.Sprintf("https://steamcommunity.com/profiles/%d/inventory", steamID)),
	)
	if err != nil {
		return nil, err
	}

	if !resp.Success {
		return nil, fmt.Errorf("steam error: %s", resp.Error)
	}

	return resp, nil
}

func processAssets(
	assets []Asset,
	descMap map[string]Description,
	tradableOnly bool,
	startPos int,
) ([]CEconItem, []CEconItem, int) {
	var (
		inventory []CEconItem
		currency  []CEconItem
	)

	pos := startPos

	for _, asset := range assets {
		key := fmt.Sprintf("%s_%s", asset.ClassID, asset.InstanceID)

		description, exists := descMap[key]
		if !exists {
			continue
		}

		if tradableOnly && description.Tradable == 0 {
			continue
		}

		asset.Pos = pos
		pos++

		item := CEconItem{
			Asset:       asset,
			Description: description,
		}

		if asset.CurrencyID != "" {
			currency = append(currency, item)
		} else {
			inventory = append(inventory, item)
		}
	}

	return inventory, currency, pos
}

func fetchInventoryPageHTML(ctx context.Context, client community.Requester, userID uint64) ([]byte, error) {
	html, err := community.GetHTML(ctx, client, "profiles/{userID}/inventory",
		aoni.WithVar("userID", userID),
	)
	if err != nil {
		return nil, fmt.Errorf("inventory: failed to fetch inventory page: %w", err)
	}
	defer html.Close()

	return io.ReadAll(html)
}

func verifyInventoryPrivacy(bodyBytes []byte) error {
	if bytes.Contains(bodyBytes, []byte("This profile is private.")) {
		return errors.New("inventory: profile is private")
	}

	if bytes.Contains(bodyBytes, []byte("The inventory is currently private.")) ||
		bytes.Contains(bodyBytes, []byte("inventory is currently private")) {
		return errors.New("inventory: inventory is private")
	}

	return nil
}

func extractAppContextJSON(bodyBytes []byte) ([]byte, error) {
	match := rxAppContextData.FindSubmatch(bodyBytes)
	if len(match) != 2 {
		return nil, errors.New("inventory: malformed page (g_rgAppContextData not found)")
	}

	return bytes.TrimSpace(match[1]), nil
}

func lookupInventoryItem(
	inventory map[string]map[string]map[string]EconItem,
	hover hoverInfo,
) (EconItem, bool) {
	appMap, exists := inventory[hover.AppID]
	if !exists {
		return EconItem{}, false
	}

	contextMap, exists := appMap[hover.ContextID]
	if !exists {
		return EconItem{}, false
	}

	item, exists := contextMap[hover.AssetID]

	return item, exists
}

func convertTimeTo24h(timestamp string) (string, error) {
	match := rxTimestamp.FindStringSubmatch(timestamp)
	if len(match) != 4 {
		return "", fmt.Errorf("invalid timestamp format: %s", timestamp)
	}

	hour, _ := strconv.Atoi(match[1])
	minute, _ := strconv.Atoi(match[2])
	period := strings.ToLower(match[3])

	if hour == 12 && period == "am" {
		hour = 0
	} else if hour < 12 && period == "pm" {
		hour += 12
	}

	return fmt.Sprintf("%02d:%02d:00", hour, minute), nil
}

func parseTradeDate(dateText, timeText string) (time.Time, error) {
	dateText = cleanWhitespace(dateText)
	timeText = cleanWhitespace(timeText)

	if !strings.Contains(dateText, ",") {
		currentYear := time.Now().UTC().Year()
		dateText = fmt.Sprintf("%s, %d", dateText, currentYear)
	}

	combined := fmt.Sprintf("%s %s UTC", dateText, timeText)

	layouts := []string{
		"2 Jan, 2006 15:04:05 MST",
		"Jan 2, 2006 15:04:05 MST",
	}

	for _, layout := range layouts {
		if t, err := time.Parse(layout, combined); err == nil {
			return t, nil
		}
	}

	cleanCombined := strings.ReplaceAll(combined, ",", "")
	if t, err := time.Parse("2 Jan 2006 15:04:05 MST", cleanCombined); err == nil {
		return t, nil
	}

	return time.Time{}, fmt.Errorf("could not parse date %q", combined)
}

func cleanWhitespace(input string) string {
	trimmed := strings.TrimSpace(input)
	return strings.ReplaceAll(trimmed, "  ", " ")
}

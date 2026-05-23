// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package inventory allows retrieving user inventory using community requester.
package inventory

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"

	"github.com/lemon4ksan/g-man/pkg/steam/api"
	"github.com/lemon4ksan/g-man/pkg/steam/community"
	"github.com/lemon4ksan/g-man/pkg/steam/id"
)

// GetUserInventoryContents recursively parses user inventory using
// community requester and returns the list of items and currencies
// with their total count.
func GetUserInventoryContents(
	ctx context.Context,
	c community.Requester,
	userID uint64,
	appID uint32,
	contextID int64,
	tradableOnly bool,
	language string,
) ([]CEconItem, []CEconItem, int, error) {
	if language == "" {
		language = "english"
	}

	var (
		inventory    []CEconItem
		currency     []CEconItem
		startAssetID string
		totalCount   int
	)

	pos := 1

	for {
		path := fmt.Sprintf("inventory/%d/%d/%d", userID, appID, contextID)

		req := struct {
			Language     string `url:"l"`
			Count        int    `url:"count"`
			StartAssetID string `url:"start_assetid,omitempty"`
		}{Language: language, Count: 1000, StartAssetID: startAssetID}

		resp, err := community.Get[inventoryResponse](ctx, c, path, req, api.WithHeader(
			"Referer", fmt.Sprintf("https://steamcommunity.com/profiles/%d/inventory", userID)),
		)
		if err != nil {
			return nil, nil, 0, err
		}

		if !resp.Success {
			return nil, nil, 0, fmt.Errorf("steam error: %s", resp.Error)
		}

		if resp.TotalCount == 0 || (len(resp.Assets) == 0) {
			return inventory, currency, resp.TotalCount, nil
		}

		descMap := make(map[string]*Description)
		for _, d := range resp.Descriptions {
			key := fmt.Sprintf("%s_%s", d.ClassID, d.InstanceID)
			descMap[key] = &d
		}

		for _, asset := range resp.Assets {
			key := fmt.Sprintf("%s_%s", asset.ClassID, asset.InstanceID)
			description := descMap[key]

			if tradableOnly && (description == nil || description.Tradable == 0) {
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

		totalCount = resp.TotalCount

		if !resp.MoreItems {
			break
		}

		startAssetID = resp.LastAssetID
	}

	return inventory, currency, totalCount, nil
}

var rxAppContextData = regexp.MustCompile(`(?s)var g_rgAppContextData\s*=\s*(.*?);`)

// GetUserInventoryContexts retrieves the application and context details for a user's inventory.
func GetUserInventoryContexts(
	ctx context.Context,
	c community.Requester,
	userID uint64,
) (map[string]*AppContext, error) {
	path := fmt.Sprintf("profiles/%d/inventory", userID)

	htmlBytes, err := community.GetHTML(ctx, c, path)
	if err != nil {
		return nil, fmt.Errorf("inventory: failed to fetch inventory page: %w", err)
	}

	if bytes.Contains(htmlBytes, []byte("This profile is private.")) {
		return nil, errors.New("inventory: profile is private")
	}

	if bytes.Contains(htmlBytes, []byte("The inventory is currently private.")) ||
		bytes.Contains(htmlBytes, []byte("inventory is currently private")) {
		return nil, errors.New("inventory: inventory is private")
	}

	match := rxAppContextData.FindSubmatch(htmlBytes)
	if len(match) != 2 {
		return nil, errors.New("inventory: malformed page (g_rgAppContextData not found)")
	}

	// In some cases, if the inventory is empty, Steam might return an empty JSON array `[]` instead of `{}`,
	// which fails to unmarshal into map[string]*AppContext. We check for `[]` first.
	cleanedJSON := bytes.TrimSpace(match[1])
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

var (
	rxHistoryInventory = regexp.MustCompile(`(?s)var g_rgHistoryInventory\s*=\s*(.*?);`)
	rxHover            = regexp.MustCompile(
		`HistoryPageCreateItemHover\(\s*'\s*([^']+)\s*'\s*,\s*(\d+)\s*,\s*['"]([^'"]+)['"]\s*,\s*['"]([^'"]+)['"]\s*,\s*['"]([^'"]+)['"]\s*\)`,
	)
	rxTime       = regexp.MustCompile(`(\d+):(\d+)\s*(am|pm|AM|PM)`)
	rxAfterTime  = regexp.MustCompile(`after_time=(\d+)`)
	rxAfterTrade = regexp.MustCompile(`after_trade=(\d+)`)
)

// GetInventoryHistory fetches and parses the Steam inventory history for the specified user.
func GetInventoryHistory(
	ctx context.Context,
	client community.Requester,
	steamID id.ID,
	opts HistoryOptions,
) (*TradeHistoryResult, error) {
	// 1. Build request queries
	query := url.Values{
		"l": {"english"},
	}

	if opts.StartTime != nil {
		query.Set("after_time", strconv.FormatInt(opts.StartTime.Unix(), 10))

		if opts.StartTrade != nil {
			query.Set("after_trade", strconv.FormatUint(*opts.StartTrade, 10))
		}
	}

	if opts.Direction == DirectionFuture {
		query.Set("prev", "1")
	}

	path := fmt.Sprintf("profiles/%d/inventoryhistory", steamID)

	// Fetch HTML page
	var htmlOpts []api.CallOption
	if len(query) > 0 {
		htmlOpts = append(htmlOpts, api.WithQueryParams(query))
	}

	htmlBytes, err := community.GetHTML(ctx, client, path, htmlOpts...)
	if err != nil {
		return nil, fmt.Errorf("history: failed to fetch inventory history page: %w", err)
	}

	// 2. Extract g_rgHistoryInventory JSON
	matchInv := rxHistoryInventory.FindSubmatch(htmlBytes)
	if len(matchInv) != 2 {
		return nil, errors.New("history: malformed page (g_rgHistoryInventory not found)")
	}

	// Map layout: [appid][contextid][assetid_or_description_id]EconItem
	var historyInventory map[string]map[string]map[string]EconItem
	if err := json.Unmarshal(matchInv[1], &historyInventory); err != nil {
		return nil, fmt.Errorf("history: failed to parse history inventory JSON: %w", err)
	}

	doc, err := goquery.NewDocumentFromReader(bytes.NewReader(htmlBytes))
	if err != nil {
		return nil, fmt.Errorf("history: failed to parse HTML document: %w", err)
	}

	if doc.Find(".inventory_history_pagingrow").Length() == 0 {
		return nil, errors.New("history: malformed page (paging row not found)")
	}

	output := &TradeHistoryResult{
		Trades: make([]TradeHistoryRow, 0),
	}

	// 3. Parse paging buttons
	doc.Find(".inventory_history_nextbtn .pagebtn:not(.disabled)").Each(func(_ int, s *goquery.Selection) {
		href, exists := s.Attr("href")
		if !exists {
			return
		}

		timeMatch := rxAfterTime.FindStringSubmatch(href)

		tradeMatch := rxAfterTrade.FindStringSubmatch(href)
		if len(timeMatch) != 2 || len(tradeMatch) != 2 {
			return
		}

		unixTime, _ := strconv.ParseInt(timeMatch[1], 10, 64)
		tVal := time.Unix(unixTime, 0).UTC()

		tradeID, _ := strconv.ParseUint(tradeMatch[1], 10, 64)

		if strings.Contains(href, "prev=1") {
			output.FirstTradeTime = &tVal
			output.FirstTradeID = &tradeID
		} else {
			output.LastTradeTime = &tVal
			output.LastTradeID = &tradeID
		}
	})

	// 4. Compile Hover Map
	hoverMap := make(map[string]hoverInfo)

	hovers := rxHover.FindAllSubmatch(htmlBytes, -1)
	for _, hover := range hovers {
		if len(hover) != 6 {
			continue
		}

		elID := string(hover[1])
		amount, _ := strconv.Atoi(string(hover[5]))
		hoverMap[elID] = hoverInfo{
			AppID:     string(hover[2]),
			ContextID: string(hover[3]),
			AssetID:   string(hover[4]),
			Amount:    amount,
		}
	}

	// 5. Parse Trade Rows
	doc.Find(".tradehistoryrow").Each(func(_ int, s *goquery.Selection) {
		row := TradeHistoryRow{
			ItemsReceived: make([]EconItem, 0),
			ItemsGiven:    make([]EconItem, 0),
		}

		// Check hold
		holdText := s.Find("span:nth-of-type(2)").Text()
		row.OnHold = strings.Contains(strings.ToLower(holdText), "trade on hold")

		// Parse Time and Date
		timeText := s.Find(".tradehistory_timestamp").Text()

		time24, err := convertTimeTo24h(timeText)
		if err == nil {
			dateText := s.Find(".tradehistory_date").Text()

			parsedTime, err := parseTradeDate(dateText, time24)
			if err == nil {
				row.Date = parsedTime
			}
		}

		// Partner info
		partnerAnchor := s.Find(".tradehistory_event_description a")
		row.PartnerName = partnerAnchor.Text()

		profileLink, exists := partnerAnchor.Attr("href")
		if exists {
			if strings.Contains(profileLink, "/profiles/") {
				parts := strings.Split(strings.TrimRight(profileLink, "/"), "/")
				if len(parts) > 0 {
					sidVal, _ := strconv.ParseUint(parts[len(parts)-1], 10, 64)
					row.PartnerSteamID = id.ID(sidVal)
				}
			} else {
				parts := strings.Split(strings.TrimRight(profileLink, "/"), "/")
				if len(parts) > 0 {
					row.PartnerVanityURL = parts[len(parts)-1]
				}
			}
		}

		// Parse Items
		s.Find(".history_item").Each(func(_ int, itemSel *goquery.Selection) {
			elID, exists := itemSel.Attr("id")
			if !exists {
				return
			}

			info, ok := hoverMap[elID]
			if !ok {
				return
			}

			appMap, ok := historyInventory[info.AppID]
			if !ok {
				return
			}

			ctxMap, ok := appMap[info.ContextID]
			if !ok {
				return
			}

			itemDetail, ok := ctxMap[info.AssetID]
			if !ok {
				return
			}

			itemDetail.Amount = info.Amount

			if strings.Contains(elID, "received") {
				row.ItemsReceived = append(row.ItemsReceived, itemDetail)
			} else {
				row.ItemsGiven = append(row.ItemsGiven, itemDetail)
			}
		})

		output.Trades = append(output.Trades, row)
	})

	return output, nil
}

func convertTimeTo24h(timestamp string) (string, error) {
	match := rxTime.FindStringSubmatch(timestamp)
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
	dateText = strings.TrimSpace(dateText)
	timeText = strings.TrimSpace(timeText)

	// Clean double commas or extra spaces
	dateText = strings.ReplaceAll(dateText, "  ", " ")

	if !strings.Contains(dateText, ",") {
		currentYear := time.Now().UTC().Year()
		dateText = fmt.Sprintf("%s, %d", dateText, currentYear)
	}

	combined := fmt.Sprintf("%s %s UTC", dateText, timeText)

	// Format: "2 Jan, 2006 15:04:05 MST"
	t, err := time.Parse("2 Jan, 2006 15:04:05 MST", combined)
	if err == nil {
		return t, nil
	}

	// Format: "Jan 2, 2006 15:04:05 MST"
	t, err = time.Parse("Jan 2, 2006 15:04:05 MST", combined)
	if err == nil {
		return t, nil
	}

	// Format: "2 Jan 2006 15:04:05 MST" (without comma)
	cleanCombined := strings.ReplaceAll(combined, ",", "")

	t, err = time.Parse("2 Jan 2006 15:04:05 MST", cleanCombined)
	if err == nil {
		return t, nil
	}

	return time.Time{}, fmt.Errorf("could not parse date %q: %w", combined, err)
}

// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package inventory retrieves and parses Steam user inventories.
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
	}{"english", opts.StartTime, opts.StartTrade, generic.Ternary(opts.Direction == DirectionFuture, 1, 0)}

	html, err := community.GetHTML(
		ctx, client, "profiles/{steamID}/inventoryhistory",
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

func fetchInventoryPage(
	ctx context.Context,
	client community.Requester,
	steamID uint64,
	appID uint32,
	contextID int64,
	startAssetID string,
	language string,
) (*inventoryResponse, error) {
	params := struct {
		Language     string `url:"l"`
		Count        int    `url:"count"`
		StartAssetID string `url:"start_assetid,omitempty"`
	}{language, 1000, startAssetID}

	resp, err := community.GetTo[inventoryResponse](
		ctx, client, "inventory/{steamID}/{appID}/{contextID}",
		aoni.WithQuery(params),
		aoni.WithVars("steamID", steamID, "appID", appID, "contextID", contextID),
		aoni.WithHeader("Referer", fmt.Sprintf(community.BaseURL+"profiles/%d/inventory", steamID)),
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
	html, err := community.GetHTML(
		ctx, client, "profiles/{userID}/inventory",
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

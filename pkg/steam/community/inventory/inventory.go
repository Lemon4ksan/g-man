// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package inventory retrieves and parses Steam user inventories.
package inventory

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	json "github.com/goccy/go-json"
	"github.com/lemon4ksan/aoni/mod"
	"github.com/lemon4ksan/miyako/generic"

	"github.com/lemon4ksan/g-man/internal/bytesconv"
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

var descMapPool = sync.Pool{
	New: func() any {
		return make(map[descKey]*Description, 128)
	},
}

func acquireDescMap() map[descKey]*Description {
	return descMapPool.Get().(map[descKey]*Description)
}

func releaseDescMap(m map[descKey]*Description) {
	if m == nil {
		return
	}

	clear(m)
	descMapPool.Put(m)
}

type descKey = uint64

// packDescKey parses ClassID and InstanceID into a 64-bit key, fast-pathing '0' instance IDs.
func packDescKey(classIDStr, instanceIDStr string) descKey {
	cID, _ := bytesconv.ParseUint64(bytesconv.S2B(classIDStr))

	var instID uint64
	if len(instanceIDStr) > 0 && (len(instanceIDStr) != 1 || instanceIDStr[0] != '0') {
		instID, _ = bytesconv.ParseUint64(bytesconv.S2B(instanceIDStr))
	}

	return descKey((cID << 32) | (instID & 0xFFFFFFFF))
}

// StreamUserInventoryContents streams user inventory items page-by-page directly to handler,
// avoiding allocating full inventory slices in heap memory.
func StreamUserInventoryContents(
	ctx context.Context,
	client community.Requester,
	steamID uint64,
	appID uint32,
	contextID int64,
	tradableOnly bool,
	language string,
	handler func(item *CEconItem, isCurrency bool) bool,
) (int, error) {
	language = generic.Coalesce(language, "english")

	var (
		startAssetID string
		totalCount   int
		pos          = 1
	)

	for {
		page, err := fetchInventoryPage(ctx, client, steamID, appID, contextID, startAssetID, language)
		if err != nil {
			return 0, err
		}

		if page.TotalCount == 0 || len(page.Assets) == 0 {
			return page.TotalCount, nil
		}

		totalCount = page.TotalCount

		descMap := acquireDescMap()
		for i := range page.Descriptions {
			d := &page.Descriptions[i]
			key := packDescKey(d.ClassID, d.InstanceID)
			descMap[key] = d
		}

		for i := range page.Assets {
			asset := &page.Assets[i]
			key := packDescKey(asset.ClassID, asset.InstanceID)

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
				Asset:       *asset,
				Description: *description,
			}

			isCurrency := asset.CurrencyID != ""
			if handler != nil && !handler(&item, isCurrency) {
				releaseDescMap(descMap)
				return totalCount, nil
			}
		}

		releaseDescMap(descMap)

		if !page.MoreItems {
			break
		}

		startAssetID = page.LastAssetID
	}

	return totalCount, nil
}

// GetUserInventoryContents retrieves inventory contents appending directly to target slices with zero intermediate allocations.
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

		if inventory == nil {
			inventory = make([]CEconItem, 0, page.TotalCount)
			currency = make([]CEconItem, 0, 16)
		}

		descMap := acquireDescMap()
		for i := range page.Descriptions {
			d := &page.Descriptions[i]
			key := packDescKey(d.ClassID, d.InstanceID)
			descMap[key] = d
		}

		pos = appendProcessedAssets(&inventory, &currency, page.Assets, descMap, tradableOnly, pos)
		releaseDescMap(descMap)

		totalCount = page.TotalCount

		if !page.MoreItems {
			break
		}

		startAssetID = page.LastAssetID
	}

	return inventory, currency, totalCount, nil
}

func appendProcessedAssets(
	dstInventory *[]CEconItem,
	dstCurrency *[]CEconItem,
	assets []Asset,
	descMap map[descKey]*Description,
	tradableOnly bool,
	startPos int,
) int {
	pos := startPos

	for i := range assets {
		asset := &assets[i]
		key := packDescKey(asset.ClassID, asset.InstanceID)

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
			Asset:       *asset,
			Description: *description,
		}

		if asset.CurrencyID != "" {
			*dstCurrency = append(*dstCurrency, item)
		} else {
			*dstInventory = append(*dstInventory, item)
		}
	}

	return pos
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
		mod.WithVar("steamID", steamID),
		mod.WithQuery(params),
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
		mod.WithQuery(params),
		mod.WithVars("steamID", steamID, "appID", appID, "contextID", contextID),
		mod.WithHeader("Referer", fmt.Sprintf(community.BaseURL+"profiles/%d/inventory", steamID)),
	)
	if err != nil {
		return nil, err
	}

	if !resp.Success {
		return nil, fmt.Errorf("steam error: %s", resp.Error)
	}

	return resp, nil
}

func fetchInventoryPageHTML(ctx context.Context, client community.Requester, userID uint64) ([]byte, error) {
	html, err := community.GetHTML(
		ctx, client, "profiles/{userID}/inventory",
		mod.WithVar("userID", userID),
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

	var buf [8]byte

	buf[0] = byte('0' + hour/10)
	buf[1] = byte('0' + hour%10)
	buf[2] = ':'
	buf[3] = byte('0' + minute/10)
	buf[4] = byte('0' + minute%10)
	buf[5] = ':'
	buf[6] = '0'
	buf[7] = '0'

	return string(buf[:]), nil
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

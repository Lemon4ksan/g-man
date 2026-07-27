// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package inventory

import (
	"bytes"
	"strconv"
	"time"

	json "github.com/goccy/go-json"
	"github.com/lemon4ksan/aoni/codec/values"

	"github.com/lemon4ksan/g-man/pkg/steam/id"
)

// AppData represents typed metadata attached to Steam inventory items.
type AppData struct {
	DefIndex     int    `json:"-"`
	Quality      int    `json:"-"`
	OriginalID   uint64 `json:"-"`
	IsAustralium bool   `json:"-"`
}

// UnmarshalJSON implements custom unmarshaling to extract defindex, quality, and australium flags without map allocation overhead.
func (a *AppData) UnmarshalJSON(data []byte) error {
	if len(data) == 0 || bytes.Equal(data, []byte("null")) {
		return nil
	}

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil //nolint:nilerr
	}

	parseInt := func(msg json.RawMessage) int {
		if len(msg) == 0 {
			return 0
		}

		s := bytes.Trim(msg, `"`)
		val, _ := strconv.Atoi(string(s))

		return val
	}

	parseUint64 := func(msg json.RawMessage) uint64 {
		if len(msg) == 0 {
			return 0
		}

		s := bytes.Trim(msg, `"`)
		val, _ := strconv.ParseUint(string(s), 10, 64)

		return val
	}

	if msg, ok := raw["def_index"]; ok {
		a.DefIndex = parseInt(msg)
	}

	if msg, ok := raw["quality"]; ok {
		a.Quality = parseInt(msg)
	}

	if msg, ok := raw["original_id"]; ok {
		a.OriginalID = parseUint64(msg)
	}

	if msg, ok := raw["attributes"]; ok {
		if bytes.Contains(msg, []byte(`"2027"`)) {
			a.IsAustralium = true
		}
	}

	return nil
}

type Asset struct {
	AssetID    string `json:"assetid"`
	ClassID    string `json:"classid"`
	InstanceID string `json:"instanceid"`
	CurrencyID string `json:"currencyid,omitempty"`
	Amount     string `json:"amount"`
	Pos        int    `json:"-"`
}

type Description struct {
	ClassID         string `json:"classid"`
	InstanceID      string `json:"instanceid"`
	Tradable        int    `json:"tradable"`
	Name            string `json:"name"`
	Type            string `json:"type"`
	MarketHashName  string `json:"market_hash_name"`
	BackgroundColor string `json:"background_color"`
	IconURL         string `json:"icon_url"`
	Tags            []Tag  `json:"tags"`
	AppData         *AppData
	Descriptions    []struct {
		Value string `json:"value"`
		Color string `json:"color,omitempty"`
	} `json:"descriptions,omitempty"`
}

type Tag struct {
	Category              string `json:"category"`
	InternalName          string `json:"internal_name"`
	LocalizedCategoryName string `json:"localized_category_name"`
	LocalizedTagName      string `json:"localized_tag_name"`
}

type CEconItem struct {
	Asset       Asset
	Description Description
}

type inventoryResponse struct {
	Success      values.BoolInt `json:"success"`
	Error        string         `json:"error"`
	Assets       []Asset        `json:"assets"`
	Descriptions []Description  `json:"descriptions"`
	MoreItems    values.BoolInt `json:"more_items"`
	LastAssetID  string         `json:"last_assetid"`
	TotalCount   int            `json:"total_inventory_count"`
}

type AppContext struct {
	AppID      uint32                    `json:"appid"`
	Name       string                    `json:"name"`
	Icon       string                    `json:"icon"`
	Link       string                    `json:"link"`
	AssetCount int                       `json:"asset_count"`
	Contexts   map[string]*ContextDetail `json:"rgContexts"`
}

type ContextDetail struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	AssetCount int    `json:"asset_count"`
}

type EconItem struct {
	AppID           uint32 `json:"appid"`
	ContextID       string `json:"contextid"`
	AssetID         string `json:"id"`
	ClassID         string `json:"classid"`
	InstanceID      string `json:"instanceid"`
	Amount          int    `json:"amount,string"`
	IconURL         string `json:"icon_url"`
	MarketHashName  string `json:"market_hash_name"`
	Name            string `json:"name"`
	Type            string `json:"type"`
	BackgroundColor string `json:"background_color"`
	Marketable      bool   `json:"marketable"`
	Tradable        bool   `json:"tradable"`
}

type TradeHistoryRow struct {
	Date             time.Time
	PartnerName      string
	PartnerSteamID   id.ID
	PartnerVanityURL string
	ItemsReceived    []EconItem
	ItemsGiven       []EconItem
	OnHold           bool
}

type TradeHistoryResult struct {
	Trades         []TradeHistoryRow
	FirstTradeTime *time.Time
	FirstTradeID   *uint64
	LastTradeTime  *time.Time
	LastTradeID    *uint64
}

type hoverInfo struct {
	AppID     string
	ContextID string
	AssetID   string
	Amount    int
}

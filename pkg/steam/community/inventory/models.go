// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package inventory

import (
	"time"

	"github.com/lemon4ksan/aoni"

	"github.com/lemon4ksan/g-man/pkg/steam/id"
)

// Asset represents an item in the inventory.
type Asset struct {
	// AssetID is the unique string identifier of the asset.
	AssetID string `json:"assetid"`
	// ClassID is the class identifier of the asset.
	ClassID string `json:"classid"`
	// InstanceID is the instance identifier of the asset.
	InstanceID string `json:"instanceid"`
	// CurrencyID is the currency identifier of the asset if applicable.
	CurrencyID string `json:"currencyid,omitempty"`
	// Amount is the quantity of the asset.
	Amount string `json:"amount"`
	// Pos is the sequential position of the item in the list.
	Pos int `json:"-"`
}

// Description represents the description of an item.
type Description struct {
	// ClassID is the class identifier of the item.
	ClassID string `json:"classid"`
	// InstanceID is the instance identifier of the item.
	InstanceID string `json:"instanceid"`
	// Tradable is the tradability status (1 if tradable, 0 otherwise).
	Tradable int `json:"tradable"`
	// Name is the display name of the item.
	Name string `json:"name"`
	// MarketHashName is the standard market identifier name.
	MarketHashName string `json:"market_hash_name"`
	// BackgroundColor is the background color of the item's icon.
	BackgroundColor string `json:"background_color"`
	// IconURL is the direct path suffix to the item's icon image.
	IconURL string `json:"icon_url"`
	// Tags contains the parsed tags of the item.
	Tags []Tag `json:"tags"`
	// AppData contains optional application-specific metadata.
	AppData map[string]any `json:"app_data,omitempty"`
	// Descriptions contains detailed inline text descriptions.
	Descriptions []struct {
		Value string `json:"value"`
		Color string `json:"color,omitempty"`
	} `json:"descriptions,omitempty"`
}

// Tag represents a tag of an item.
type Tag struct {
	// Category is the category identifier of the tag.
	Category string `json:"category"`
	// InternalName is the internal identifier name of the tag.
	InternalName string `json:"internal_name"`
	// LocalizedCategoryName is the localized name of the category.
	LocalizedCategoryName string `json:"localized_category_name"`
	// LocalizedTagName is the localized name of the tag.
	LocalizedTagName string `json:"localized_tag_name"`
}

// CEconItem represents an item in the inventory with its description.
type CEconItem struct {
	// Asset is the underlying inventory asset details.
	Asset Asset
	// Description is the parsed item description details.
	Description Description
}

type inventoryResponse struct {
	Success      aoni.BoolInt  `json:"success"`
	Error        string        `json:"error"`
	Assets       []Asset       `json:"assets"`
	Descriptions []Description `json:"descriptions"`
	MoreItems    aoni.BoolInt  `json:"more_items"`
	LastAssetID  string        `json:"last_assetid"`
	TotalCount   int           `json:"total_inventory_count"`
}

// AppContext represents an application context block in Steam inventory data.
type AppContext struct {
	// AppID is the Steam AppID of the application.
	AppID uint32 `json:"appid"`
	// Name is the name of the application.
	Name string `json:"name"`
	// Icon is the direct URL to the application icon.
	Icon string `json:"icon"`
	// Link is the link to the application inventory.
	Link string `json:"link"`
	// AssetCount is the total number of assets in this application.
	AssetCount int `json:"asset_count"`
	// Contexts maps context IDs to their detailed descriptors.
	Contexts map[string]*ContextDetail `json:"rgContexts"`
}

// ContextDetail represents individual context detail inside AppContext.
type ContextDetail struct {
	// ID is the unique string identifier of the context.
	ID string `json:"id"`
	// Name is the display name of the context.
	Name string `json:"name"`
	// AssetCount is the total number of assets in this specific context.
	AssetCount int `json:"asset_count"`
}

// EconItem represents a flat description of a traded item in history.
type EconItem struct {
	// AppID is the Steam AppID of the application.
	AppID uint32 `json:"appid"`
	// ContextID is the unique string identifier of the context.
	ContextID string `json:"contextid"`
	// AssetID is the unique string identifier of the asset.
	AssetID string `json:"id"`
	// ClassID is the class identifier of the asset.
	ClassID string `json:"classid"`
	// InstanceID is the instance identifier of the asset.
	InstanceID string `json:"instanceid"`
	// Amount is the quantity of the asset.
	Amount int `json:"amount,string"`
	// IconURL is the direct path suffix to the item's icon image.
	IconURL string `json:"icon_url"`
	// MarketHashName is the standard market identifier name.
	MarketHashName string `json:"market_hash_name"`
	// Name is the display name of the item.
	Name string `json:"name"`
	// Type is the localized classification type of the item.
	Type string `json:"type"`
	// BackgroundColor is the background color of the item's icon.
	BackgroundColor string `json:"background_color"`
	// Marketable is true if the item can be sold on the Steam Market.
	Marketable bool `json:"marketable"`
	// Tradable is true if the item can be traded.
	Tradable bool `json:"tradable"`
}

// TradeHistoryRow represents a single completed or pending trade history event.
type TradeHistoryRow struct {
	// Date is the timestamp when the trade occurred.
	Date time.Time
	// PartnerName is the display name of the trade partner.
	PartnerName string
	// PartnerSteamID is the unique 64-bit Steam identifier of the trade partner.
	PartnerSteamID id.ID
	// PartnerVanityURL is the vanity profile slug of the trade partner.
	PartnerVanityURL string
	// ItemsReceived is the list of items received during the trade.
	ItemsReceived []EconItem
	// ItemsGiven is the list of items given during the trade.
	ItemsGiven []EconItem
	// OnHold is true if the trade is currently in a trade hold (escrow).
	OnHold bool
}

// TradeHistoryResult aggregates all parsed trades and the pagination markers.
type TradeHistoryResult struct {
	// Trades is the chronological list of parsed trades.
	Trades []TradeHistoryRow
	// FirstTradeTime specifies the starting time boundary for the next chronological page.
	FirstTradeTime *time.Time
	// FirstTradeID specifies the starting trade ID boundary for the next chronological page.
	FirstTradeID *uint64
	// LastTradeTime specifies the ending time boundary for the previous chronological page.
	LastTradeTime *time.Time
	// LastTradeID specifies the ending trade ID boundary for the previous chronological page.
	LastTradeID *uint64
}

type hoverInfo struct {
	AppID     string
	ContextID string
	AssetID   string
	Amount    int
}

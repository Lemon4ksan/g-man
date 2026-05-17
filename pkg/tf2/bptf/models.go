// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package bptf

import "github.com/lemon4ksan/g-man/pkg/steam/id"

// PricesResponseV4 represents a response from the bptf prices API.
type PricesResponseV4 struct {
	Success     int                    `json:"success"`
	Message     string                 `json:"message,omitempty"`
	CurrentTime int64                  `json:"current_time"`
	RawUSDValue int                    `json:"raw_usd_value"`
	USDCurrency string                 `json:"usd_currency"`
	Items       map[string]BaseItemDoc `json:"items"`
}

// BaseItemDoc represents a base item document.
type BaseItemDoc struct {
	Defindexes []string                                               `json:"defindex"`
	Prices     map[string]map[string]map[string]map[string]PriceEntry `json:"prices"` // Quality -> Tradable -> Craftable -> PriceIndex
}

// PriceEntry represents a price entry.
type PriceEntry struct {
	Currency   string  `json:"currency"`
	Value      float64 `json:"value"`
	ValueHigh  float64 `json:"value_high,omitempty"`
	ValueRaw   float64 `json:"value_raw"`
	LastUpdate int64   `json:"last_update"`
}

// CurrenciesResponseV1 represents a response from the bptf currencies API.
type CurrenciesResponseV1 struct {
	Success    int                     `json:"success"`
	Message    string                  `json:"message,omitempty"`
	Name       string                  `json:"name"`
	Currencies map[string]CurrencyInfo `json:"currencies"`
}

// ListingResolvable represents a resolvable listing.
type ListingResolvable struct {
	ID         uint64             `json:"id,omitempty"`
	Item       any                `json:"item,omitempty"`
	Intent     string             `json:"intent,omitempty"` // "buy" or "sell"
	Details    string             `json:"details,omitempty"`
	Currencies map[string]float64 `json:"currencies"` // e.g. {"keys": 1, "metal": 25.33}
}

// ListingResponse represents a listing.
type ListingResponse struct {
	ID                   string             `json:"id"`
	SteamID              string             `json:"steamid"`
	AppID                int                `json:"appid"`
	Intent               string             `json:"intent"` // "buy" or "sell"
	Details              string             `json:"details"`
	Currencies           map[string]float64 `json:"currencies"`
	Count                int                `json:"count"`
	Promoted             bool               `json:"promoted"`
	TradeOffersPreferred bool               `json:"tradeOffersPreferred"`
	BuyoutOnly           bool               `json:"buyoutOnly"`
	ListedAt             int64              `json:"listedAt,string"`
	BumpedAt             int64              `json:"bumpedAt,string"`
	Item                 ItemDocument       `json:"item"`
}

// ListingsResponse represents a scrollable response of listings.
type ListingsResponse struct {
	Results []ListingResponse `json:"results"`
	Cursor  Cursor            `json:"cursor"`
}

// SnapshotResponse represents the response from the classifieds listings snapshot API.
type SnapshotResponse struct {
	Listings []ListingResponse `json:"listings"`
}

// ListingBatchCreateResult represents a batch create result.
type ListingBatchCreateResult struct {
	Result *ListingResponse `json:"result,omitempty"`
	Error  *struct {
		Message string `json:"message"`
		Reason  string `json:"reason"`
	} `json:"error,omitempty"`
}

// CurrencyInfo represents currency information.
type CurrencyInfo struct {
	Name       string     `json:"name"`
	Quality    string     `json:"quality"`
	Priceindex string     `json:"priceindex"`
	Single     string     `json:"single"` // Singular form (e.g. "ref")
	Plural     string     `json:"plural"` // Plural form (e.g. "refs")
	Round      int        `json:"round"`
	Craftable  string     `json:"craftable"`
	Defindex   int        `json:"defindex"`
	Active     int        `json:"active"`
	Price      PriceEntry `json:"price"`
}

// InventoryStatus represents inventory status.
type InventoryStatus struct {
	RefreshInterval int   `json:"refresh_interval"`
	NextUpdate      int64 `json:"next_update"`
	LastUpdate      int64 `json:"last_update"` // Last attempt
	Timestamp       int64 `json:"timestamp"`   // Last success
	CurrentTime     int64 `json:"current_time"`
}

// InventoryValues represents inventory values.
type InventoryValues struct {
	Value       float64 `json:"value"`        // Community value
	MarketValue float64 `json:"market_value"` // Steam Community Market value
}

// V1UserResponse represents a response from the bptf user API.
type V1UserResponse struct {
	Users map[id.ID]V1User `json:"users"`
}

// V1User represents a user.
type V1User struct {
	Name       string         `json:"name"`
	Avatar     string         `json:"avatar"`
	LastOnline int64          `json:"last_online,string"`
	Admin      int            `json:"admin,omitempty"`
	Donated    float64        `json:"donated,omitempty"`
	Premium    int            `json:"premium,omitempty"`
	Bans       *UserBans      `json:"bans,omitempty"`
	Trust      UserTrust      `json:"trust"`
	Inventory  map[string]any `json:"inventory,omitempty"`
}

// UserBans represents ban information for a user.
type UserBans struct {
	All             string `json:"all,omitempty"`
	SteamRepScammer int    `json:"steamrep_scammer,omitempty"`
	BPTF            string `json:"bptf_banned,omitempty"`
}

// UserTrust represents user trust information.
type UserTrust struct {
	Positive int `json:"positive"`
	Negative int `json:"negative"`
}

// AlertsResponse represents a response from the bptf alerts API.
type AlertsResponse struct {
	Results []Alert `json:"results"`
	Cursor  Cursor  `json:"cursor"`
}

// Alert represents an alert.
type Alert struct {
	ID       string  `json:"id"`
	ItemName string  `json:"item_name"`
	Intent   string  `json:"intent"` // "buy" or "sell"
	Currency string  `json:"currency"`
	Min      float64 `json:"min"`
	Max      float64 `json:"max"`
}

// Cursor represents a cursor.
type Cursor struct {
	Skip  int `json:"skip"`
	Limit int `json:"limit"`
	Total int `json:"total"`
}

// ResponseError represents a response error.
type ResponseError struct {
	Message string `json:"message"`
	Reason  string `json:"reason"` // Machine-readable code
}

// UserAgentStatus represents user agent status.
type UserAgentStatus struct {
	Status      string `json:"status"` // "active" or "inactive"
	Client      string `json:"client,omitempty"`
	ExpireAt    int64  `json:"expire_at,omitempty"`
	CurrentTime int64  `json:"current_time"`
}

// NotificationsResponse represents a response from the bptf notifications API.
type NotificationsResponse struct {
	Results []Notification `json:"results"`
	Cursor  Cursor         `json:"cursor"`
}

// Notification represents a notification.
type Notification struct {
	ID      string         `json:"id"`
	Type    int            `json:"type"`
	Time    int64          `json:"time"`
	Unread  int            `json:"unread"`
	Message string         `json:"message"`
	Bundle  map[string]any `json:"bundle,omitempty"`
}

// PriceHistoryResponse represents a response from the bptf price history API.
type PriceHistoryResponse struct {
	Success int                `json:"success"`
	History []PriceHistoryNode `json:"history"`
}

// PriceHistoryNode represents a single point in price history.
type PriceHistoryNode struct {
	Value     float64 `json:"value"`
	ValueHigh float64 `json:"value_high,omitempty"`
	Currency  string  `json:"currency"`
	Timestamp int64   `json:"timestamp"`
}

// NotificationMarkResponse represents a response from marking notifications as read.
type NotificationMarkResponse struct {
	Modified int `json:"modified"`
}

// Entity represents an item attribute that usually has a name and an ID.
type Entity struct {
	ID    int    `json:"id"`
	Name  string `json:"name"`
	Color string `json:"color,omitempty"`
}

// ItemDocument represents the V2 item document.
type ItemDocument struct {
	BaseName        string  `json:"baseName"`
	Name            string  `json:"name"`
	Quantity        int     `json:"quantity"`
	Quality         Entity  `json:"quality"`
	Rarity          Entity  `json:"rarity"`
	Paint           *Entity `json:"paint,omitempty"`
	Particle        *Entity `json:"particle,omitempty"`
	ElevatedQuality *Entity `json:"elevatedQuality,omitempty"`
	Tradable        bool    `json:"tradable"`
	Craftable       bool    `json:"craftable"`
}

// ListingDropRequest represents a request to delete all active or archived listings.
type ListingDropRequest struct {
	Intent string `json:"intent,omitempty"`
}

// ListingPatchRequest represents a request to update some fields of a listing.
type ListingPatchRequest struct {
	Currencies           map[string]float64 `json:"currencies,omitempty"`
	Details              *string            `json:"details,omitempty"`
	Count                *int               `json:"count,omitempty"`
	TradeOffersPreferred *bool              `json:"tradeOffersPreferred,omitempty"`
	BuyoutOnly           *bool              `json:"buyoutOnly,omitempty"`
}

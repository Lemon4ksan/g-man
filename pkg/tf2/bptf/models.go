// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package bptf

import "github.com/lemon4ksan/g-man/pkg/steam/id"

type PricesResponseV4 struct {
	Success     int                    `json:"success"`
	Message     string                 `json:"message,omitempty"`
	CurrentTime int64                  `json:"current_time"`
	RawUSDValue int                    `json:"raw_usd_value"`
	USDCurrency string                 `json:"usd_currency"`
	Items       map[string]BaseItemDoc `json:"items"`
}

type BaseItemDoc struct {
	Defindexes []string                                               `json:"defindex"`
	Prices     map[string]map[string]map[string]map[string]PriceEntry `json:"prices"` // Quality -> Tradable -> Craftable -> PriceIndex
}

type PriceEntry struct {
	Currency   string  `json:"currency"`
	Value      float64 `json:"value"`
	ValueHigh  float64 `json:"value_high,omitempty"`
	ValueRaw   float64 `json:"value_raw"`
	LastUpdate int64   `json:"last_update"`
}

type CurrenciesResponseV1 struct {
	Success    int                     `json:"success"`
	Message    string                  `json:"message,omitempty"`
	Name       string                  `json:"name"`
	Currencies map[string]CurrencyInfo `json:"currencies"`
}

type ListingResolvable struct {
	ID         uint64             `json:"id,omitempty"`      // Если это Sell (из инвентаря)
	Item       any                `json:"item,omitempty"`    // Если это Buy (поиск по характеристикам)
	Details    string             `json:"details,omitempty"` // Описание листинга
	Currencies map[string]float64 `json:"currencies"`        // e.g. {"keys": 1, "metal": 25.33}
}

type ListingResponse struct {
	ID         string             `json:"id"`
	SteamID    string             `json:"steamid"`
	AppID      int                `json:"appid"`
	Intent     string             `json:"intent"` // "buy" or "sell"
	Details    string             `json:"details"`
	Currencies map[string]float64 `json:"currencies"`
}

type ListingBatchCreateResult struct {
	Result *ListingResponse `json:"result,omitempty"`
	Error  *struct {
		Message string `json:"message"`
		Reason  string `json:"reason"`
	} `json:"error,omitempty"`
}

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

type InventoryStatus struct {
	RefreshInterval int   `json:"refresh_interval"`
	NextUpdate      int64 `json:"next_update"`
	LastUpdate      int64 `json:"last_update"` // Last attempt
	Timestamp       int64 `json:"timestamp"`   // Last success
	CurrentTime     int64 `json:"current_time"`
}

type InventoryValues struct {
	Value       float64 `json:"value"`        // Community value
	MarketValue float64 `json:"market_value"` // Steam Community Market value
}

type V1UserResponse struct {
	Users map[id.ID]V1User `json:"users"`
}

type V1User struct {
	Name       string         `json:"name"`
	Avatar     string         `json:"avatar"`
	LastOnline int64          `json:"last_online,string"`
	Admin      int            `json:"admin,omitempty"`
	Donated    float64        `json:"donated,omitempty"`
	Premium    int            `json:"premium,omitempty"`
	Bans       any            `json:"bans,omitempty"` // Can be complex object or string
	Trust      UserTrust      `json:"trust"`
	Inventory  map[string]any `json:"inventory,omitempty"`
}

type UserTrust struct {
	Positive int `json:"positive"`
	Negative int `json:"negative"`
}

type AlertsResponse struct {
	Results []Alert `json:"results"`
	Cursor  Cursor  `json:"cursor"`
}

type Alert struct {
	ID       string  `json:"id"`
	ItemName string  `json:"item_name"`
	Intent   string  `json:"intent"` // "buy" or "sell"
	Currency string  `json:"currency"`
	Min      float64 `json:"min"`
	Max      float64 `json:"max"`
}

type Cursor struct {
	Skip  int `json:"skip"`
	Limit int `json:"limit"`
	Total int `json:"total"`
}

type ResponseError struct {
	Message string `json:"message"`
	Reason  string `json:"reason"` // Machine-readable code
}

type UserAgentStatus struct {
	Status      string `json:"status"` // "active" or "inactive"
	Client      string `json:"client,omitempty"`
	ExpireAt    int64  `json:"expire_at,omitempty"`
	CurrentTime int64  `json:"current_time"`
}

type NotificationsResponse struct {
	Results []Notification `json:"results"`
	Cursor  Cursor         `json:"cursor"`
}

type Notification struct {
	ID      string         `json:"id"`
	Type    int            `json:"type"`
	Time    int64          `json:"time"`
	Unread  int            `json:"unread"`
	Message string         `json:"message"`
	Bundle  map[string]any `json:"bundle,omitempty"`
}

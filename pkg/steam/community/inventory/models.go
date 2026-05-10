// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package inventory

// Asset represents an item in the inventory.
type Asset struct {
	AssetID    string `json:"assetid"`
	ClassID    string `json:"classid"`
	InstanceID string `json:"instanceid"`
	CurrencyID string `json:"currencyid,omitempty"`
	Amount     string `json:"amount"`
	Pos        int    `json:"-"`
}

// Description represents the description of an item.
type Description struct {
	ClassID         string         `json:"classid"`
	InstanceID      string         `json:"instanceid"`
	Tradable        int            `json:"tradable"`
	Name            string         `json:"name"`
	MarketHashName  string         `json:"market_hash_name"`
	BackgroundColor string         `json:"background_color"`
	IconURL         string         `json:"icon_url"`
	Tags            []Tag          `json:"tags"`
	AppData         map[string]any `json:"app_data,omitempty"`
	Descriptions    []struct {
		Value string `json:"value"`
		Color string `json:"color,omitempty"`
	} `json:"descriptions,omitempty"`
}

// Tag represents a tag of an item.
type Tag struct {
	Category              string `json:"category"`
	InternalName          string `json:"internal_name"`
	LocalizedCategoryName string `json:"localized_category_name"`
	LocalizedTagName      string `json:"localized_tag_name"`
}

// CEconItem represents an item in the inventory with its description.
type CEconItem struct {
	Asset       Asset
	Description *Description
}

type inventoryResponse struct {
	Success      bool          `json:"success"`
	Error        string        `json:"error"`
	Assets       []Asset       `json:"assets"`
	Descriptions []Description `json:"descriptions"`
	MoreItems    bool          `json:"more_items"`
	LastAssetID  string        `json:"last_assetid"`
	TotalCount   int           `json:"total_inventory_count"`
}

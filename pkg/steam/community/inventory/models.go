// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package inventory

type InventoryAsset struct {
	AssetID    string `json:"assetid"`
	ClassID    string `json:"classid"`
	InstanceID string `json:"instanceid"`
	CurrencyID string `json:"currencyid,omitempty"`
	Amount     string `json:"amount"`
	Pos        int    `json:"-"`
}

type InventoryDescription struct {
	ClassID         string         `json:"classid"`
	InstanceID      string         `json:"instanceid"`
	Tradable        int            `json:"tradable"`
	Name            string         `json:"name"`
	MarketHashName  string         `json:"market_hash_name"`
	BackgroundColor string         `json:"background_color"`
	IconURL         string         `json:"icon_url"`
	Tags            []InventoryTag `json:"tags"`
}

type InventoryTag struct {
	Category              string `json:"category"`
	InternalName          string `json:"internal_name"`
	LocalizedCategoryName string `json:"localized_category_name"`
	LocalizedTagName      string `json:"localized_tag_name"`
}

type CEconItem struct {
	Asset       InventoryAsset
	Description *InventoryDescription
}

type inventoryResponse struct {
	Success      bool                   `json:"success"`
	Error        string                 `json:"error"`
	Assets       []InventoryAsset       `json:"assets"`
	Descriptions []InventoryDescription `json:"descriptions"`
	MoreItems    bool                   `json:"more_items"`
	LastAssetID  string                 `json:"last_assetid"`
	TotalCount   int                    `json:"total_inventory_count"`
}

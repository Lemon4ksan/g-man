// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package pricedb

// Currencies represents the price in TF2 currency.
type Currencies struct {
	Keys  int     `json:"keys"`
	Metal float64 `json:"metal"`
}

// Price represents a single price entry for an item.
type Price struct {
	Name   string     `json:"name"`
	SKU    string     `json:"sku"`
	Source string     `json:"source"`
	Time   int64      `json:"time"`
	Buy    Currencies `json:"buy"`
	Sell   Currencies `json:"sell"`
}

// SearchResult represents the response from the fuzzy search endpoint.
type SearchResult struct {
	Query   string `json:"query"`
	Total   int    `json:"total"`
	Limit   int    `json:"limit"`
	Results []struct {
		Price
		Relevance int `json:"relevance"`
	} `json:"results"`
}

// ItemStats represents the aggregated statistics for an item's price history.
type ItemStats struct {
	Buy  StatDetails `json:"buy"`
	Sell StatDetails `json:"sell"`
}

type StatDetails struct {
	Count int `json:"count"`
	Keys  struct {
		Min int     `json:"min"`
		Max int     `json:"max"`
		Avg float64 `json:"avg"`
	} `json:"keys"`
	Metal struct {
		Min float64 `json:"min"`
		Max float64 `json:"max"`
		Avg float64 `json:"avg"`
	} `json:"metal"`
}

// CompareResult represents the side-by-side comparison of two items.
type CompareResult struct {
	Items map[string]struct {
		Name string     `json:"name"`
		SKU  string     `json:"sku"`
		Buy  Currencies `json:"buy"`
		Sell Currencies `json:"sell"`
	} `json:"items"`
	Comparison struct {
		BuyDifference  Currencies `json:"buyDifference"`
		SellDifference Currencies `json:"sellDifference"`
	} `json:"comparison"`
	Meta struct {
		Compared    string `json:"compared"`
		HistoryDays int    `json:"historyDays"`
	} `json:"meta"`
}

// CacheStats represents the internal health and stats of the PriceDB server.
type CacheStats struct {
	Status string `json:"status"` // From /api/ health check
	DB     string `json:"db"`     // From /api/ health check
	Cache  struct {
		Size         int `json:"size"`
		MaxSize      int `json:"maxSize"`
		ActiveTimers int `json:"activeTimers"`
	} `json:"cache"`
	Database struct {
		TotalPrices  int   `json:"totalPrices"`
		UniqueItems  int   `json:"uniqueItems"`
		LatestUpdate int64 `json:"latestUpdate"`
	} `json:"database"`
}

// bulkRequest is the internal payload for fetching multiple SKUs.
type bulkRequest struct {
	SKUs []string `json:"skus"`
}

// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package market

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
)

// SearchOptions contains parameters for determining the TP.
type SearchOptions struct {
	Query              string
	Start              int
	Count              int
	SearchDescriptions bool
	SortColumn         string // "popular", "price", "quantity", "name"
	SortDir            string // "asc", "desc"
}

// Search searches for items on the marketplace.
func (m *Manager) Search(ctx context.Context, appID uint32, opts SearchOptions) (*SearchResponse, error) {
	if opts.Count == 0 {
		opts.Count = 100
	}
	if opts.SortColumn == "" {
		opts.SortColumn = "popular"
	}
	if opts.SortDir == "" {
		opts.SortDir = "desc"
	}

	searchDesc := "0"
	if opts.SearchDescriptions {
		searchDesc = "1"
	}

	params := url.Values{
		"query":               {opts.Query},
		"start":               {strconv.Itoa(opts.Start)},
		"count":               {strconv.Itoa(opts.Count)},
		"search_descriptions": {searchDesc},
		"sort_column":         {opts.SortColumn},
		"sort_dir":            {opts.SortDir},
		"appid":               {strconv.FormatUint(uint64(appID), 10)},
		"norender":            {"1"},
	}

	referer := fmt.Sprintf("https://steamcommunity.com/market/search?appid=%d", appID)
	var response SearchResponse

	err := m.community.GetJSON(ctx, "market/search/render", params, &response, withMarketHeaders(referer))
	if err != nil {
		return nil, err
	}

	return &response, nil
}

// GetPriceOverview gets a quick summary of the item's price.
func (m *Manager) GetPriceOverview(ctx context.Context, appID uint32, marketHashName string) (*PriceOverviewResponse, error) {
	params := url.Values{
		"appid":            {strconv.FormatUint(uint64(appID), 10)},
		"currency":         {strconv.Itoa(int(m.config.Currency))},
		"market_hash_name": {marketHashName},
	}

	var response PriceOverviewResponse
	err := m.community.GetJSON(ctx, "market/priceoverview", params, &response, withMarketHeaders(""))
	if err != nil {
		return nil, err
	}

	return &response, nil
}

// GetItemOrdersHistogram gets a histogram of active buy and sell orders.
// The itemNameID can be obtained by parsing the lot page (usually cached by the bot).
func (m *Manager) GetItemOrdersHistogram(ctx context.Context, appID uint32, marketHashName string, itemNameID uint64) (*ItemOrdersHistogram, error) {
	params := url.Values{
		"country":     {m.config.Country},
		"language":    {m.config.Language},
		"currency":    {strconv.Itoa(int(m.config.Currency))},
		"item_nameid": {strconv.FormatUint(itemNameID, 10)},
		"two_factor":  {"0"},
	}

	referer := fmt.Sprintf("https://steamcommunity.com/market/listings/%d/%s", appID, url.PathEscape(marketHashName))
	var response ItemOrdersHistogramResponse

	err := m.community.GetJSON(ctx, "market/itemordershistogram", params, &response, withMarketHeaders(referer))
	if err != nil {
		return nil, err
	}

	histogram := &ItemOrdersHistogram{
		Success:          response.Success == 1,
		SellOrderTable:   response.SellOrderTable,
		SellOrderSummary: response.SellOrderSummary,
		BuyOrderTable:    response.BuyOrderTable,
		BuyOrderSummary:  response.BuyOrderSummary,
		BuyOrderGraph:    response.BuyOrderGraph,
		SellOrderGraph:   response.SellOrderGraph,
		GraphMaxY:        response.GraphMaxY,
		GraphMinX:        response.GraphMinX,
		GraphMaxX:        response.GraphMaxX,
		PricePrefix:      response.PricePrefix,
		PriceSuffix:      response.PriceSuffix,
	}

	if response.HighestBuyOrder != "" {
		histogram.HighestBuyOrder, _ = strconv.ParseFloat(response.HighestBuyOrder, 64)
	}
	if response.LowestSellOrder != "" {
		histogram.LowestSellOrder, _ = strconv.ParseFloat(response.LowestSellOrder, 64)
	}

	return histogram, nil
}

// GetMyListings gets the active lots and orders of an account.
func (m *Manager) GetMyListings(ctx context.Context, start, count int) (*MyListingsResponse, error) {
	if count == 0 {
		count = 100
	}
	params := url.Values{
		"start":    {strconv.Itoa(start)},
		"count":    {strconv.Itoa(count)},
		"norender": {"1"},
	}

	var response MyListingsResponse
	err := m.community.GetJSON(ctx, "market/mylistings", params, &response, withMarketHeaders(""))
	if err != nil {
		return nil, err
	}
	return &response, nil
}

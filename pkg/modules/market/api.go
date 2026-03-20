// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package market

import (
	"context"
	"fmt"
	"net/url"
	"strconv"

	"github.com/lemon4ksan/g-man/pkg/steam/community"
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
	referer := fmt.Sprintf("https://steamcommunity.com/market/search?appid=%d", appID)

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

	req := struct {
		Query              string `url:"query"`
		Start              int    `url:"start"`
		Count              int    `url:"count"`
		SearchDescriptions string `url:"search_descriptions"`
		SortColumn         string `url:"sort_column"`
		SortDir            string `url:"sort_dir"`
		AppID              uint32 `url:"appid"`
		NoRender           string `url:"norender"`
	}{opts.Query, opts.Start, opts.Count, searchDesc, opts.SortColumn, opts.SortDir, appID, "1"}
	return community.Get[SearchResponse](
		ctx, m.community, "market/search/render", req, withMarketHeaders(referer),
	)
}

// GetPriceOverview gets a quick summary of the item's price.
func (m *Manager) GetPriceOverview(ctx context.Context, appID uint32, marketHashName string) (*PriceOverviewResponse, error) {
	req := struct {
		AppID          uint32       `url:"appid"`
		Currency       CurrencyCode `url:"currency"`
		MarketHashName string       `url:"market_hash_name"`
	}{appID, m.config.Currency, marketHashName}
	return community.Get[PriceOverviewResponse](
		ctx, m.community, "market/priceoverview", req, withMarketHeaders(""),
	)
}

// GetItemOrdersHistogram gets a histogram of active buy and sell orders.
// The itemNameID can be obtained by parsing the lot page (usually cached by the bot).
func (m *Manager) GetItemOrdersHistogram(ctx context.Context, appID uint32, marketHashName string, itemNameID uint64) (*ItemOrdersHistogram, error) {
	referer := fmt.Sprintf("https://steamcommunity.com/market/listings/%d/%s", appID, url.PathEscape(marketHashName))

	req := struct {
		Country    string       `url:"country"`
		Language   string       `url:"language"`
		Currency   CurrencyCode `url:"currency"`
		ItemNameID uint64       `url:"item_nameid"`
		TwoFactor  int          `url:"two_factor"`
	}{m.config.Country, m.config.Language, m.config.Currency, itemNameID, 0}

	resp, err := community.Get[ItemOrdersHistogramResponse](ctx, m.community, "market/itemordershistogram", req, withMarketHeaders(referer))
	if err != nil {
		return nil, err
	}

	histogram := &ItemOrdersHistogram{
		SellOrderTable:   resp.SellOrderTable,
		SellOrderSummary: resp.SellOrderSummary,
		BuyOrderTable:    resp.BuyOrderTable,
		BuyOrderSummary:  resp.BuyOrderSummary,
		BuyOrderGraph:    resp.BuyOrderGraph,
		SellOrderGraph:   resp.SellOrderGraph,
		GraphMaxY:        resp.GraphMaxY,
		GraphMinX:        resp.GraphMinX,
		GraphMaxX:        resp.GraphMaxX,
		PricePrefix:      resp.PricePrefix,
		PriceSuffix:      resp.PriceSuffix,
	}

	if resp.HighestBuyOrder != "" {
		histogram.HighestBuyOrder, _ = strconv.ParseFloat(resp.HighestBuyOrder, 64)
	}
	if resp.LowestSellOrder != "" {
		histogram.LowestSellOrder, _ = strconv.ParseFloat(resp.LowestSellOrder, 64)
	}

	return histogram, nil
}

// GetMyListings gets the active lots and orders of an account.
func (m *Manager) GetMyListings(ctx context.Context, start, count int) (*MyListingsResponse, error) {
	if count == 0 {
		count = 100
	}
	req := struct {
		Start    int `url:"start"`
		Count    int `url:"count"`
		NoRender int `url:"norender"`
	}{start, count, 1}
	return community.Get[MyListingsResponse](ctx, m.community, "market/mylistings", req, withMarketHeaders(""))
}

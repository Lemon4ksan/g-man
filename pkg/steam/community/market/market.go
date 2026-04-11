// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package market provides functionality to interact with the Steam Community Market,
// including creating buy/sell orders and managing listings.
package market

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"sync"

	"github.com/lemon4ksan/g-man/pkg/log"
	"github.com/lemon4ksan/g-man/pkg/steam"
	"github.com/lemon4ksan/g-man/pkg/steam/api"
	"github.com/lemon4ksan/g-man/pkg/steam/community"
	"github.com/lemon4ksan/g-man/pkg/steam/id"
	"github.com/lemon4ksan/g-man/pkg/steam/module"
	"github.com/lemon4ksan/g-man/pkg/steam/service"
	tr "github.com/lemon4ksan/g-man/pkg/steam/transport"
)

const ModuleName string = "market"

func WithModule(cfg Config) steam.Option {
	return func(c *steam.Client) {
		c.RegisterModule(New(cfg))
	}
}

// Config contains settings for requests to the Trading Platform.
type Config struct {
	Currency CurrencyCode
	Country  string // e.g. "US", "RU"
	Language string // e.g. "english", "russian"
}

// DefaultConfig returns the default settings (USD, US, english).
func DefaultConfig() Config {
	return Config{
		Currency: CurrencyCodeUSD,
		Country:  "US",
		Language: "english",
	}
}

// Market manages interactions with the Steam Community Market.
// It embeds modules.BaseModule for lifecycle and logging consistency.
type Market struct {
	module.Base

	config    Config
	community community.Requester

	mu      sync.RWMutex
	steamID id.ID
}

// New creates a new Market module instance.
func New(cfg Config) *Market {
	return &Market{
		Base:   module.New(ModuleName),
		config: cfg,
	}
}

// Init initializes the module dependencies.
func (m *Market) Init(init module.InitContext) error {
	return m.Base.Init(init)
}

// StartAuthed is called when a community session is established.
// It captures the authenticated community requester and the user's SteamID.
func (m *Market) StartAuthed(ctx context.Context, auth module.AuthContext) error {
	m.mu.Lock()
	m.community = auth.Community()
	m.steamID = auth.SteamID()
	m.mu.Unlock()

	m.Logger.Info("Market module ready",
		log.Int("currency", int(m.config.Currency)),
		log.SteamID(m.steamID.Uint64()),
	)

	return nil
}

// Close ensures the module is shut down correctly.
func (m *Market) Close() error {
	return m.Base.Close()
}

// CreateSellOrder places an item from the user's inventory onto the market.
// The price should be in the smallest currency unit (e.g., cents/kopecks)
// and represents the amount the seller receives.
func (m *Market) CreateSellOrder(ctx context.Context, opts CreateSellOrderOptions) (*CreateSellOrder, error) {
	m.mu.RLock()
	comm := m.community
	myID := m.steamID
	m.mu.RUnlock()

	if comm == nil {
		return nil, module.ErrNotAuthenticated
	}

	sessionID := comm.SessionID(community.BaseURL)
	referer := fmt.Sprintf("%sprofiles/%d/inventory?modal=1&market=1", community.BaseURL, myID)

	req := struct {
		SessionID string `url:"sessionid"`
		AppID     uint32 `url:"appid"`
		ContextID int64  `url:"contextid"`
		AssetID   uint64 `url:"assetid"`
		Amount    int    `url:"amount"`
		Price     int    `url:"price"`
	}{sessionID, opts.AppID, opts.ContextID, opts.AssetID, opts.Amount, opts.Price}

	resp, err := community.PostForm[CreateSellOrderResponse](ctx, comm, "market/sellitem", req,
		withMarketHeaders(referer),
		withOrigin(),
	)
	if err != nil {
		return nil, fmt.Errorf("market: sell order failed: %w", err)
	}

	return &CreateSellOrder{
		Success:                 resp.Success,
		RequiresConfirmation:    resp.RequiresConfirmation == 1,
		NeedsMobileConfirmation: resp.NeedsMobileConfirmation,
		NeedsEmailConfirmation:  resp.NeedsEmailConfirmation,
		EmailDomain:             resp.EmailDomain,
	}, nil
}

// CreateBuyOrder creates a buy order (buy order) for a specific item.
func (m *Market) CreateBuyOrder(ctx context.Context, opts CreateBuyOrderOptions) (*CreateBuyOrderResponse, error) {
	m.mu.RLock()
	comm := m.community
	m.mu.RUnlock()

	if comm == nil {
		return nil, module.ErrNotAuthenticated
	}

	sessionID := comm.SessionID(community.BaseURL)
	referer := fmt.Sprintf(
		"%smarket/listings/%d/%s",
		community.BaseURL,
		opts.AppID,
		url.PathEscape(opts.MarketHashName),
	)

	// Format price to Steam's expected decimal string (e.g., "1.50")
	totalCents := opts.Price * opts.Amount
	priceTotalStr := formatCurrencyDecimal(totalCents, m.config.Currency)

	req := struct {
		SessionID      string       `url:"sessionid"`
		AppID          uint32       `url:"appid"`
		Currency       CurrencyCode `url:"currency"`
		MarketHashName string       `url:"market_hash_name"`
		PriceTotal     string       `url:"price_total"`
		Quantity       int          `url:"quantity"`
		BillingState   string       `url:"billing_state"`
		SaveMyAddress  string       `url:"save_my_address"`
	}{
		SessionID:      sessionID,
		AppID:          opts.AppID,
		Currency:       m.config.Currency,
		MarketHashName: opts.MarketHashName,
		PriceTotal:     priceTotalStr,
		Quantity:       opts.Amount,
		BillingState:   "",
		SaveMyAddress:  "0",
	}

	resp, err := community.PostForm[CreateBuyOrderResponse](ctx, comm, "market/createbuyorder", req,
		withMarketHeaders(referer),
		withOrigin(),
	)
	if err != nil {
		return nil, fmt.Errorf("market: buy order failed: %w", err)
	}

	return resp, nil
}

// CancelBuyOrder cancels an existing active buy order.
func (m *Market) CancelBuyOrder(ctx context.Context, buyOrderID uint64) error {
	m.mu.RLock()
	comm := m.community
	m.mu.RUnlock()

	if comm == nil {
		return module.ErrNotAuthenticated
	}

	req := struct {
		SessionID  string `url:"sessionid"`
		BuyOrderID uint64 `url:"buy_orderid"`
	}{comm.SessionID(community.BaseURL), buyOrderID}

	_, err := community.PostForm[service.NoResponse](
		ctx,
		comm,
		"market/cancelbuyorder",
		req,
		withMarketHeaders(""),
		withOrigin(),
	)

	return err
}

// CancelSellOrder removes an item from sale on the market.
func (m *Market) CancelSellOrder(ctx context.Context, listingID uint64) error {
	m.mu.RLock()
	comm := m.community
	m.mu.RUnlock()

	if comm == nil {
		return module.ErrNotAuthenticated
	}

	req := struct {
		SessionID string `url:"sessionid"`
	}{comm.SessionID(community.BaseURL)}

	path := fmt.Sprintf("market/removelisting/%d", listingID)
	_, err := community.PostForm[service.NoResponse](ctx, comm, path, req, withMarketHeaders(""), withOrigin())

	return err
}

// Search searches for items on the marketplace.
func (m *Market) Search(ctx context.Context, appID uint32, opts SearchOptions) (*SearchResponse, error) {
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
func (m *Market) GetPriceOverview(
	ctx context.Context,
	appID uint32,
	marketHashName string,
) (*PriceOverviewResponse, error) {
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
func (m *Market) GetItemOrdersHistogram(
	ctx context.Context,
	appID uint32,
	marketHashName string,
	itemNameID uint64,
) (*ItemOrdersHistogram, error) {
	referer := fmt.Sprintf("https://steamcommunity.com/market/listings/%d/%s", appID, url.PathEscape(marketHashName))

	req := struct {
		Country    string       `url:"country"`
		Language   string       `url:"language"`
		Currency   CurrencyCode `url:"currency"`
		ItemNameID uint64       `url:"item_nameid"`
		TwoFactor  int          `url:"two_factor"`
	}{m.config.Country, m.config.Language, m.config.Currency, itemNameID, 0}

	resp, err := community.Get[ItemOrdersHistogramResponse](
		ctx,
		m.community,
		"market/itemordershistogram",
		req,
		withMarketHeaders(referer),
	)
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
func (m *Market) GetMyListings(ctx context.Context, start, count int) (*MyListingsResponse, error) {
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

func formatCurrencyDecimal(cents int, currency CurrencyCode) string {
	// Some currencies don't use decimals (JPY, KRW, VND)
	switch currency {
	case CurrencyCodeJPY, CurrencyCodeKRW, CurrencyCodeVND:
		return strconv.Itoa(cents)
	default:
		return fmt.Sprintf("%.2f", float64(cents)/100.0)
	}
}

// withMarketHeaders injects headers required for Steam Market AJAX calls.
func withMarketHeaders(referer string) api.CallOption {
	return func(req *tr.Request, _ *api.CallConfig) {
		req.WithHeader("X-Requested-With", "XMLHttpRequest")
		req.WithHeader("X-Prototype-Version", "1.7")

		if referer != "" {
			req.WithHeader("Referer", referer)
		} else {
			req.WithHeader("Referer", community.BaseURL+"market/")
		}
	}
}

func withOrigin() api.CallOption {
	return func(req *tr.Request, _ *api.CallConfig) {
		req.WithHeader("Origin", "https://steamcommunity.com")
	}
}

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
	"github.com/lemon4ksan/g-man/pkg/modules"
	"github.com/lemon4ksan/g-man/pkg/steam"
	"github.com/lemon4ksan/g-man/pkg/steam/api"
	"github.com/lemon4ksan/g-man/pkg/steam/community"
	tr "github.com/lemon4ksan/g-man/pkg/steam/transport"
)

const ModuleName string = "market"

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

func WithModule(cfg Config) steam.Option {
    return func(c *steam.Client) {
        c.RegisterModule(New(cfg))
    }
}

// Manager manages interactions with the Steam Community Market.
// It embeds modules.BaseModule for lifecycle and logging consistency.
type Manager struct {
	modules.BaseModule

	config    Config
	community community.Requester

	mu      sync.RWMutex
	steamID uint64
}

// New creates a new Market module instance.
func New(cfg Config) *Manager {
	return &Manager{
		BaseModule: modules.NewBase(ModuleName),
		config:     cfg,
	}
}

// Init initializes the module dependencies.
func (m *Manager) Init(init modules.InitContext) error {
	return m.BaseModule.Init(init)
}

// StartAuthed is called when a community session is established.
// It captures the authenticated community requester and the user's SteamID.
func (m *Manager) StartAuthed(ctx context.Context, auth modules.AuthContext) error {
	m.mu.Lock()
	m.community = auth.Community()
	m.steamID = auth.SteamID()
	m.mu.Unlock()

	m.Logger.Info("Market module ready",
		log.Int("currency", int(m.config.Currency)),
		log.Uint64("steam_id", m.steamID),
	)
	return nil
}

// Close ensures the module is shut down correctly.
func (m *Manager) Close() error {
	return m.BaseModule.Close()
}

// CreateSellOrder places an item from the user's inventory onto the market.
// The price should be in the smallest currency unit (e.g., cents/kopecks)
// and represents the amount the seller receives.
func (m *Manager) CreateSellOrder(ctx context.Context, opts CreateSellOrderOptions) (*CreateSellOrder, error) {
	m.mu.RLock()
	comm := m.community
	myID := m.steamID
	m.mu.RUnlock()

	if comm == nil {
		return nil, modules.ErrNotAuthenticated
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
func (m *Manager) CreateBuyOrder(ctx context.Context, opts CreateBuyOrderOptions) (*CreateBuyOrderResponse, error) {
	m.mu.RLock()
	comm := m.community
	m.mu.RUnlock()

	if comm == nil {
		return nil, modules.ErrNotAuthenticated
	}

	sessionID := comm.SessionID(community.BaseURL)
	referer := fmt.Sprintf("%smarket/listings/%d/%s", community.BaseURL, opts.AppID, url.PathEscape(opts.MarketHashName))

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
func (m *Manager) CancelBuyOrder(ctx context.Context, buyOrderID uint64) error {
	m.mu.RLock()
	comm := m.community
	m.mu.RUnlock()

	if comm == nil {
		return modules.ErrNotAuthenticated
	}

	req := struct {
		SessionID  string `url:"sessionid"`
		BuyOrderID uint64 `url:"buy_orderid"`
	}{comm.SessionID(community.BaseURL), buyOrderID}

	_, err := community.PostForm[any](ctx, comm, "market/cancelbuyorder", req, withMarketHeaders(""), withOrigin())
	return err
}

// CancelSellOrder removes an item from sale on the market.
func (m *Manager) CancelSellOrder(ctx context.Context, listingID uint64) error {
	m.mu.RLock()
	comm := m.community
	m.mu.RUnlock()

	if comm == nil {
		return modules.ErrNotAuthenticated
	}

	req := struct {
		SessionID string `url:"sessionid"`
	}{comm.SessionID(community.BaseURL)}

	path := fmt.Sprintf("market/removelisting/%d", listingID)
	_, err := community.PostForm[any](ctx, comm, path, req, withMarketHeaders(""), withOrigin())
	return err
}

// --- Helpers ---

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

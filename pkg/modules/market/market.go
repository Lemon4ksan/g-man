// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package market

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"sync"

	"github.com/lemon4ksan/g-man/pkg/log"
	"github.com/lemon4ksan/g-man/pkg/steam"
	"github.com/lemon4ksan/g-man/pkg/steam/api"
	"github.com/lemon4ksan/g-man/pkg/steam/bus"
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

// Manager manages interaction with the Steam Community Market.
type Manager struct {
	config    Config
	bus       *bus.Bus
	logger    log.Logger
	community api.CommunityRequester
	steamID   uint64

	mu        sync.RWMutex
	closeFunc func()
}

// New creates a new Market module.
func New(cfg Config) *Manager {
	return &Manager{
		config: cfg,
		logger: log.Discard,
	}
}

func (m *Manager) Name() string { return ModuleName }

func (m *Manager) Init(init steam.InitContext) error {
	m.bus = init.Bus()
	if m.bus == nil {
		return errors.New("market: nil bus")
	}
	m.logger = init.Logger().WithModule(ModuleName)

	return nil
}

func (m *Manager) Start(ctx context.Context) error {
	return nil
}

func (m *Manager) StartAuthed(ctx context.Context, auth steam.AuthContext) error {
	m.community = auth.Community()
	if m.community == nil {
		return errors.New("market: nil community client")
	}
	m.steamID = auth.SteamID()
	m.logger.Info("Market module successfully authenticated", log.Int("currency", int(m.config.Currency)))
	return nil
}

func (m *Manager) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closeFunc != nil {
		m.closeFunc()
		m.closeFunc = nil
	}
	return nil
}

// CreateSellOrder lists an item for sale.
// The price is given in kopecks/cents, excluding the Steam commission (the commission is added automatically).
func (m *Manager) CreateSellOrder(ctx context.Context, appID uint32, opts CreateSellOrderOptions) (*CreateSellOrder, error) {
	sessionID := m.community.SessionID(api.CommunityBase)

	data := url.Values{
		"sessionid": {sessionID},
		"appid":     {strconv.FormatUint(uint64(appID), 10)},
		"contextid": {strconv.FormatInt(opts.ContextID, 10)},
		"assetid":   {strconv.FormatUint(opts.AssetID, 10)},
		"amount":    {strconv.Itoa(opts.Amount)},
		"price":     {strconv.Itoa(opts.Price)},
	}

	referer := fmt.Sprintf("https://steamcommunity.com/profiles/%d/inventory?modal=1&market=1", m.steamID)

	resp, err := m.community.PostForm(ctx, "market/sellitem", data, withMarketHeaders(referer), withOrigin())
	if err != nil {
		return nil, err
	}

	var response CreateSellOrderResponse
	if err := json.Unmarshal(resp.Body, &response); err != nil {
		return nil, fmt.Errorf("failed to parse sell order response: %w", err)
	}

	return &CreateSellOrder{
		Success:                 response.Success,
		RequiresConfirmation:    response.RequiresConfirmation == 1,
		NeedsMobileConfirmation: response.NeedsMobileConfirmation,
		NeedsEmailConfirmation:  response.NeedsEmailConfirmation,
		EmailDomain:             response.EmailDomain,
	}, nil
}

// CreateBuyOrder places an order to automatically purchase an item (Buy Order).
func (m *Manager) CreateBuyOrder(ctx context.Context, appID uint32, opts CreateBuyOrderOptions) (*CreateBuyOrderResponse, error) {
	sessionID := m.community.SessionID(api.CommunityBase)

	// Steam expects price_total to be a floating-point string, depending on the currency.
	// For example, for USD (1), it's "1.50" (if the price is 150 cents). For RUB (5), it's "150.00".
	totalCents := opts.Price * opts.Amount
	priceTotalStr := formatCurrencyDecimal(totalCents, m.config.Currency)

	data := url.Values{
		"sessionid":        {sessionID},
		"currency":         {strconv.Itoa(int(m.config.Currency))},
		"appid":            {strconv.FormatUint(uint64(appID), 10)},
		"market_hash_name": {opts.MarketHashName},
		"price_total":      {priceTotalStr},
		"quantity":         {strconv.Itoa(opts.Amount)},
		"billing_state":    {""},
		"save_my_address":  {"0"},
	}

	referer := fmt.Sprintf("https://steamcommunity.com/market/listings/%d/%s", appID, url.PathEscape(opts.MarketHashName))

	resp, err := m.community.PostForm(ctx, "market/createbuyorder", data, withMarketHeaders(referer), withOrigin())
	if err != nil {
		return nil, err
	}

	var response CreateBuyOrderResponse
	if err := json.Unmarshal(resp.Body, &response); err != nil {
		return nil, fmt.Errorf("failed to parse buy order response: %w", err)
	}

	return &response, nil
}

// CancelBuyOrder cancels an active buy order.
func (m *Manager) CancelBuyOrder(ctx context.Context, buyOrderID uint64) error {
	sessionID := m.community.SessionID(api.CommunityBase)

	data := url.Values{
		"sessionid":   {sessionID},
		"buy_orderid": {strconv.FormatUint(buyOrderID, 10)},
	}

	_, err := m.community.PostForm(ctx, "market/cancelbuyorder", data, withMarketHeaders(""), withOrigin())
	if err != nil {
		return err
	}

	return nil
}

// CancelSellOrder removes the item from sale.
func (m *Manager) CancelSellOrder(ctx context.Context, listingID uint64) error {
	sessionID := m.community.SessionID(api.CommunityBase)

	data := url.Values{
		"sessionid": {sessionID},
	}

	path := fmt.Sprintf("market/removelisting/%d", listingID)
	_, err := m.community.PostForm(ctx, path, data, withMarketHeaders(""), withOrigin())
	if err != nil {
		return err
	}

	return nil
}

func formatCurrencyDecimal(cents int, currency CurrencyCode) string {
	switch currency {
	case CurrencyCodeJPY, CurrencyCodeKRW, CurrencyCodeVND:
		return strconv.Itoa(cents)
	default:
		return fmt.Sprintf("%.2f", float64(cents)/100.0)
	}
}

func withMarketHeaders(referer string) api.RequestModifier {
	return func(req *tr.Request) {
		req.WithHeader("X-Requested-With", "XMLHttpRequest")
		req.WithHeader("X-Prototype-Version", "1.7")
		if referer != "" {
			req.WithHeader("Referer", referer)
		} else {
			req.WithHeader("Referer", "https://steamcommunity.com/market/")
		}
	}
}

func withOrigin() api.RequestModifier {
	return func(req *tr.Request) {
		req.WithHeader("Origin", "https://steamcommunity.com")
	}
}

// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package trading

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"sync"
	"time"

	"golang.org/x/time/rate"

	"github.com/lemon4ksan/g-man/log"
	"github.com/lemon4ksan/g-man/modules/econ"
	"github.com/lemon4ksan/g-man/steam"
	"github.com/lemon4ksan/g-man/steam/api"
)

const ModuleName = "trading"

type Config struct {
	PollInterval time.Duration
	CancelTime   time.Duration
	Language     string
	APIKeyDomain string // Domain to register WebAPI Key if missing (e.g. "localhost")
}

func DefaultConfig() Config {
	return Config{
		PollInterval: 30 * time.Second,
		Language:     "english",
		APIKeyDomain: "localhost",
	}
}

type Manager struct {
	client *steam.Client
	logger log.Logger
	config Config
	cache  *AssetCache

	mu          sync.RWMutex
	lastPoll    time.Time
	knownOffers map[uint64]econ.TradeOfferState

	// Offers that we saw last time. Used for GC (Garbage Collection of knownOffers).
	lastSeenOffers map[uint64]time.Time

	rateLimiter *rate.Limiter
}

func New(cfg Config, logger log.Logger) *Manager {
	if cfg.PollInterval < 1*time.Second {
		cfg.PollInterval = 30 * time.Second
	}

	return &Manager{
		config:         cfg,
		cache:          NewAssetCache(),
		logger:         logger,
		knownOffers:    make(map[uint64]econ.TradeOfferState),
		lastSeenOffers: make(map[uint64]time.Time),
		rateLimiter:    rate.NewLimiter(rate.Every(2*time.Second), 1),
	}
}

func (m *Manager) Name() string { return ModuleName }

func (m *Manager) Init(c *steam.Client) error {
	m.client = c
	return nil
}

func (m *Manager) Start(ctx context.Context) error {
	// Ensure we have an API Key before polling starts
	// This is async, so pollLoop handles the wait.
	go m.pollLoop(ctx)
	return nil
}

// AcceptOffer accepts a trade offer.
func (m *Manager) AcceptOffer(ctx context.Context, offerID uint64) error {
	if err := m.rateLimiter.Wait(ctx); err != nil {
		return err
	}

	params := url.Values{}
	params.Set("tradeofferid", strconv.FormatUint(offerID, 10))
	params.Set("serverid", "1")

	// AcceptTradeOffer usually returns 200 OK with empty body or a tradeid
	return m.client.API().CallWebAPI(ctx, "POST", "IEconService", "AcceptTradeOffer", 1, nil, api.WithParams(params))
}

// DeclineOffer declines a trade offer.
func (m *Manager) DeclineOffer(ctx context.Context, offerID uint64) error {
	if err := m.rateLimiter.Wait(ctx); err != nil {
		return err
	}

	params := url.Values{}
	params.Set("tradeofferid", strconv.FormatUint(offerID, 10))

	return m.client.API().CallWebAPI(ctx, "POST", "IEconService", "DeclineTradeOffer", 1, nil, api.WithParams(params))
}

// CancelOffer cancels a trade offer sent by us.
func (m *Manager) CancelOffer(ctx context.Context, offerID uint64) error {
	if err := m.rateLimiter.Wait(ctx); err != nil {
		return err
	}

	params := url.Values{}
	params.Set("tradeofferid", strconv.FormatUint(offerID, 10))

	return m.client.API().CallWebAPI(ctx, "POST", "IEconService", "CancelTradeOffer", 1, nil, api.WithParams(params))
}

// GetOffer fetches details for a single offer.
func (m *Manager) GetOffer(ctx context.Context, offerID uint64) (*TradeOffer, error) {
	if err := m.rateLimiter.Wait(ctx); err != nil {
		return nil, err
	}

	params := url.Values{}
	params.Set("tradeofferid", strconv.FormatUint(offerID, 10))
	params.Set("language", m.config.Language)

	var resp struct {
		Response struct {
			Offer *TradeOffer `json:"offer"`
		} `json:"response"`
	}

	err := m.client.API().CallWebAPI(ctx, "GET", "IEconService", "GetTradeOffer", 1, &resp, api.WithParams(params))
	if err != nil {
		return nil, err
	}

	if resp.Response.Offer == nil {
		return nil, fmt.Errorf("offer %d not found", offerID)
	}

	return resp.Response.Offer, nil
}

func (m *Manager) pollLoop(ctx context.Context) {
	// Wait until client is connected and ready
	ticker := time.NewTicker(m.config.PollInterval)
	defer ticker.Stop()

	// Initial check for API Key
	if err := m.ensureAPIKey(ctx); err != nil {
		m.logger.Error("Failed to ensure API Key", log.Err(err))
		// We continue anyway, hoping it might work or be fixed later
	}

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if m.client.State() != steam.StateRunning {
				continue
			}
			m.doPoll(ctx)
		}
	}
}

func (m *Manager) doPoll(ctx context.Context) {
	if err := m.rateLimiter.Wait(ctx); err != nil {
		return
	}

	params := url.Values{}
	params.Set("get_received_offers", "1")
	params.Set("get_sent_offers", "1")
	params.Set("active_only", "1")
	params.Set("get_descriptions", "0")
	params.Set("time_historical_cutoff", strconv.FormatInt(time.Now().Add(-24*time.Hour).Unix(), 10))

	var resp struct {
		Response struct {
			Sent     []*TradeOffer `json:"trade_offers_sent"`
			Received []*TradeOffer `json:"trade_offers_received"`
		} `json:"response"`
	}

	err := m.client.API().CallWebAPI(ctx, "GET", "IEconService", "GetTradeOffers", 1, &resp, api.WithParams(params))
	if err != nil {
		m.logger.Warn("Poll failed", log.Err(err))
		return
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	now := time.Now()
	allOffers := append(resp.Response.Sent, resp.Response.Received...)

	for _, offer := range allOffers {
		m.lastSeenOffers[offer.ID] = now

		oldState, exists := m.knownOffers[offer.ID]

		if !exists {
			m.knownOffers[offer.ID] = offer.State
			if offer.State == econ.TradeOfferStateActive {
				m.logger.Info("New offer detected", log.Uint64("id", offer.ID))
				m.client.Bus().Publish(&NewOfferEvent{Offer: offer})
			}
		} else if oldState != offer.State {
			m.knownOffers[offer.ID] = offer.State
			m.client.Bus().Publish(&OfferChangedEvent{
				Offer:    offer,
				OldState: oldState,
			})
		}
	}

	// If we haven't seen an offer for > 1 hour (and it's not active), forget it.
	// This prevents knownOffers from growing infinitely.
	for id, lastSeen := range m.lastSeenOffers {
		if now.Sub(lastSeen) > 1*time.Hour {
			if state, ok := m.knownOffers[id]; ok && state != econ.TradeOfferStateActive {
				delete(m.knownOffers, id)
				delete(m.lastSeenOffers, id)
			}
		}
	}

	m.client.Bus().Publish(&PollSuccessEvent{})
}

func (m *Manager) ensureAPIKey(ctx context.Context) error {
	// Simple check using Community Client to see if we have a key
	// If not, register one using m.config.APIKeyDomain
	// (Implementation depends on Community Client having a GetWebAPIKey method)
	return nil
}

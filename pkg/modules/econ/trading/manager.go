// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package trading

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/time/rate"

	"github.com/lemon4ksan/g-man/pkg/log"
	"github.com/lemon4ksan/g-man/pkg/modules/auth"
	"github.com/lemon4ksan/g-man/pkg/modules/econ"
	"github.com/lemon4ksan/g-man/pkg/steam"
	"github.com/lemon4ksan/g-man/pkg/steam/api"
	"github.com/lemon4ksan/g-man/pkg/steam/bus"
)

const ModuleName = "trading"

var (
	ErrManagerClosed  = errors.New("trade: closed")
	ErrManagerPolling = errors.New("trade: already polling")
)

type State int32

const (
	StateStopped State = iota
	StatePolling
	StateClosed
)

func (s State) String() string {
	switch s {
	case StateStopped:
		return "stopped"
	case StatePolling:
		return "polling"
	case StateClosed:
		return "closed"
	default:
		return "unknown"
	}
}

type Config struct {
	PollInterval time.Duration
	CancelTime   time.Duration
	Language     string
}

func DefaultConfig() Config {
	return Config{
		PollInterval: 30 * time.Second,
		Language:     "english",
	}
}

type Manager struct {
	bus       *bus.Bus
	sub       *bus.Subscription
	processor *Processor

	web       api.WebAPIRequester
	community *api.CommunityClient
	logger    log.Logger
	config    Config
	cache     *AssetCache
	state     atomic.Int32

	mu            sync.RWMutex
	wg            sync.WaitGroup
	pollingCtx    context.Context
	pollingCancel context.CancelFunc
	lastPoll      time.Time
	knownOffers   map[uint64]econ.TradeOfferState

	// Offers that we saw last time. Used for Garbage Collection of knownOffers.
	lastSeenOffers map[uint64]time.Time

	rateLimiter *rate.Limiter
}

func New(cfg Config) *Manager {
	if cfg.PollInterval < 1*time.Second {
		cfg.PollInterval = 30 * time.Second
	}

	return &Manager{
		config:         cfg,
		logger:         log.Discard,
		cache:          NewAssetCache(),
		knownOffers:    make(map[uint64]econ.TradeOfferState),
		lastSeenOffers: make(map[uint64]time.Time),
		rateLimiter:    rate.NewLimiter(rate.Every(2*time.Second), 1),
	}
}

func (m *Manager) Name() string { return ModuleName }

func (m *Manager) Init(init steam.InitContext) error {
	m.bus = init.Bus()
	if m.bus == nil {
		return errors.New("nil bus")
	}
	m.web = init.Proto()
	if m.web == nil {
		return errors.New("nil proto client")
	}
	m.logger = init.Logger().WithModule(ModuleName)

	m.sub = init.Bus().Subscribe(auth.StateEvent{})
	go m.listenEvents()

	return nil
}

func (m *Manager) Start(ctx context.Context) error {
	return nil
}

func (m *Manager) StartAuthed(ctx context.Context, auth steam.AuthContext) error {
	m.mu.Lock()
	m.community = auth.Community()
	if m.pollingCancel != nil {
		m.pollingCancel()
		m.pollingCancel = nil
	}
	m.mu.Unlock()

	return m.StartPolling(ctx)
}

// SetOfferHandler registers the business logic and starts the processor.
// Call this from your main bot code during initialization.
func (m *Manager) SetOfferHandler(ctx context.Context, handler OfferHandler) {
	m.processor = NewProcessor(m, handler, m.logger)
	m.processor.Start(ctx)
}

// Close stops the polling process and discards this module.
func (m *Manager) Close() error {
	m.state.Store(int32(StateClosed))

	m.mu.Lock()
	defer m.mu.Unlock()

	if m.pollingCancel != nil {
		m.pollingCancel()
	}

	if m.sub != nil {
		m.sub.Unsubscribe()
	}

	return nil
}

func (m *Manager) StartPolling(ctx context.Context) error {
	if !m.state.CompareAndSwap(int32(StateStopped), int32(StatePolling)) {
		return ErrManagerPolling
	}

	m.mu.Lock()
	m.pollingCtx, m.pollingCancel = context.WithCancel(ctx)
	m.mu.Unlock()

	m.wg.Add(1)
	go m.pollingLoop()

	m.logger.Info("Polling started", log.Duration("interval", m.config.PollInterval))
	m.bus.Publish(&StateEvent{New: StatePolling})
	return nil
}

// StopPolling stops the automatic confirmation polling.
func (m *Manager) StopPolling() {
	if m.state.CompareAndSwap(int32(StatePolling), int32(StateStopped)) {
		m.mu.Lock()
		if m.pollingCancel != nil {
			m.pollingCancel()
		}
		m.mu.Unlock()

		m.wg.Wait() // Wait for the loop to exit cleanly
		m.logger.Info("Polling stopped")
		m.bus.Publish(&StateEvent{New: StateStopped})
	}
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
	return m.web.CallWebAPI(ctx, "POST", "IEconService", "AcceptTradeOffer", 1, nil, api.WithParams(params))
}

// DeclineOffer declines a trade offer.
func (m *Manager) DeclineOffer(ctx context.Context, offerID uint64) error {
	if err := m.rateLimiter.Wait(ctx); err != nil {
		return err
	}

	params := url.Values{}
	params.Set("tradeofferid", strconv.FormatUint(offerID, 10))

	return m.web.CallWebAPI(ctx, "POST", "IEconService", "DeclineTradeOffer", 1, nil, api.WithParams(params))
}

// CancelOffer cancels a trade offer sent by us.
func (m *Manager) CancelOffer(ctx context.Context, offerID uint64) error {
	if err := m.rateLimiter.Wait(ctx); err != nil {
		return err
	}

	params := url.Values{}
	params.Set("tradeofferid", strconv.FormatUint(offerID, 10))

	return m.web.CallWebAPI(ctx, "POST", "IEconService", "CancelTradeOffer", 1, nil, api.WithParams(params))
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

	err := m.web.CallWebAPI(ctx, "GET", "IEconService", "GetTradeOffer", 1, &resp, api.WithParams(params))
	if err != nil {
		return nil, err
	}

	if resp.Response.Offer == nil {
		return nil, fmt.Errorf("offer %d not found", offerID)
	}

	return resp.Response.Offer, nil
}

// IsItemInTrade allows external modules to know whether an item in the offer is already occupied.
func (m *Manager) IsItemInTrade(assetID uint64) bool {
	if m.processor == nil {
		return false
	}
	return m.processor.IsInTrade(assetID)
}

func (m *Manager) pollingLoop() {
	defer m.wg.Done()

	interval := m.config.PollInterval
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-m.pollingCtx.Done():
			return
		case <-ticker.C:
			m.doPoll()
		}
	}
}

func (m *Manager) doPoll() {
	if err := m.rateLimiter.Wait(m.pollingCtx); err != nil {
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

	err := m.web.CallWebAPI(m.pollingCtx, "GET", "IEconService", "GetTradeOffers", 1, &resp, api.WithParams(params))
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
				m.bus.Publish(&NewOfferEvent{Offer: offer})

				if m.processor != nil {
					m.processor.Enqueue(offer)
				}
			}
		} else if oldState != offer.State {
			m.knownOffers[offer.ID] = offer.State
			m.bus.Publish(&OfferChangedEvent{
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

	m.bus.Publish(&PollSuccessEvent{})
}

func (m *Manager) listenEvents() {
	for ev := range m.sub.C() {
		switch e := ev.(type) {
		case *auth.StateEvent:
			m.handleStateChange(e)
		}
	}
}

func (m *Manager) handleStateChange(e *auth.StateEvent) {
	switch e.New {
	case auth.StateLoggedOn:
	case auth.StateDisconnected:
		m.logger.Warn("Disconnected, stopping session context")
		m.mu.Lock()
		if m.pollingCancel != nil {
			m.pollingCancel()
		}
		m.mu.Unlock()
	}
}

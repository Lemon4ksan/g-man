// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package trading

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"golang.org/x/time/rate"

	"github.com/lemon4ksan/g-man/pkg/bus"
	"github.com/lemon4ksan/g-man/pkg/log"
	"github.com/lemon4ksan/g-man/pkg/steam"
	"github.com/lemon4ksan/g-man/pkg/steam/auth"
	"github.com/lemon4ksan/g-man/pkg/steam/community"
	"github.com/lemon4ksan/g-man/pkg/steam/module"
	"github.com/lemon4ksan/g-man/pkg/steam/service"
	schema "github.com/lemon4ksan/g-man/pkg/tf2/schema"
	"github.com/lemon4ksan/g-man/pkg/trading"
)

const ModuleName = "trading"

var (
	ErrManagerClosed  = errors.New("trade: closed")
	ErrManagerPolling = errors.New("trade: already polling")
)

// State constants representing the module lifecycle.
type State int32

const (
	StateStopped int32 = iota
	StatePolling
	StateClosed
)

func (s State) String() string {
	switch int32(s) {
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

func WithModule(cfg Config) steam.Option {
	return func(c *steam.Client) {
		c.RegisterModule(New(cfg))
	}
}

type SchemaProvider interface {
	Get() *schema.Schema
}

// Manager handles trade offer synchronization, polling, and state tracking.
// It integrates with a Processor to handle business logic for individual offers.
type Manager struct {
	module.Base

	// Dependencies
	web       service.Doer
	community community.Requester
	processor *Processor
	schema    SchemaProvider

	config Config
	cache  *AssetCache

	// Polling synchronization
	mu             sync.RWMutex
	knownOffers    map[uint64]trading.OfferState
	lastSeenOffers map[uint64]time.Time

	rateLimiter *rate.Limiter
}

// New creates a new instance of the trade manager.
func New(cfg Config) *Manager {
	if cfg.PollInterval < 1*time.Second {
		cfg.PollInterval = 30 * time.Second
	}

	return &Manager{
		Base:           module.New(ModuleName),
		config:         cfg,
		cache:          NewAssetCache(),
		knownOffers:    make(map[uint64]trading.OfferState),
		lastSeenOffers: make(map[uint64]time.Time),
		rateLimiter:    rate.NewLimiter(rate.Every(2*time.Second), 1),
	}
}

func (m *Manager) Init(init module.InitContext) error {
	if err := m.Base.Init(init); err != nil {
		return err
	}

	m.web = init.Service()

	schemaMod := init.Module("tf2_schema")
	if schemaMod == nil {
		m.Logger.Warn("tf2_schema module not found, cannot resolve SKUs")
	}

	return nil
}

func (m *Manager) StartAuthed(ctx context.Context, authCtx module.AuthContext) error {
	if m.State.Load() == StatePolling {
		m.StopPolling()
	}

	m.mu.Lock()
	m.community = authCtx.Community()
	m.mu.Unlock()

	// Listen for auth events to handle disconnects
	sub := m.Bus.Subscribe(auth.StateEvent{})
	m.Go(func(ctx context.Context) {
		m.listenEvents(ctx, sub)
	})

	return m.StartPolling()
}

// Close stops all background activities and cleans up the module.
func (m *Manager) Close() error {
	m.State.Store(StateClosed)
	return m.Base.Close()
}

// SetOfferHandler injects the business logic for processing trade offers.
func (m *Manager) SetOfferHandler(ctx context.Context, handler OfferHandler) {
	m.processor = NewProcessor(m, handler, m.Logger)
	m.Go(func(moduleCtx context.Context) {
		m.processor.Start(moduleCtx)
	})
}

// StartPolling begins the trade offer polling loop.
func (m *Manager) StartPolling() error {
	if !m.State.CompareAndSwap(StateStopped, StatePolling) {
		return ErrManagerPolling
	}

	m.Go(func(ctx context.Context) {
		m.pollingLoop(ctx)
	})

	m.Logger.Info("Trade polling started", log.Duration("interval", m.config.PollInterval))
	return nil
}

// StopPolling halts the trade offer polling loop and waits for completion.
func (m *Manager) StopPolling() {
	if m.State.CompareAndSwap(StatePolling, StateStopped) {
		m.Logger.Info("Trade polling stopped")
	}
}

// --- API Methods ---

// AcceptOffer accepts a trade offer.
func (m *Manager) AcceptOffer(ctx context.Context, offerID uint64) error {
	if err := m.rateLimiter.Wait(ctx); err != nil {
		return err
	}
	req := struct {
		TradeOfferID uint64 `url:"tradeofferid"`
		ServerID     int    `url:"serverid"`
	}{offerID, 1}
	_, err := service.WebAPI[service.NoResponse](ctx, m.web, "POST", "IEconService", "AcceptTradeOffer", 1, req)
	return err
}

// DeclineOffer declines a trade offer.
func (m *Manager) DeclineOffer(ctx context.Context, offerID uint64) error {
	if err := m.rateLimiter.Wait(ctx); err != nil {
		return err
	}
	req := struct {
		TradeOfferID uint64 `url:"tradeofferid"`
	}{offerID}
	_, err := service.WebAPI[service.NoResponse](ctx, m.web, "POST", "IEconService", "DeclineTradeOffer", 1, req)
	return err
}

// CancelOffer cancels a trade offer sent by us.
func (m *Manager) CancelOffer(ctx context.Context, offerID uint64) error {
	if err := m.rateLimiter.Wait(ctx); err != nil {
		return err
	}
	req := struct {
		TradeOfferID uint64 `url:"tradeofferid"`
	}{offerID}
	_, err := service.WebAPI[service.NoResponse](ctx, m.web, "POST", "IEconService", "CancelTradeOffer", 1, req)
	return err
}

// GetOffer fetches details for a single offer.
func (m *Manager) GetOffer(ctx context.Context, offerID uint64) (*TradeOffer, error) {
	if err := m.rateLimiter.Wait(ctx); err != nil {
		return nil, err
	}

	req := struct {
		TradeOfferID uint64 `url:"tradeofferid"`
		Language     string `url:"language"`
	}{offerID, m.config.Language}
	type respStruct struct {
		Offer *TradeOffer `json:"offer"`
	}
	resp, err := service.WebAPI[respStruct](ctx, m.web, "GET", "IEconService", "GetTradeOffer", 1, req)
	if err != nil {
		return nil, err
	}

	if resp.Offer == nil {
		return nil, fmt.Errorf("offer %d not found", offerID)
	}

	return resp.Offer, nil
}

// IsItemInTrade allows external modules to know whether an item in the offer is already occupied.
func (m *Manager) IsItemInTrade(assetID uint64) bool {
	if m.processor == nil {
		return false
	}
	return m.processor.IsInTrade(assetID)
}

func (m *Manager) pollingLoop(ctx context.Context) {
	ticker := time.NewTicker(m.config.PollInterval)
	defer ticker.Stop()

	for {
		if m.State.Load() != StatePolling {
			return
		}

		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if m.State.Load() != StatePolling {
				return
			}
			m.doPoll(ctx)
		}
	}
}

func (m *Manager) doPoll(ctx context.Context) {
	if err := m.rateLimiter.Wait(ctx); err != nil {
		return
	}

	// Fetch active sent and received offers from the last 24 hours
	req := struct {
		GetReceivedOffers    int   `url:"get_received_offers"`
		GetSentOffers        int   `url:"get_sent_offers"`
		ActiveOnly           int   `url:"active_only"`
		GetDescriptions      int   `url:"get_descriptions"`
		TimeHistoricalCutoff int64 `url:"time_historical_cutoff"`
	}{1, 1, 1, 0, time.Now().Add(-24 * time.Hour).Unix()}

	type respStruct struct {
		Sent     []*TradeOffer `json:"trade_offers_sent"`
		Received []*TradeOffer `json:"trade_offers_received"`
	}

	resp, err := service.WebAPI[respStruct](ctx, m.web, "GET", "IEconService", "GetTradeOffers", 1, req)
	if err != nil {
		if ctx.Err() == nil {
			m.Logger.Warn("Trade poll failed", log.Err(err))
		}
		return
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	now := time.Now()
	allOffers := append(resp.Sent, resp.Received...)

	for _, offer := range allOffers {
		m.enrichOfferWithSKUs(offer)
		m.lastSeenOffers[offer.ID] = now
		oldState, exists := m.knownOffers[offer.ID]

		if !exists {
			m.knownOffers[offer.ID] = offer.State
			if offer.State == trading.OfferStateActive {
				m.Logger.Info("New trade offer detected", log.Uint64("offer_id", offer.ID))
				m.Bus.Publish(&NewOfferEvent{Offer: offer})

				if m.processor != nil {
					m.processor.Enqueue(offer)
				}
			}
		} else if oldState != offer.State {
			m.knownOffers[offer.ID] = offer.State
			m.Bus.Publish(&OfferChangedEvent{
				Offer:    offer,
				OldState: oldState,
			})
		}
	}

	m.gcKnownOffers(now)
}

func (m *Manager) enrichOfferWithSKUs(offer *TradeOffer) {
	if m.schema == nil {
		m.Logger.Error("tf2_schema module not found, cannot resolve SKU")
		return
	}

	schema := m.schema.Get()
	if schema == nil {
		m.Logger.Warn("schema is not ready yet")
		return
	}

	for i := range offer.ItemsToGive {
		offer.ItemsToGive[i].SKU = schema.GetSKUFromEconItem(offer.ItemsToGive[i])
	}

	for i := range offer.ItemsToReceive {
		offer.ItemsToReceive[i].SKU = schema.GetSKUFromEconItem(offer.ItemsToReceive[i])
	}
}

// gcKnownOffers removes stale offers from memory to prevent memory leaks.
func (m *Manager) gcKnownOffers(now time.Time) {
	for id, lastSeen := range m.lastSeenOffers {
		if now.Sub(lastSeen) > 1*time.Hour {
			if state, ok := m.knownOffers[id]; ok && state != trading.OfferStateActive {
				delete(m.knownOffers, id)
				delete(m.lastSeenOffers, id)
			}
		}
	}
}

func (m *Manager) listenEvents(ctx context.Context, sub *bus.Subscription) {
	defer sub.Unsubscribe()
	for {
		select {
		case <-ctx.Done():
			return
		case ev, ok := <-sub.C():
			if !ok {
				return
			}
			if e, ok := ev.(*auth.StateEvent); ok {
				if e.New == auth.StateDisconnected {
					m.StopPolling()
				}
			}
		}
	}
}

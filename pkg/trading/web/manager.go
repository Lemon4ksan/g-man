// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package web

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"sync"
	"time"

	"golang.org/x/time/rate"

	"github.com/lemon4ksan/g-man/pkg/bus"
	"github.com/lemon4ksan/g-man/pkg/log"
	"github.com/lemon4ksan/g-man/pkg/steam"
	"github.com/lemon4ksan/g-man/pkg/steam/api"
	"github.com/lemon4ksan/g-man/pkg/steam/auth"
	"github.com/lemon4ksan/g-man/pkg/steam/community"
	"github.com/lemon4ksan/g-man/pkg/steam/id"
	"github.com/lemon4ksan/g-man/pkg/steam/module"
	"github.com/lemon4ksan/g-man/pkg/steam/service"
	"github.com/lemon4ksan/g-man/pkg/trading"
	"github.com/lemon4ksan/g-man/pkg/trading/web/offer"
	"github.com/lemon4ksan/g-man/pkg/trading/web/processor"
)

const ModuleName = "trading"

var (
	ErrManagerClosed  = errors.New("trade: closed")
	ErrManagerPolling = errors.New("trade: already polling")
)

// ItemsCollection represents items for one side of the offer.
type ItemsCollection struct {
	Items    []trading.Item // Items from someone else's inventory
	Assets   []uint64       // IDs of items from our inventory
	Currency []uint64       // Currency ID from our inventory
}

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

// Manager handles trade offer synchronization, polling, and state tracking.
// It integrates with a Processor to handle business logic for individual offers.
type Manager struct {
	module.Base

	config Config

	// Dependencies
	web       service.Doer
	community community.Requester
	processor *processor.Processor

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
func (m *Manager) SetOfferHandler(ctx context.Context, handler processor.OfferHandler, bp processor.BackpackProvider) {
	m.processor = processor.NewProcessor(m, bp, handler, processor.WithLogger(m.Logger))
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

// SendOffers builds and sends a new trade offer based on provided parameters.
func (m *Manager) SendOffer(ctx context.Context, p trading.OfferParams) (uint64, error) {
	if err := m.rateLimiter.Wait(ctx); err != nil {
		return 0, err
	}

	type steamObject struct {
		AppID     uint32 `json:"appid"`
		ContextID string `json:"contextid"`
		Amount    int64  `json:"amount"`
		AssetID   string `json:"assetid"`
	}

	tradeOfferObj := struct {
		NewVersion bool          `json:"new_version"`
		Version    int           `json:"version"`
		Me         []steamObject `json:"me"`
		Them       []steamObject `json:"them"`
	}{true, 2, make([]steamObject, 0), make([]steamObject, 0)}

	for _, it := range p.ItemsToGive {
		tradeOfferObj.Me = append(tradeOfferObj.Me, steamObject{
			AppID: it.AppID, ContextID: strconv.FormatInt(it.ContextID, 10),
			AssetID: strconv.FormatUint(it.AssetID, 10), Amount: it.Amount,
		})
	}

	for _, it := range p.ItemsToReceive {
		tradeOfferObj.Them = append(tradeOfferObj.Them, steamObject{
			AppID: it.AppID, ContextID: strconv.FormatInt(it.ContextID, 10),
			AssetID: strconv.FormatUint(it.AssetID, 10), Amount: it.Amount,
		})
	}

	jsonObj, _ := json.Marshal(tradeOfferObj)
	sessionID := m.community.SessionID(community.BaseURL)

	payload := struct {
		SessionID   string `url:"sessionid"`
		ServerID    int    `url:"serverid"`
		PartnerID   id.ID  `url:"partner"`
		Message     string `url:"tradeoffermessage"`
		JSON        string `url:"json_tradeoffer"`
		Token       string `url:"trade_offer_access_token,omitempty"`
		CounteredID uint64 `url:"tradeofferid_countered,omitempty"`
	}{
		sessionID, 1, p.PartnerID, p.Message, string(jsonObj), p.Token, p.CounteredID,
	}

	type sendResponse struct {
		TradeOfferID string `json:"tradeofferid"`
		NeedsMobile  bool   `json:"needs_mobile_confirmation"`
		NeedsEmail   bool   `json:"needs_email_confirmation"`
	}

	resp, err := community.PostForm[sendResponse](ctx, m.community, "tradeoffer/new/send", payload)
	if err != nil {
		return 0, err
	}

	id, _ := strconv.ParseUint(resp.TradeOfferID, 10, 64)

	return id, nil
}

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
func (m *Manager) GetOffer(ctx context.Context, offerID uint64) (*offer.TradeOffer, error) {
	if err := m.rateLimiter.Wait(ctx); err != nil {
		return nil, err
	}

	req := struct {
		TradeOfferID uint64 `url:"tradeofferid"`
		Language     string `url:"language"`
	}{offerID, m.config.Language}

	type respStruct struct {
		Offer *offer.TradeOffer `json:"offer"`
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

// GetEscrowDuration loads the trade page and parses the Trade Hold information.
func (m *Manager) GetEscrowDuration(ctx context.Context, offerID uint64) (processor.Details, error) {
	m.mu.RLock()
	c := m.community
	m.mu.RUnlock()

	if c == nil {
		return processor.Details{}, processor.ErrCommunityNotReady
	}

	resp, err := community.Get[[]byte](ctx, c, fmt.Sprintf("tradeoffer/%d/", offerID), nil,
		api.WithFormat(api.FormatRaw),
	)
	if err != nil {
		return processor.Details{}, fmt.Errorf("failed to fetch offer page: %w", err)
	}

	html := string(*resp)

	theirMatches := processor.RxTheir.FindStringSubmatch(html)
	myMatches := processor.RxMy.FindStringSubmatch(html)

	if len(theirMatches) < 2 || len(myMatches) < 2 {
		return processor.Details{}, processor.ErrEscrowNotFound
	}

	theirDays, _ := strconv.Atoi(theirMatches[1])
	myDays, _ := strconv.Atoi(myMatches[1])

	return processor.Details{
		TheirDays: theirDays,
		MyDays:    myDays,
	}, nil
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
		Sent     []*offer.TradeOffer `json:"trade_offers_sent"`
		Received []*offer.TradeOffer `json:"trade_offers_received"`
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

	allOffers := make([]*offer.TradeOffer, len(resp.Sent)+len(resp.Received))
	copy(allOffers, resp.Sent)
	copy(allOffers[len(resp.Sent):], resp.Received)

	for _, off := range allOffers {
		oldState, exists := m.knownOffers[off.ID]
		m.knownOffers[off.ID] = off.State

		if !exists && off.State == trading.OfferStateActive {
			m.Bus.Publish(&NewOfferEvent{Offer: off})

			if m.processor != nil {
				m.processor.Enqueue(off)
			}
		} else if oldState != off.State {
			m.knownOffers[off.ID] = off.State
			m.Bus.Publish(&OfferChangedEvent{
				Offer:    off,
				OldState: oldState,
			})
		}
	}

	m.gcKnownOffers(now)
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

// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package web

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"net/url"
	"slices"
	"strconv"
	"sync"
	"time"

	"github.com/lemon4ksan/aoni"
	"github.com/lemon4ksan/miyako/bus"
	"github.com/lemon4ksan/miyako/generic"
	"github.com/lemon4ksan/miyako/kata"
	"github.com/lemon4ksan/miyako/yumi"
	"golang.org/x/time/rate"

	"github.com/lemon4ksan/g-man/pkg/log"
	"github.com/lemon4ksan/g-man/pkg/steam"
	"github.com/lemon4ksan/g-man/pkg/steam/auth"
	"github.com/lemon4ksan/g-man/pkg/steam/community"
	"github.com/lemon4ksan/g-man/pkg/steam/community/inventory"
	"github.com/lemon4ksan/g-man/pkg/steam/guard"
	"github.com/lemon4ksan/g-man/pkg/steam/id"
	"github.com/lemon4ksan/g-man/pkg/steam/module"
	"github.com/lemon4ksan/g-man/pkg/steam/protocol"
	"github.com/lemon4ksan/g-man/pkg/steam/protocol/enums"
	"github.com/lemon4ksan/g-man/pkg/steam/service"
	"github.com/lemon4ksan/g-man/pkg/trading"
	"github.com/lemon4ksan/g-man/pkg/trading/web/processor"
)

// ModuleName is the name of the module.
const ModuleName = "trading"

// WithModule returns a steam.Option that registers the trade manager with the given configuration.
func WithModule(cfg Config) steam.Option {
	return func(c *steam.Client) {
		c.RegisterModule(New(cfg))
	}
}

// From returns the trade manager module from the client.
func From(c *steam.Client) *Manager {
	return steam.GetModule[*Manager](c)
}

var (
	// ErrManagerClosed is returned when the manager is closed.
	ErrManagerClosed = errors.New("trade: closed")
	// ErrManagerPolling is returned when the manager is already polling.
	ErrManagerPolling = errors.New("trade: already polling")
)

// descKey uniquely identifies an asset class description.
type descKey struct {
	ClassID    uint64
	InstanceID uint64
}

// ItemsCollection represents items for one side of the offer.
type ItemsCollection struct {
	// Items is the list of fully populated items.
	Items []trading.Item
	// Assets is the list of unique asset identifiers.
	Assets []uint64
	// Currency is the list of unique currency identifiers.
	Currency []uint64
}

// State constants representing the module lifecycle.
type State int32

const (
	// StateStopped represents the stopped state.
	StateStopped State = iota
	// StatePolling represents the polling state.
	StatePolling
	// StateClosed represents the closed state.
	StateClosed
)

// Event represents a trigger that drives a Manager state transition.
type Event int32

const (
	// EventStartPolling triggers transition from Stopped to Polling.
	EventStartPolling Event = iota
	// EventStopPolling triggers transition from Polling to Stopped.
	EventStopPolling
	// EventClose triggers transition to Closed from any active state.
	EventClose
)

// String returns the string representation of the state.
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

// Config holds the configuration for the trade manager.
type Config struct {
	// PollInterval is the time interval between trade offer polls.
	PollInterval time.Duration
	// Language is the language to use for trade offers.
	Language string
	// AppID is the Steam AppID for the current game (e.g. 440 for TF2).
	AppID uint32
	// ContextID is the Steam ContextID for the current game (e.g. 2 for TF2).
	ContextID int64
	// CancelOfferCount is the limit of active sent offers, exceeding which will auto-cancel the oldest active sent offer.
	CancelOfferCount int
	// CancelOfferCountMinAge is the minimum duration an offer must have existed before it is eligible for CancelOfferCount auto-cancellation.
	CancelOfferCountMinAge time.Duration
	// CancelTime is the duration after which active sent offers are automatically cancelled.
	CancelTime time.Duration
}

// DefaultConfig returns the default configuration.
func DefaultConfig() Config {
	return Config{
		PollInterval:           30 * time.Second,
		Language:               "english",
		AppID:                  440,
		ContextID:              2,
		CancelOfferCount:       25,
		CancelOfferCountMinAge: 5 * time.Minute,
		CancelTime:             24 * time.Hour,
	}
}

// Manager handles trade offer synchronization, polling, and state tracking.
//
// It runs a background polling loop, monitors state transitions of sent and
// received offers, and coordinates asynchronous decision processing via an internal
// [processor.Processor] instance.
//
// Create new instances of Manager using the [New] constructor.
type Manager struct {
	module.Base

	config Config

	// Dependencies
	web       service.Doer
	community community.Requester
	processor *processor.Processor

	// Polling synchronization
	mu             sync.RWMutex
	offersSince    int64
	sentOffers     map[uint64]trading.OfferState
	receivedOffers map[uint64]trading.OfferState
	lastSeenOffers map[uint64]time.Time

	rateLimiter *rate.Limiter
	trigger     chan struct{}
	fsm         *kata.FSM[State, Event]

	// Cache for asset class descriptions to avoid re-fetching
	descCache *generic.Cache[descKey, rawAssetClassDescription]
}

// New creates a new instance of the trade manager.
func New(cfg Config) *Manager {
	if cfg.PollInterval < 1*time.Second {
		cfg.PollInterval = 30 * time.Second
	}

	fsm := kata.NewFSM[State, Event](StateStopped)
	fsm.AddRules(
		kata.TransitionRule[State, Event]{From: StateStopped, Event: EventStartPolling, To: StatePolling},
		kata.TransitionRule[State, Event]{From: StatePolling, Event: EventStopPolling, To: StateStopped},
		kata.TransitionRule[State, Event]{From: StateStopped, Event: EventClose, To: StateClosed},
		kata.TransitionRule[State, Event]{From: StatePolling, Event: EventClose, To: StateClosed},
	)

	return &Manager{
		Base:           module.New(ModuleName),
		config:         cfg,
		sentOffers:     make(map[uint64]trading.OfferState),
		receivedOffers: make(map[uint64]trading.OfferState),
		lastSeenOffers: make(map[uint64]time.Time),
		rateLimiter:    rate.NewLimiter(rate.Every(2*time.Second), 1),
		trigger:        make(chan struct{}, 1),
		fsm:            fsm,
		descCache:      generic.NewCache[descKey, rawAssetClassDescription](),
	}
}

// Init initializes the trade manager.
func (m *Manager) Init(init module.InitContext) error {
	if err := m.Base.Init(init); err != nil {
		return err
	}

	m.web = init.Service()

	init.RegisterPacketHandler(enums.EMsg_ClientTradeOffersStateChanged, func(pkt *protocol.Packet) {
		m.Logger.Debug("Received trade offers state change notification")
		m.TriggerPoll()
	})

	return nil
}

// Start initializes the module's context and starts the processor if configured.
func (m *Manager) Start(ctx context.Context) error {
	if err := m.Base.Start(ctx); err != nil {
		return err
	}

	m.mu.RLock()
	proc := m.processor
	m.mu.RUnlock()

	if proc != nil {
		proc.Start(m.Ctx)
	}

	return nil
}

// StartAuthed starts the trade offer polling loop.
func (m *Manager) StartAuthed(ctx context.Context, authCtx module.AuthContext) error {
	if m.fsm.CurrentState() == StatePolling {
		m.StopPolling()
	}

	m.mu.Lock()
	m.community = authCtx.Community()
	m.mu.Unlock()

	// Listen for auth events to handle disconnects
	sub := m.Bus.Subscribe(&auth.StateEvent{})
	m.Go(func(ctx context.Context) {
		m.listenEvents(ctx, sub)
	})

	return m.StartPolling()
}

// Close stops all background activities and cleans up the module.
func (m *Manager) Close() error {
	_ = m.fsm.Transition(context.Background(), EventClose)
	return m.Base.Close()
}

// SetOfferHandler injects the business logic for processing trade offers.
func (m *Manager) SetOfferHandler(ctx context.Context, handler processor.OfferHandler, bp processor.BackpackProvider) {
	m.mu.Lock()
	m.processor = processor.New(m, bp, handler, processor.WithLogger(m.Logger))
	m.mu.Unlock()

	// If the module has already started, start the processor immediately.
	if m.Ctx != nil && m.Ctx.Err() == nil {
		m.processor.Start(m.Ctx)
	}
}

// StartPolling begins the trade offer polling loop.
func (m *Manager) StartPolling() error {
	if err := m.fsm.Transition(context.Background(), EventStartPolling); err != nil {
		return ErrManagerPolling
	}

	m.Go(m.pollingLoop)

	m.Logger.Info("Trade polling started", log.Duration("interval", m.config.PollInterval))

	return nil
}

// StopPolling halts the trade offer polling loop and waits for completion.
func (m *Manager) StopPolling() {
	if err := m.fsm.Transition(context.Background(), EventStopPolling); err == nil {
		m.Logger.Info("Trade polling stopped")
	}
}

// SendOffer builds and sends a new trade offer based on provided parameters.
//
// It enforces rate limiting on outgoing calls and may block the current goroutine.
// If the rate limit wait is canceled, or if the underlying HTTP request fails,
// it returns an error. If Steam requires manual validation, it publishes a
// [guard.ConfirmationRequiredEvent] before returning the offer ID.
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

	type sideObject struct {
		Assets   []steamObject `json:"assets"`
		Currency []any         `json:"currency"`
		Ready    bool          `json:"ready"`
	}

	tradeOfferObj := struct {
		NewVersion bool       `json:"newversion"`
		Version    int        `json:"version"`
		Me         sideObject `json:"me"`
		Them       sideObject `json:"them"`
	}{
		NewVersion: true,
		Version:    2,
		Me: sideObject{
			Assets:   make([]steamObject, 0),
			Currency: make([]any, 0),
			Ready:    false,
		},
		Them: sideObject{
			Assets:   make([]steamObject, 0),
			Currency: make([]any, 0),
			Ready:    false,
		},
	}

	for _, it := range p.ItemsToGive {
		tradeOfferObj.Me.Assets = append(tradeOfferObj.Me.Assets, steamObject{
			AppID: it.AppID, ContextID: strconv.FormatInt(it.ContextID, 10),
			AssetID: strconv.FormatUint(it.AssetID, 10), Amount: it.Amount,
		})
	}

	for _, it := range p.ItemsToReceive {
		tradeOfferObj.Them.Assets = append(tradeOfferObj.Them.Assets, steamObject{
			AppID: it.AppID, ContextID: strconv.FormatInt(it.ContextID, 10),
			AssetID: strconv.FormatUint(it.AssetID, 10), Amount: it.Amount,
		})
	}

	jsonObj, _ := json.Marshal(tradeOfferObj)
	sessionID := m.community.SessionID(community.BaseURL)

	type createParams struct {
		TradeOfferAccessToken string `json:"trade_offer_access_token"`
	}

	var paramsStr string
	if p.Token != "" {
		paramsObj := createParams{TradeOfferAccessToken: p.Token}
		paramsBytes, _ := json.Marshal(paramsObj)
		paramsStr = string(paramsBytes)
	}

	payload := struct {
		SessionID    string `url:"sessionid"`
		ServerID     int    `url:"serverid"`
		PartnerID    id.ID  `url:"partner"`
		Message      string `url:"tradeoffermessage"`
		JSON         string `url:"json_tradeoffer"`
		CreateParams string `url:"trade_offer_create_params,omitempty"`
		CounteredID  uint64 `url:"tradeofferid_countered,omitempty"`
	}{
		sessionID, 1, p.PartnerID, p.Message, string(jsonObj), paramsStr, p.CounteredID,
	}

	type sendResponse struct {
		TradeOfferID string `json:"tradeofferid"`
		NeedsMobile  bool   `json:"needs_mobile_confirmation"`
		NeedsEmail   bool   `json:"needs_email_confirmation"`
	}

	referer := fmt.Sprintf("https://steamcommunity.com/tradeoffer/new/?partner=%d", p.PartnerID.AccountID())
	if p.Token != "" {
		referer = fmt.Sprintf("%s&token=%s", referer, p.Token)
	}

	resp, err := community.PostForm[sendResponse](
		ctx,
		m.community,
		"tradeoffer/new/send",
		payload,
		aoni.WithHeader("Referer", referer),
		aoni.WithHeader("Origin", "https://steamcommunity.com"),
	)
	if err != nil {
		return 0, err
	}

	if resp.NeedsMobile || resp.NeedsEmail {
		m.Bus.Publish(&guard.ConfirmationRequiredEvent{
			TradeOfferID: resp.TradeOfferID,
			IsAppConfirm: resp.NeedsMobile,
			IsEmail:      resp.NeedsEmail,
		})
	}

	id, _ := strconv.ParseUint(resp.TradeOfferID, 10, 64)

	return id, nil
}

// AcceptOffer accepts a trade offer.
//
// It enforces rate limiting and may block the current goroutine.
// If the rate limit wait is canceled, or if the underlying HTTP request fails,
// it returns an error. If the trade requires additional mobile or email confirmation,
// it publishes a [guard.ConfirmationRequiredEvent].
func (m *Manager) AcceptOffer(ctx context.Context, offerID uint64) error {
	if err := m.rateLimiter.Wait(ctx); err != nil {
		return err
	}

	req := struct {
		TradeOfferID uint64 `url:"tradeofferid"`
		ServerID     int    `url:"serverid"`
	}{offerID, 1}

	type acceptResponse struct {
		TradeID                 string `json:"tradeid"`
		NeedsMobileConfirmation bool   `json:"needs_mobile_confirmation"`
		NeedsEmailConfirmation  bool   `json:"needs_email_confirmation"`
		EmailDomain             string `json:"email_domain"`
	}

	resp, err := service.WebAPI[acceptResponse](ctx, m.web, "POST", "IEconService", "AcceptTradeOffer", 1, req)
	if err != nil {
		return err
	}

	if resp.NeedsMobileConfirmation || resp.NeedsEmailConfirmation {
		m.Bus.Publish(&guard.ConfirmationRequiredEvent{
			TradeOfferID: strconv.FormatUint(offerID, 10),
			IsAppConfirm: resp.NeedsMobileConfirmation,
			IsEmail:      resp.NeedsEmailConfirmation,
			EmailDomain:  resp.EmailDomain,
		})
	}

	return nil
}

// DeclineOffer declines a trade offer.
//
// It enforces rate limiting and may block the current goroutine.
// If the rate limit wait is canceled, or if the underlying HTTP request fails,
// it returns an error.
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
//
// It enforces rate limiting and may block the current goroutine.
// If the rate limit wait is canceled, or if the underlying HTTP request fails,
// it returns an error.
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

// GetPollData returns the current polling state.
func (m *Manager) GetPollData() trading.PollData {
	m.mu.RLock()
	defer m.mu.RUnlock()

	sent := make(map[uint64]trading.OfferState, len(m.sentOffers))
	maps.Copy(sent, m.sentOffers)

	received := make(map[uint64]trading.OfferState, len(m.receivedOffers))
	maps.Copy(received, m.receivedOffers)

	return trading.PollData{
		OffersSince: m.offersSince,
		Sent:        sent,
		Received:    received,
	}
}

// SetPollData restores a previously saved polling state.
func (m *Manager) SetPollData(data trading.PollData) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.offersSince = data.OffersSince

	m.sentOffers = make(map[uint64]trading.OfferState)
	maps.Copy(m.sentOffers, data.Sent)

	m.receivedOffers = make(map[uint64]trading.OfferState)
	maps.Copy(m.receivedOffers, data.Received)
}

// ParseTradeURL extracts the partner's Steam ID and the trade token from a trade offer URL.
func ParseTradeURL(tradeURL string) (id.ID, string, error) {
	u, err := url.Parse(tradeURL)
	if err != nil {
		return 0, "", fmt.Errorf("failed to parse trade URL: %w", err)
	}

	q := u.Query()

	partnerStr := q.Get("partner")
	if partnerStr == "" {
		return 0, "", errors.New("trade URL is missing partner parameter")
	}

	partnerAccountID, err := strconv.ParseUint(partnerStr, 10, 32)
	if err != nil {
		return 0, "", fmt.Errorf("invalid partner ID in trade URL: %w", err)
	}

	partnerID := id.FromAccountID(uint32(partnerAccountID))
	token := q.Get("token")

	return partnerID, token, nil
}

// GetExchangeDetails retrieves the status, timing, and assets with their new IDs for a completed trade.
func (m *Manager) GetExchangeDetails(ctx context.Context, tradeID uint64) (*trading.ExchangeDetails, error) {
	if err := m.rateLimiter.Wait(ctx); err != nil {
		return nil, err
	}

	req := struct {
		TradeID         uint64 `url:"tradeid"`
		GetDescriptions bool   `url:"get_descriptions"`
		Language        string `url:"language"`
	}{
		TradeID:         tradeID,
		GetDescriptions: false,
		Language:        m.config.Language,
	}

	type tradeStatusResp struct {
		Trades []struct {
			TradeID        uint64                  `json:"tradeid,string"`
			SteamIDOther   uint64                  `json:"steamid_other,string"`
			TimeInit       int64                   `json:"time_init"`
			Status         int                     `json:"status"`
			AssetsReceived []trading.ExchangeAsset `json:"assets_received"`
			AssetsGiven    []trading.ExchangeAsset `json:"assets_given"`
		} `json:"trades"`
	}

	resp, err := service.WebAPI[tradeStatusResp](ctx, m.web, "GET", "IEconService", "GetTradeStatus", 1, req)
	if err != nil {
		return nil, err
	}

	if len(resp.Trades) == 0 {
		return nil, fmt.Errorf("trade %d not found", tradeID)
	}

	t := resp.Trades[0]

	return &trading.ExchangeDetails{
		Status:         t.Status,
		TimeInit:       t.TimeInit,
		AssetsReceived: t.AssetsReceived,
		AssetsGiven:    t.AssetsGiven,
	}, nil
}

// GetOffer fetches details for a single offer.
func (m *Manager) GetOffer(ctx context.Context, offerID uint64) (*trading.TradeOffer, error) {
	if err := m.rateLimiter.Wait(ctx); err != nil {
		return nil, err
	}

	req := struct {
		TradeOfferID    uint64 `url:"tradeofferid"`
		GetDescriptions bool   `url:"get_descriptions"`
		Language        string `url:"language"`
	}{offerID, true, m.config.Language}

	type respStruct struct {
		Offer        *trading.TradeOffer `json:"offer"`
		Descriptions []rawDescription    `json:"descriptions"`
	}

	resp, err := service.WebAPI[respStruct](ctx, m.web, "GET", "IEconService", "GetTradeOffer", 1, req)
	if err != nil {
		return nil, err
	}

	if resp.Offer == nil {
		return nil, fmt.Errorf("offer %d not found", offerID)
	}

	mapDescriptionsToOffer(resp.Offer, resp.Descriptions)
	_ = m.enrichOfferDescriptions(ctx, resp.Offer)

	return resp.Offer, nil
}

// GetPartnerInventory fetches the inventory of a trade partner for the current game (TF2).
func (m *Manager) GetPartnerInventory(ctx context.Context, partnerID id.ID) ([]*trading.Item, error) {
	m.mu.RLock()
	c := m.community
	m.mu.RUnlock()

	if c == nil {
		return nil, processor.ErrCommunityNotReady
	}

	inv, _, _, err := inventory.GetUserInventoryContents(
		ctx,
		c,
		uint64(partnerID),
		m.config.AppID,
		m.config.ContextID,
		true,
		m.config.Language,
	)
	if err != nil {
		return nil, err
	}

	result := make([]*trading.Item, len(inv))
	for i, it := range inv {
		assetID, _ := strconv.ParseUint(it.Asset.AssetID, 10, 64)
		classID, _ := strconv.ParseUint(it.Asset.ClassID, 10, 64)
		instanceID, _ := strconv.ParseUint(it.Asset.InstanceID, 10, 64)
		amount, _ := strconv.ParseInt(it.Asset.Amount, 10, 64)

		var descs []trading.Description
		if len(it.Description.Descriptions) > 0 {
			descs = make([]trading.Description, len(it.Description.Descriptions))
			for idx, d := range it.Description.Descriptions {
				descs[idx] = trading.Description{
					Value: d.Value,
					Color: d.Color,
				}
			}
		}

		var tags []trading.Tag
		if len(it.Description.Tags) > 0 {
			tags = make([]trading.Tag, len(it.Description.Tags))
			for idx, t := range it.Description.Tags {
				tags[idx] = trading.Tag{
					Category:      t.Category,
					InternalName:  t.InternalName,
					Localized:     t.LocalizedCategoryName,
					LocalizedName: t.LocalizedTagName,
				}
			}
		}

		result[i] = &trading.Item{
			AppID:          440,
			ContextID:      2,
			AssetID:        assetID,
			ClassID:        classID,
			InstanceID:     instanceID,
			Amount:         amount,
			Name:           it.Description.Name,
			MarketHashName: it.Description.MarketHashName,
			Tradable:       it.Description.Tradable == 1,
			Descriptions:   descs,
			Tags:           tags,
		}
	}

	_ = m.enrichItemsDescriptions(ctx, result)

	return result, nil
}

// GetEscrowDuration loads the trade page and parses the Trade Hold information.
//
// It returns [processor.ErrCommunityNotReady] if the community requester is nil.
// It returns [processor.ErrEscrowNotFound] if it fails to parse valid escrow hold durations from the page.
func (m *Manager) GetEscrowDuration(ctx context.Context, offerID uint64) (processor.Details, error) {
	m.mu.RLock()
	c := m.community
	m.mu.RUnlock()

	if c == nil {
		return processor.Details{}, processor.ErrCommunityNotReady
	}

	body, err := community.GetHTML(ctx, c, "tradeoffer/{offerID}/",
		aoni.WithVar("offerID", offerID),
	)
	if err != nil {
		return processor.Details{}, fmt.Errorf("failed to fetch offer page: %w", err)
	}

	var buf bytes.Buffer
	if _, err := io.Copy(&buf, body); err != nil {
		return processor.Details{}, err
	}

	theirMatches := processor.RxTheir.FindStringSubmatch(buf.String())
	myMatches := processor.RxMy.FindStringSubmatch(buf.String())

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

// CheckEscrow checks if a trade offer has a trade hold.
// This fulfills the trading.EscrowChecker interface.
func (m *Manager) CheckEscrow(ctx context.Context, offer *trading.TradeOffer) (bool, error) {
	details, err := m.GetEscrowDuration(ctx, offer.ID)
	if err != nil {
		return false, err
	}

	return details.HasHold(), nil
}

func (m *Manager) pollingLoop(ctx context.Context) {
	ticker := time.NewTicker(m.config.PollInterval)
	defer ticker.Stop()

	for {
		if m.fsm.CurrentState() != StatePolling {
			return
		}

		select {
		case <-ctx.Done():
			return
		case <-m.trigger:
			time.Sleep(1 * time.Second)
			m.doPoll(ctx)
		case <-ticker.C:
			if m.fsm.CurrentState() != StatePolling {
				return
			}

			m.doPoll(ctx)
		}
	}
}

// TriggerPoll manually triggers a trade offer poll.
func (m *Manager) TriggerPoll() {
	select {
	case m.trigger <- struct{}{}:
	default:
		// Already triggered
	}
}

func (m *Manager) doPoll(ctx context.Context) {
	if err := m.rateLimiter.Wait(ctx); err != nil {
		return
	}

	m.Logger.Debug("Polling trade offers...")

	m.mu.RLock()

	cutoff := time.Now().Add(-24 * time.Hour).Unix()
	if m.offersSince > 0 {
		cutoff = m.offersSince - 1800 // 30-minute buffer
	}

	m.mu.RUnlock()

	// Fetch active sent and received offers
	req := struct {
		GetReceivedOffers    int   `url:"get_received_offers"`
		GetSentOffers        int   `url:"get_sent_offers"`
		ActiveOnly           int   `url:"active_only"`
		GetDescriptions      int   `url:"get_descriptions"`
		TimeHistoricalCutoff int64 `url:"time_historical_cutoff"`
	}{1, 1, 1, 1, cutoff}

	type respStruct struct {
		Sent         []*trading.TradeOffer `json:"trade_offers_sent"`
		Received     []*trading.TradeOffer `json:"trade_offers_received"`
		Descriptions []rawDescription      `json:"descriptions"`
	}

	resp, err := service.WebAPI[respStruct](ctx, m.web, "GET", "IEconService", "GetTradeOffers", 1, req)
	if err != nil {
		if ctx.Err() == nil {
			m.Logger.Warn("Trade poll failed", log.Err(err))
		}

		return
	}

	for _, o := range resp.Sent {
		if o != nil {
			mapDescriptionsToOffer(o, resp.Descriptions)
			_ = m.enrichOfferDescriptions(ctx, o)
		}
	}

	for _, o := range resp.Received {
		if o != nil {
			mapDescriptionsToOffer(o, resp.Descriptions)
			_ = m.enrichOfferDescriptions(ctx, o)
		}
	}

	// Auto-cancellation checks for active sent offers (CancelTime) and pending limits (CancelOfferCount)
	if len(resp.Sent) > 0 {
		activeSent := generic.FilterInPlace(resp.Sent, func(off *trading.TradeOffer) bool {
			return off.State == trading.OfferStateActive
		})

		// CancelTime auto-cancellation
		if m.config.CancelTime > 0 {
			for _, off := range activeSent {
				age := time.Since(off.UpdatedAt())
				if age >= m.config.CancelTime {
					m.Logger.Info("Auto-cancelling active sent offer due to CancelTime timeout",
						log.Uint64("offer_id", off.ID),
						log.Duration("age", age),
					)

					go func(id uint64) {
						if err := m.CancelOffer(ctx, id); err != nil {
							m.Logger.Error("Failed to auto-cancel offer", log.Uint64("offer_id", id), log.Err(err))
						}
					}(off.ID)
				}
			}
		}

		// CancelOfferCount limit auto-cancellation
		if m.config.CancelOfferCount > 0 && len(activeSent) >= m.config.CancelOfferCount {
			var oldest *trading.TradeOffer
			for _, off := range activeSent {
				age := time.Since(off.UpdatedAt())
				if m.config.CancelOfferCountMinAge > 0 && age < m.config.CancelOfferCountMinAge {
					continue
				}

				if oldest == nil || off.TimeUpdated < oldest.TimeUpdated {
					oldest = off
				}
			}

			if oldest != nil {
				m.Logger.Info("Auto-cancelling oldest active sent offer due to limit",
					log.Uint64("offer_id", oldest.ID),
					log.Int("active_count", len(activeSent)),
					log.Int("limit", m.config.CancelOfferCount),
				)

				go func(id uint64) {
					if err := m.CancelOffer(ctx, id); err != nil {
						m.Logger.Error("Failed to auto-cancel oldest offer", log.Uint64("offer_id", id), log.Err(err))
					}
				}(oldest.ID)
			}
		}
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	// Clone original poll data for comparison
	origPollData := trading.PollData{
		OffersSince: m.offersSince,
		Sent:        make(map[uint64]trading.OfferState, len(m.sentOffers)),
		Received:    make(map[uint64]trading.OfferState, len(m.receivedOffers)),
	}
	maps.Copy(origPollData.Sent, m.sentOffers)
	maps.Copy(origPollData.Received, m.receivedOffers)

	now := time.Now()

	allOffers := make([]*trading.TradeOffer, 0, len(resp.Sent)+len(resp.Received))
	for _, off := range resp.Sent {
		if off != nil {
			allOffers = append(allOffers, off)
		}
	}

	for _, off := range resp.Received {
		if off != nil {
			allOffers = append(allOffers, off)
		}
	}

	for _, off := range allOffers {
		m.lastSeenOffers[off.ID] = now

		var (
			oldState trading.OfferState
			exists   bool
		)

		if off.IsOurOffer {
			oldState, exists = m.sentOffers[off.ID]
			m.sentOffers[off.ID] = off.State
		} else {
			oldState, exists = m.receivedOffers[off.ID]
			m.receivedOffers[off.ID] = off.State
		}

		if !exists && off.State == trading.OfferStateActive {
			m.Bus.Publish(&NewOfferEvent{Offer: off})

			if m.processor != nil {
				m.processor.Enqueue(off)
			}
		} else if oldState != off.State {
			m.Bus.Publish(&OfferChangedEvent{
				Offer:    off,
				OldState: oldState,
			})
		}
	}

	// Update offersSince
	latest := m.offersSince
	for _, off := range allOffers {
		if off.TimeUpdated > latest {
			latest = off.TimeUpdated
		}
	}

	m.offersSince = latest

	m.gcKnownOffers(now)

	// Compare with old poll data and trigger PollDataEvent if changed
	newPollData := trading.PollData{
		OffersSince: m.offersSince,
		Sent:        make(map[uint64]trading.OfferState, len(m.sentOffers)),
		Received:    make(map[uint64]trading.OfferState, len(m.receivedOffers)),
	}
	maps.Copy(newPollData.Sent, m.sentOffers)
	maps.Copy(newPollData.Received, m.receivedOffers)

	isEqual := origPollData.OffersSince == newPollData.OffersSince &&
		len(origPollData.Sent) == len(newPollData.Sent) &&
		len(origPollData.Received) == len(newPollData.Received)

	if isEqual {
		for k, v := range origPollData.Sent {
			if newPollData.Sent[k] != v {
				isEqual = false
				break
			}
		}
	}

	if isEqual {
		for k, v := range origPollData.Received {
			if newPollData.Received[k] != v {
				isEqual = false
				break
			}
		}
	}

	if !isEqual {
		m.Bus.Publish(&PollDataEvent{
			PollData: newPollData,
		})
	}

	m.Logger.Debug("Trade poll completed",
		log.Int("sent_active", len(resp.Sent)),
		log.Int("received_active", len(resp.Received)),
	)
}

// gcKnownOffers removes stale offers from memory to prevent memory leaks.
func (m *Manager) gcKnownOffers(now time.Time) {
	for id, lastSeen := range m.lastSeenOffers {
		if now.Sub(lastSeen) > 1*time.Hour {
			if state, ok := m.sentOffers[id]; ok && state != trading.OfferStateActive {
				delete(m.sentOffers, id)
				delete(m.lastSeenOffers, id)
			} else if state, ok := m.receivedOffers[id]; ok && state != trading.OfferStateActive {
				delete(m.receivedOffers, id)
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

// GetActiveSentOffers returns all active trade offers sent by us.
func (m *Manager) GetActiveSentOffers(ctx context.Context) ([]trading.TradeOffer, error) {
	if err := m.rateLimiter.Wait(ctx); err != nil {
		return nil, err
	}

	req := struct {
		GetReceivedOffers    int   `url:"get_received_offers"`
		GetSentOffers        int   `url:"get_sent_offers"`
		ActiveOnly           int   `url:"active_only"`
		GetDescriptions      int   `url:"get_descriptions"`
		TimeHistoricalCutoff int64 `url:"time_historical_cutoff"`
	}{0, 1, 1, 1, time.Now().Add(-24 * time.Hour).Unix()}

	type respStruct struct {
		Sent         []*trading.TradeOffer `json:"trade_offers_sent"`
		Descriptions []rawDescription      `json:"descriptions"`
	}

	resp, err := service.WebAPI[respStruct](ctx, m.web, "GET", "IEconService", "GetTradeOffers", 1, req)
	if err != nil {
		return nil, err
	}

	offers := make([]trading.TradeOffer, 0, len(resp.Sent))
	for _, o := range resp.Sent {
		if o != nil {
			mapDescriptionsToOffer(o, resp.Descriptions)
			_ = m.enrichOfferDescriptions(ctx, o)
			offers = append(offers, *o)
		}
	}

	return offers, nil
}

type rawDescription struct {
	AppID          uint32                `json:"appid"`
	ClassID        string                `json:"classid"`
	InstanceID     string                `json:"instanceid"`
	Name           string                `json:"name"`
	NameColor      string                `json:"name_color"`
	Type           string                `json:"type"`
	MarketName     string                `json:"market_name"`
	MarketHashName string                `json:"market_hash_name"`
	IconURL        string                `json:"icon_url"`
	Tradable       aoni.BoolInt          `json:"tradable"`
	Marketable     aoni.BoolInt          `json:"marketable"`
	Descriptions   []trading.Description `json:"descriptions"`
	Tags           []trading.Tag         `json:"tags"`
	Actions        []trading.Action      `json:"actions"`
}

func mapDescriptionsToOffer(offer *trading.TradeOffer, rawDescs []rawDescription) {
	if offer == nil || len(rawDescs) == 0 {
		return
	}

	descMap := make(map[descKey]*rawDescription)
	for i := range rawDescs {
		d := &rawDescs[i]
		classID, _ := strconv.ParseUint(d.ClassID, 10, 64)
		instanceID, _ := strconv.ParseUint(d.InstanceID, 10, 64)
		descMap[descKey{ClassID: classID, InstanceID: instanceID}] = d
	}

	mapItems := func(items []*trading.Item) {
		for _, it := range items {
			key := descKey{ClassID: it.ClassID, InstanceID: it.InstanceID}
			if d, ok := descMap[key]; ok {
				it.Name = d.Name
				it.NameColor = d.NameColor
				it.Type = d.Type
				it.MarketName = d.MarketName
				it.MarketHashName = d.MarketHashName
				it.IconURL = d.IconURL
				it.Tradable = bool(d.Tradable)
				it.Marketable = bool(d.Marketable)
				it.Descriptions = d.Descriptions
				it.Tags = d.Tags
				it.Actions = d.Actions
			}
		}
	}

	mapItems(offer.ItemsToGive)
	mapItems(offer.ItemsToReceive)
}

type assetClassTag struct {
	Category              string `json:"category"`
	InternalName          string `json:"internal_name"`
	LocalizedCategoryName string `json:"localized_category_name"`
	LocalizedTagName      string `json:"localized_tag_name"`
	Name                  string `json:"name"`
}

type flexibleDescriptions []trading.Description

func (fd *flexibleDescriptions) UnmarshalJSON(data []byte) error {
	if len(data) == 0 {
		return nil
	}

	if data[0] == '"' {
		var s string
		if err := json.Unmarshal(data, &s); err == nil {
			*fd = nil
			return nil
		}
	}

	if data[0] == '[' {
		var arr []trading.Description
		if err := json.Unmarshal(data, &arr); err == nil {
			*fd = arr
			return nil
		}
	}

	if data[0] == '{' {
		var m map[string]trading.Description
		if err := json.Unmarshal(data, &m); err == nil {
			type item struct {
				idx int
				val trading.Description
			}

			var items []item
			for k, v := range m {
				idx, _ := strconv.Atoi(k)
				items = append(items, item{idx: idx, val: v})
			}

			slices.SortFunc(items, func(a, b item) int {
				return a.idx - b.idx
			})

			res := make([]trading.Description, len(items))
			for idx, item := range items {
				res[idx] = item.val
			}

			*fd = res

			return nil
		}
	}

	return fmt.Errorf("failed to unmarshal descriptions: %s", string(data))
}

type flexibleTags []assetClassTag

func (ft *flexibleTags) UnmarshalJSON(data []byte) error {
	if len(data) == 0 {
		return nil
	}

	if data[0] == '"' {
		var s string
		if err := json.Unmarshal(data, &s); err == nil {
			*ft = nil
			return nil
		}
	}

	if data[0] == '[' {
		var arr []assetClassTag
		if err := json.Unmarshal(data, &arr); err == nil {
			*ft = arr
			return nil
		}
	}

	if data[0] == '{' {
		var m map[string]assetClassTag
		if err := json.Unmarshal(data, &m); err == nil {
			type item struct {
				idx int
				val assetClassTag
			}

			var items []item
			for k, v := range m {
				idx, _ := strconv.Atoi(k)
				items = append(items, item{idx: idx, val: v})
			}

			slices.SortFunc(items, func(a, b item) int {
				return a.idx - b.idx
			})

			res := make([]assetClassTag, len(items))
			for idx, item := range items {
				res[idx] = item.val
			}

			*ft = res

			return nil
		}
	}

	return fmt.Errorf("failed to unmarshal tags: %s", string(data))
}

type rawAssetClassDescription struct {
	ClassID        string               `json:"classid"`
	InstanceID     string               `json:"instanceid"`
	Name           string               `json:"name"`
	MarketName     string               `json:"market_name"`
	Type           string               `json:"type"`
	MarketHashName string               `json:"market_hash_name"`
	IconURL        string               `json:"icon_url"`
	Descriptions   flexibleDescriptions `json:"descriptions"`
	Tags           flexibleTags         `json:"tags"`
	Tradable       aoni.BoolInt         `json:"tradable"`
	Marketable     aoni.BoolInt         `json:"marketable"`
}

func (m *Manager) enrichOfferDescriptions(ctx context.Context, offer *trading.TradeOffer) error {
	if offer == nil {
		return nil
	}

	items := make([]*trading.Item, 0, len(offer.ItemsToGive)+len(offer.ItemsToReceive))

	items = append(items, offer.ItemsToGive...)
	items = append(items, offer.ItemsToReceive...)

	return m.enrichItemsDescriptions(ctx, items)
}

func (m *Manager) enrichItemsDescriptions(ctx context.Context, items []*trading.Item) error {
	if len(items) == 0 {
		return nil
	}

	var missingKeys []descKey

	seenKeys := generic.NewSet[descKey]()

	for _, it := range items {
		if it.MarketHashName == "" {
			k := descKey{ClassID: it.ClassID, InstanceID: it.InstanceID}
			if !seenKeys.Has(k) {
				seenKeys.Add(k)
				missingKeys = append(missingKeys, k)
			}
		}
	}

	if len(missingKeys) == 0 {
		return nil
	}

	resolvedDescs := make(map[descKey]rawAssetClassDescription)

	var uncachedKeys []descKey
	for _, k := range missingKeys {
		if desc, ok := m.descCache.Get(k); ok {
			resolvedDescs[k] = desc
		} else {
			uncachedKeys = append(uncachedKeys, k)
		}
	}

	if len(uncachedKeys) == 0 {
		updateItems(items, resolvedDescs)
		return nil
	}

	type GetAssetClassInfoResponse struct {
		Result map[string]json.RawMessage `json:"result"`
	}

	type chunkResult struct {
		descs map[descKey]rawAssetClassDescription
	}

	chunkSize := 50

	var chunks [][]descKey
	for i := 0; i < len(uncachedKeys); i += chunkSize {
		end := min(i+chunkSize, len(uncachedKeys))
		chunks = append(chunks, uncachedKeys[i:end])
	}

	results, err := yumi.Map(ctx, yumi.PipelineConfig{
		Workers: 3,
		RPS:     5,
		Burst:   2,
	}, chunks, func(chunkCtx context.Context, chunk []descKey) (chunkResult, error) {
		params := make(url.Values)
		params.Set("appid", strconv.FormatUint(uint64(m.config.AppID), 10))
		params.Set("language", m.config.Language)
		params.Set("class_count", strconv.Itoa(len(chunk)))

		for idx, k := range chunk {
			params.Set(fmt.Sprintf("classid%d", idx), strconv.FormatUint(k.ClassID, 10))

			if k.InstanceID != 0 {
				params.Set(fmt.Sprintf("instanceid%d", idx), strconv.FormatUint(k.InstanceID, 10))
			}
		}

		apiResp, err := service.WebAPI[GetAssetClassInfoResponse](
			chunkCtx,
			m.web,
			"GET",
			"ISteamEconomy",
			"GetAssetClassInfo",
			1,
			params,
		)
		if err != nil {
			return chunkResult{}, err
		}

		resolvedDescs := make(map[descKey]rawAssetClassDescription)
		if apiResp != nil && apiResp.Result != nil {
			for key, rawVal := range apiResp.Result {
				if key == "success" {
					continue
				}

				var desc rawAssetClassDescription
				if err := json.Unmarshal(rawVal, &desc); err == nil {
					var cID uint64
					if desc.ClassID != "" {
						cID, _ = strconv.ParseUint(desc.ClassID, 10, 64)
					} else {
						cID, _ = strconv.ParseUint(key, 10, 64)
					}

					instID, _ := strconv.ParseUint(desc.InstanceID, 10, 64)
					resolvedDescs[descKey{ClassID: cID, InstanceID: instID}] = desc
				}
			}
		}

		return chunkResult{descs: resolvedDescs}, nil
	})
	if err != nil {
		return err
	}

	for _, r := range results {
		maps.Copy(resolvedDescs, r.descs)
	}

	for k, desc := range resolvedDescs {
		m.descCache.Set(k, desc, 5*time.Minute)
	}

	updateItems(items, resolvedDescs)

	return nil
}

func updateItems(items []*trading.Item, descs map[descKey]rawAssetClassDescription) {
	for _, it := range items {
		if it.MarketHashName == "" {
			k := descKey{ClassID: it.ClassID, InstanceID: it.InstanceID}

			var desc rawAssetClassDescription

			found := false
			if desc, found = descs[k]; !found {
				desc, found = descs[descKey{ClassID: it.ClassID, InstanceID: 0}]
			}

			if found {
				it.Name = desc.Name
				it.MarketName = desc.MarketName
				it.MarketHashName = desc.MarketHashName
				it.Type = desc.Type
				it.IconURL = desc.IconURL
				it.Descriptions = desc.Descriptions
				it.Tradable = bool(desc.Tradable)
				it.Marketable = bool(desc.Marketable)

				it.Tags = make([]trading.Tag, len(desc.Tags))
				for idx, t := range desc.Tags {
					locName := t.LocalizedTagName
					if locName == "" {
						locName = t.Name
					}

					it.Tags[idx] = trading.Tag{
						Category:      t.Category,
						InternalName:  t.InternalName,
						Localized:     t.LocalizedCategoryName,
						LocalizedName: locName,
					}
				}
			}
		}
	}
}

// Web returns the internal service.Doer.
func (m *Manager) Web() service.Doer {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.web
}

// Community returns the internal community.Requester.
func (m *Manager) Community() community.Requester {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.community
}

// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package web manages trade offer polling, creation, acceptance, and offer lifecycle state tracking via WebAPI and Community endpoints.
package web

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"
	"time"

	"github.com/lemon4ksan/foundation/async/fsm"
	log "github.com/lemon4ksan/foundation/async/logkit"
	"github.com/lemon4ksan/foundation/async/rate"
	"github.com/lemon4ksan/foundation/sync/keylock"

	"github.com/lemon4ksan/g-man/pkg/steam/auth"
	"github.com/lemon4ksan/g-man/pkg/steam/client"
	"github.com/lemon4ksan/g-man/pkg/steam/community"
	"github.com/lemon4ksan/g-man/pkg/steam/module"
	"github.com/lemon4ksan/g-man/pkg/steam/service"
	"github.com/lemon4ksan/g-man/pkg/steam/sys/notifications"
	"github.com/lemon4ksan/g-man/pkg/trading"
	"github.com/lemon4ksan/g-man/pkg/trading/web/processor"
)

const ModuleName string = "trading"

var (
	// ErrManagerClosed indicates operation was attempted on closed trade Manager.
	ErrManagerClosed = errors.New("trade: closed")
	// ErrManagerPolling indicates polling is already active.
	ErrManagerPolling = errors.New("trade: already polling")
	// ErrCommunityNotReady indicates community requester is unavailable.
	ErrCommunityNotReady = processor.ErrCommunityNotReady
	// ErrEscrowNotFound indicates escrow data was absent from trade offer HTML page.
	ErrEscrowNotFound = processor.ErrEscrowNotFound

	// ErrUnauthenticatedTrade indicates community client is not authenticated.
	ErrUnauthenticatedTrade = errors.New("trading: community client not authenticated or initialized")
	// ErrOfferNotFound indicates requested trade offer does not exist.
	ErrOfferNotFound = errors.New("trade offer not found")
	// ErrMissingPartnerParam indicates trade URL does not contain partner parameter.
	ErrMissingPartnerParam = errors.New("trade URL is missing partner parameter")
	// ErrItemAlreadyReserved indicates one or more items are already locked in another active offer.
	ErrItemAlreadyReserved = errors.New("trading: one or more items are already reserved in another active offer")
)

// WithModule registers the Manager module in the client.
func WithModule(cfg Config) client.Option {
	return client.WithModule(New(cfg))
}

// From retrieves the Manager module instance from the client.
func From(c *client.Client) *Manager {
	return client.GetModule[*Manager](c)
}

// State represents the lifecycle state of the trade polling engine.
type State int32

const (
	StateStopped State = iota
	StatePolling
	StateClosed
)

// Event represents lifecycle events driving trade polling transitions.
type Event int32

const (
	EventStartPolling Event = iota
	EventStopPolling
	EventClose
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

// Config configures trade polling, cancellation, and inventory parameters.
type Config struct {
	PollInterval           time.Duration
	Language               string
	AppID                  uint32
	ContextID              int64
	CancelOfferCount       int
	CancelOfferCountMinAge time.Duration
	CancelTime             time.Duration
}

// DefaultConfig provides sensible production defaults for trading configuration.
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

// Manager polls IEconService for trade offer changes, manages offer creation, and tracks historical states.
//
// Thread Safety:
//   - Safe for concurrent use across all methods.
type Manager struct {
	module.Base

	config Config

	web       service.Doer
	community community.Requester
	processor *processor.Processor
	canceller *Canceller
	enricher  *Enricher

	mu             sync.RWMutex
	offersSince    int64
	sentOffers     map[uint64]trading.OfferState
	receivedOffers map[uint64]trading.OfferState
	lastSeenOffers map[uint64]time.Time

	rateLimiter *rate.Limiter
	trigger     chan struct{}
	fsm         *fsm.FSM[State, Event]
	itemLocks   *keylock.KeyMutex[uint64]
}

// New constructs a Manager instance with configured polling parameters.
func New(cfg Config) *Manager {
	if cfg.PollInterval < 1*time.Second {
		cfg.PollInterval = 30 * time.Second
	}

	mach := fsm.NewFSM[State, Event](StateStopped)
	mach.AddRules(
		fsm.TransitionRule[State, Event]{From: StateStopped, Event: EventStartPolling, To: StatePolling},
		fsm.TransitionRule[State, Event]{From: StatePolling, Event: EventStopPolling, To: StateStopped},
		fsm.TransitionRule[State, Event]{From: StateStopped, Event: EventClose, To: StateClosed},
		fsm.TransitionRule[State, Event]{From: StatePolling, Event: EventClose, To: StateClosed},
	)

	return &Manager{
		Base:           module.New(ModuleName),
		config:         cfg,
		canceller:      NewCanceller(log.Discard),
		enricher:       NewEnricher(),
		sentOffers:     make(map[uint64]trading.OfferState),
		receivedOffers: make(map[uint64]trading.OfferState),
		lastSeenOffers: make(map[uint64]time.Time),
		rateLimiter:    rate.NewLimiter(rate.Every(2*time.Second), 1),
		trigger:        make(chan struct{}, 1),
		fsm:            mach,
		itemLocks:      keylock.New[uint64](),
	}
}

// ReserveItems attempts to reserve multiple asset IDs atomically (e.g. before creating a trade offer).
// If any asset is already reserved, all acquired locks are released and ErrItemAlreadyReserved is returned.
// Returns an unlock cleanup function.
func (m *Manager) ReserveItems(assetIDs ...uint64) (func(), error) {
	if len(assetIDs) == 0 {
		return func() {}, nil
	}

	// Sort asset IDs to avoid deadlock potential
	sorted := make([]uint64, len(assetIDs))
	copy(sorted, assetIDs)
	slices.Sort(sorted)

	locked := make([]uint64, 0, len(sorted))
	for _, id := range sorted {
		if !m.itemLocks.TryLock(id) {
			for _, prevID := range locked {
				m.itemLocks.Unlock(prevID)
			}

			return nil, fmt.Errorf("%w: asset ID %d", ErrItemAlreadyReserved, id)
		}

		locked = append(locked, id)
	}

	var once sync.Once

	return func() {
		once.Do(func() {
			for _, id := range locked {
				m.itemLocks.Unlock(id)
			}
		})
	}, nil
}

// LockItems blocks until all specified asset IDs are successfully locked.
// Returns an unlock cleanup function.
func (m *Manager) LockItems(assetIDs ...uint64) func() {
	if len(assetIDs) == 0 {
		return func() {}
	}

	sorted := make([]uint64, len(assetIDs))
	copy(sorted, assetIDs)
	slices.Sort(sorted)

	for _, id := range sorted {
		m.itemLocks.Lock(id)
	}

	var once sync.Once

	return func() {
		once.Do(func() {
			for _, id := range sorted {
				m.itemLocks.Unlock(id)
			}
		})
	}
}

// IsItemReserved reports whether the given asset ID is currently locked/reserved.
func (m *Manager) IsItemReserved(assetID uint64) bool {
	if m == nil || m.itemLocks == nil {
		return false
	}

	return m.itemLocks.IsLocked(assetID)
}

// Web returns the underlying WebAPI service client.
func (m *Manager) Web() service.Doer { return m.web }

// Community returns the active community requester.
func (m *Manager) Community() community.Requester {
	m.mu.RLock()
	defer m.mu.RUnlock()

	return m.community
}

// Init initializes module resources and registers WebAPI client.
func (m *Manager) Init(init module.InitContext) error {
	if err := m.Base.Init(init); err != nil {
		return err
	}

	m.web = init.Service()
	m.canceller.logger = m.Logger

	return nil
}

// Start begins background workers when client initializes.
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

// StartAuthed begins authenticated polling and notification listeners upon successful login.
func (m *Manager) StartAuthed(ctx context.Context, authCtx module.AuthContext) error {
	if m.fsm.CurrentState() == StatePolling {
		m.StopPolling()
	}

	m.mu.Lock()
	m.community = authCtx.Community()
	m.mu.Unlock()

	sub := m.Bus.Subscribe(&auth.StateEvent{})
	m.Go(func(ctx context.Context) {
		m.listenEvents(ctx, sub)
	})

	notifSub := m.Bus.Subscribe(
		&notifications.UserNotificationsEvent{},
		&notifications.ReceivedEvent{},
	)
	m.Go(func(ctx context.Context) {
		m.listenNotifications(ctx, notifSub)
	})

	return m.StartPolling()
}

// Close terminates polling and cleans up module resources.
func (m *Manager) Close() error {
	_ = m.fsm.Transition(context.Background(), EventClose)

	return m.Base.Close()
}

// TriggerPoll immediately schedules a polling tick.
func (m *Manager) TriggerPoll() {
	select {
	case m.trigger <- struct{}{}:
	default:
	}
}

// SetOfferHandler configures a high-level offer processor.
func (m *Manager) SetOfferHandler(ctx context.Context, handler processor.OfferHandler, bp processor.BackpackProvider) {
	m.mu.Lock()
	m.processor = processor.New(m, bp, handler, processor.WithLogger(m.Logger))
	m.mu.Unlock()

	if m.Ctx != nil && m.Ctx.Err() == nil {
		m.processor.Start(m.Ctx)
	}
}

// StartPolling transitions to polling state and starts background loop.
func (m *Manager) StartPolling() error {
	if err := m.fsm.Transition(context.Background(), EventStartPolling); err != nil {
		return ErrManagerPolling
	}

	m.Go(m.pollingLoop)
	m.Logger.Info("Trade polling started", log.Duration("interval", m.config.PollInterval))

	return nil
}

// StopPolling halts the active background polling loop.
func (m *Manager) StopPolling() {
	if err := m.fsm.Transition(context.Background(), EventStopPolling); err == nil {
		m.Logger.Info("Trade polling stopped")
	}
}

func (m *Manager) enrichItemsDescriptions(ctx context.Context, items []*trading.Item) error {
	return m.enricher.EnrichItems(ctx, m.web, m.config.AppID, m.config.Language, items)
}

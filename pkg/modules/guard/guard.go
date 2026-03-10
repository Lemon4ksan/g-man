// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package guard implements Steam Guard Mobile Confirmation handling.
package guard

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/time/rate"

	"github.com/lemon4ksan/g-man/pkg/log"
	"github.com/lemon4ksan/g-man/pkg/modules/auth"
	"github.com/lemon4ksan/g-man/pkg/steam"
	"github.com/lemon4ksan/g-man/pkg/steam/bus"
	"github.com/lemon4ksan/g-man/pkg/steam/crypto/totp"
)

const ModuleName string = "guard"

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

var (
	ErrGuardClosed      = errors.New("guard: closed")
	ErrGuardPolling     = errors.New("guard: already polling")
	ErrNotAuthenticated = errors.New("guard: not authenticated")
)

// Config holds all configuration options for the Guardian.
// Use DefaultConfig() for production-ready defaults.
type Config struct {
	// SteamID is the id of the account the guardian is connected to.
	SteamID uint64

	// SharedSecret is the TOTP secret used to generate 2FA codes.
	SharedSecret string

	// IdentitySecret is the TOTP secret used to generate confirmation keys.
	// Store this securely! Never log or expose it as it gives full access to your steam guard.
	IdentitySecret string

	// DeviceID is the mobile device identifier registered with Steam.
	// Format: "android:<unique_id>" or "ios:<unique_id>"
	// Must match the device used when enabling mobile confirmations.
	DeviceID string

	// PollInterval determines how often to check for new confirmations.
	// Default: 5 seconds
	// Minimum: 2 seconds (Steam rate limiting)
	PollInterval time.Duration

	// AutoAccept enables automatic acceptance of confirmations.
	// When true, confirmations matching AutoAcceptTypes are automatically accepted.
	// Default: false
	AutoAccept bool

	// AutoAcceptTypes specifies which confirmation types to auto-accept.
	// Only relevant when AutoAccept is true.
	// Example: {ConfTypeTrade, ConfTypeMarket}
	AutoAcceptTypes []ConfirmationType

	// MaxPollFailures is the number of consecutive failures before
	// increasing the poll interval (exponential backoff).
	// Default: 3
	MaxPollFailures int

	// MaxBackoff is the maximum poll interval when backing off due to errors.
	// Default: 30 seconds
	MaxBackoff time.Duration

	// RateLimit is the minimum time between API calls to Steam.
	// Prevents IP bans from excessive requests.
	// Default: 2 seconds
	RateLimit time.Duration
}

// DefaultConfig returns a Config with production-ready defaults.
func DefaultConfig() Config {
	return Config{
		PollInterval:    5 * time.Second,
		AutoAccept:      false,
		AutoAcceptTypes: []ConfirmationType{},
		MaxPollFailures: 3,
		MaxBackoff:      30 * time.Second,
		RateLimit:       2 * time.Second,
	}
}

// Validate checks if the configuration is valid for use.
// Returns an error if any required fields are missing or invalid.
func (c Config) Validate() error {
	if c.IdentitySecret == "" {
		return fmt.Errorf("identity secret is required")
	}
	if c.DeviceID == "" {
		return fmt.Errorf("device ID is required")
	}
	if c.PollInterval <= 0 {
		return fmt.Errorf("poll interval must be positive")
	}
	if c.MaxPollFailures < 0 {
		return fmt.Errorf("max poll failures cannot be negative")
	}
	if c.MaxBackoff < c.PollInterval {
		return fmt.Errorf("max backoff (%v) must be >= poll interval (%v)",
			c.MaxBackoff, c.PollInterval)
	}
	if c.RateLimit < 500*time.Millisecond {
		return fmt.Errorf("rate limit too aggressive (minimum 500ms)")
	}
	return nil
}

// GuardianMetrics tracks operational metrics for monitoring using atomics.
type GuardianMetrics struct {
	TotalFetched  atomic.Int64
	TotalAccepted atomic.Int64
	TotalRejected atomic.Int64
	TotalErrors   atomic.Int64
}

// Guardian manages Steam Guard mobile confirmations.
// The polling is started automatically once client is loaded.
type Guardian struct {
	bus *bus.Bus
	sub *bus.Subscription

	logger  log.Logger
	service *MobileConf
	config  Config
	state   atomic.Int32

	// Confirmation tracking
	mu            sync.RWMutex
	confirmations map[uint64]*Confirmation
	seenIDs       map[uint64]time.Time

	// Polling lifecycle
	pollingCtx    context.Context // polling
	pollingCancel context.CancelFunc
	wg            sync.WaitGroup

	rateLimiter *rate.Limiter
	metrics     *GuardianMetrics
}

// New creates a new confirmation guardian.
func New(cfg Config) (*Guardian, error) {
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("invalid guard config: %w", err)
	}

	g := &Guardian{
		config:        cfg,
		logger:        log.Discard,
		confirmations: make(map[uint64]*Confirmation),
		seenIDs:       make(map[uint64]time.Time),
		metrics:       &GuardianMetrics{},
		rateLimiter:   rate.NewLimiter(rate.Every(cfg.RateLimit), 1),
	}

	g.state.Store(int32(StateStopped))
	return g, nil
}

func (g *Guardian) Name() string { return ModuleName }

func (g *Guardian) Init(init steam.InitContext) error {
	g.bus = init.Bus()
	if g.bus == nil {
		return errors.New("nil bus")
	}

	g.logger = init.Logger().
		WithModule(ModuleName).
		With(log.String("device_id", maskDeviceID(g.config.DeviceID)))

	g.sub = init.Bus().Subscribe(auth.StateEvent{})
	go g.listenEvents()

	return nil
}

func (g *Guardian) Start(ctx context.Context) error {
	return nil
}

func (g *Guardian) StartAuthed(ctx context.Context, auth steam.AuthContext) error {
	g.mu.Lock()
	if g.pollingCancel != nil {
		g.pollingCancel()
	}

	communityClient := auth.Community()
	if communityClient == nil {
		return errors.New("nil community client despite successful auth")
	}
	g.service = NewMobileConf(communityClient)
	g.mu.Unlock()

	return g.StartPolling(ctx)
}

// Close stops the polling process and discards this module.
func (g *Guardian) Close() error {
	g.state.Store(int32(StateClosed))

	g.mu.Lock()
	if g.pollingCancel != nil {
		g.pollingCancel()
	}
	g.mu.Unlock()

	if g.sub != nil {
		g.sub.Unsubscribe()
	}

	return nil
}

func (g *Guardian) Metrics() *GuardianMetrics { return g.metrics }
func (g *Guardian) State() State              { return State(g.state.Load()) }

// StartPolling begins automatic confirmation polling in a background goroutine.
func (g *Guardian) StartPolling(ctx context.Context) error {
	if !g.state.CompareAndSwap(int32(StateStopped), int32(StatePolling)) {
		return ErrGuardPolling
	}

	g.mu.Lock()
	g.pollingCtx, g.pollingCancel = context.WithCancel(ctx)
	g.mu.Unlock()

	g.wg.Add(1)
	go g.pollingLoop()

	g.logger.Info("Polling started", log.Duration("interval", g.config.PollInterval))
	g.bus.Publish(&StateEvent{New: StatePolling})
	return nil
}

// StopPolling stops the automatic confirmation polling.
func (g *Guardian) StopPolling() {
	if g.state.CompareAndSwap(int32(StatePolling), int32(StateStopped)) {
		g.mu.Lock()
		if g.pollingCancel != nil {
			g.pollingCancel()
		}
		g.mu.Unlock()

		g.wg.Wait() // Wait for the loop to exit cleanly
		g.logger.Info("Polling stopped")
		g.bus.Publish(&StateEvent{New: StateStopped})
	}
}

// FetchConfirmations requests the list of active confirmations from Steam.
func (g *Guardian) FetchConfirmations(ctx context.Context) ([]*Confirmation, error) {
	if g.service == nil {
		return nil, ErrNotAuthenticated
	}

	// Respect rate limits before hitting Steam API
	if err := g.rateLimiter.Wait(ctx); err != nil {
		return nil, err
	}

	timestamp := time.Now().Unix()
	key, err := totp.GenerateConfirmationKey(g.config.IdentitySecret, timestamp, "conf")
	if err != nil {
		return nil, fmt.Errorf("key generation: %w", err)
	}

	resp, err := g.service.GetConfirmations(ctx, g.config.DeviceID, g.config.SteamID, key, timestamp)
	if err != nil {
		g.metrics.TotalErrors.Add(1)
		return nil, fmt.Errorf("api request failed: %w", err)
	}

	if !resp.Success {
		g.metrics.TotalErrors.Add(1)
		if resp.NeedAuth {
			g.bus.Publish(&NeedAuthEvent{Message: resp.Message})
		}
		return nil, fmt.Errorf("steam rejected conf request: %s", resp.Message)
	}

	g.metrics.TotalFetched.Add(int64(len(resp.Confirmations)))
	return resp.Confirmations, nil
}

// Accept approves a single confirmation.
func (g *Guardian) Accept(ctx context.Context, conf *Confirmation) error {
	return g.respond(ctx, conf, true)
}

// Cancel declines a single confirmation.
func (g *Guardian) Cancel(ctx context.Context, conf *Confirmation) error {
	return g.respond(ctx, conf, false)
}

func (g *Guardian) respond(ctx context.Context, conf *Confirmation, accept bool) error {
	if g.service == nil {
		return ErrNotAuthenticated
	}

	if err := g.rateLimiter.Wait(ctx); err != nil {
		return err
	}

	tag := "allow"
	if !accept {
		tag = "cancel"
	}

	timestamp := time.Now().Unix()
	key, err := totp.GenerateConfirmationKey(g.config.IdentitySecret, timestamp, tag)
	if err != nil {
		return err
	}

	err = g.service.RespondToConfirmation(ctx, conf, accept, g.config.DeviceID, g.config.SteamID, key, timestamp)
	if err != nil {
		g.metrics.TotalErrors.Add(1)
		return err
	}

	g.mu.Lock()
	delete(g.confirmations, conf.ID)
	g.mu.Unlock()

	if accept {
		g.metrics.TotalAccepted.Add(1)
	} else {
		g.metrics.TotalRejected.Add(1)
	}
	return nil
}

func (g *Guardian) pollingLoop() {
	defer g.wg.Done()

	interval := g.config.PollInterval
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	consecutiveFailures := 0

	for {
		select {
		case <-g.pollingCtx.Done():
			return
		case <-ticker.C:
			confs, err := g.FetchConfirmations(g.pollingCtx)
			if err != nil {
				if errors.Is(err, context.Canceled) {
					return
				}

				// Exponential Backoff
				consecutiveFailures++
				if consecutiveFailures > g.config.MaxPollFailures {
					interval = min(interval*2, g.config.MaxBackoff)
					ticker.Reset(interval)
					g.logger.Warn("Backed off polling due to errors", log.Duration("new_interval", interval))
				}
				continue
			}

			// Success - Reset backoff
			if consecutiveFailures > 0 {
				consecutiveFailures = 0
				interval = g.config.PollInterval
				ticker.Reset(interval)
			}

			g.processFetchedConfirmations(confs)
			g.cleanupSeenIDs()
		}
	}
}

func (g *Guardian) processFetchedConfirmations(confs []*Confirmation) {
	for _, conf := range confs {
		g.mu.Lock()
		if _, seen := g.seenIDs[conf.ID]; seen {
			g.mu.Unlock()
			continue
		}
		g.seenIDs[conf.ID] = time.Now()
		g.confirmations[conf.ID] = conf
		g.mu.Unlock()

		g.logger.Info("New confirmation received", log.String("title", conf.Title))
		g.bus.Publish(&ConfirmationReceivedEvent{Confirmation: conf})

		if g.config.AutoAccept && slices.Contains(g.config.AutoAcceptTypes, conf.Type) {
			go func(c *Confirmation) {
				if err := g.Accept(g.pollingCtx, c); err != nil {
					g.logger.Error("Auto-accept failed", log.Err(err), log.Uint64("id", c.ID))
				} else {
					g.logger.Info("Auto-accepted confirmation", log.Uint64("id", c.ID))
				}
			}(conf)
		}
	}
}

func (g *Guardian) cleanupSeenIDs() {
	g.mu.Lock()
	defer g.mu.Unlock()

	now := time.Now()
	for id, seenTime := range g.seenIDs {
		if now.Sub(seenTime) > 15*time.Minute {
			delete(g.seenIDs, id)
		}
	}
}

func (g *Guardian) listenEvents() {
	for ev := range g.sub.C() {
		switch e := ev.(type) {
		case *auth.StateEvent:
			g.handleStateChange(e)
		}
	}
}

func (g *Guardian) handleStateChange(e *auth.StateEvent) {
	switch e.New {
	case auth.StateLoggedOn:
	case auth.StateDisconnected:
		g.logger.Warn("Disconnected, stopping session context")
		g.StopPolling()
	}
}

func maskDeviceID(deviceID string) string {
	if len(deviceID) <= 8 {
		return "****"
	}
	return deviceID[:4] + "..." + deviceID[len(deviceID)-4:]
}

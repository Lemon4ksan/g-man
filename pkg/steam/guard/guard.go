// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package guard

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/time/rate"

	"github.com/lemon4ksan/g-man/pkg/bus"
	"github.com/lemon4ksan/g-man/pkg/crypto"
	"github.com/lemon4ksan/g-man/pkg/log"
	"github.com/lemon4ksan/g-man/pkg/steam"
	"github.com/lemon4ksan/g-man/pkg/steam/auth"
	"github.com/lemon4ksan/g-man/pkg/steam/id"
	"github.com/lemon4ksan/g-man/pkg/steam/module"
)

const ModuleName string = "guard"

// State represents the lifecycle state of the high-level client.
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

var (
	ErrGuardClosed      = errors.New("guard: closed")
	ErrGuardPolling     = errors.New("guard: already polling")
	ErrNotAuthenticated = errors.New("guard: not authenticated")
)

type ConfService interface {
	GetConfirmations(ctx context.Context, deviceID string, steamID id.ID, confKey string, timestamp int64) (*ConfirmationsList, error)
	RespondToConfirmation(ctx context.Context, conf *Confirmation, accept bool, deviceID string, steamID id.ID, confKey string, timestamp int64) error
	RespondToMultiple(ctx context.Context, confs []*Confirmation, accept bool, deviceID string, steamID id.ID, confKey string, timestamp int64) error
}

// Config holds all configuration options for the Guardian.
// Use DefaultConfig() for production-ready defaults.
type Config struct {
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
	if !strings.HasPrefix(c.DeviceID, "android:") && !strings.HasPrefix(c.DeviceID, "ios:") {
		return fmt.Errorf("device ID must start with 'android:' or 'ios:'")
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
	return nil
}

func (c Config) String() string {
	return fmt.Sprintf("GuardConfig{DeviceID: %s, PollInterval: %s, AutoAccept: %v}",
		maskDeviceID(c.DeviceID), c.PollInterval, c.AutoAccept)
}

func WithModule(cfg Config) steam.Option {
	return func(c *steam.Client) {
		m, err := New(cfg)
		if err != nil {
			c.Logger().Error("Failed to register guardian", log.Err(err))
		} else {
			c.RegisterModule(m)
		}
	}
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
	module.Base

	steamID      id.ID
	service      ConfService
	config       Config
	clock        *OffsetClock
	twoFactorSvc *TwoFactorService

	// Confirmation tracking
	mu            sync.RWMutex
	pollingWg     sync.WaitGroup
	confirmations map[uint64]*Confirmation
	seenIDs       map[uint64]time.Time

	metrics     *GuardianMetrics
	rateLimiter *rate.Limiter
}

// New creates a new confirmation guardian instance.
func New(cfg Config) (*Guardian, error) {
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("invalid guard config: %w", err)
	}

	g := &Guardian{
		Base:          module.New(ModuleName),
		config:        cfg,
		clock:         &OffsetClock{},
		confirmations: make(map[uint64]*Confirmation),
		seenIDs:       make(map[uint64]time.Time),
		metrics:       &GuardianMetrics{},
		rateLimiter:   rate.NewLimiter(rate.Every(cfg.RateLimit), 1),
	}

	g.State.Store(StateStopped)
	return g, nil
}

func (g *Guardian) Metrics() *GuardianMetrics { return g.metrics }

// Init initializes the module dependencies and starts background event listeners.
func (g *Guardian) Init(init module.InitContext) error {
	if err := g.Base.Init(init); err != nil {
		return err
	}

	if web := init.Service(); web != nil {
		g.twoFactorSvc = NewTwoFactorService(web)
	}

	g.Logger = g.Logger.With(log.String("device_id", maskDeviceID(g.config.DeviceID)))
	sub := init.Bus().Subscribe(auth.StateEvent{})

	g.Go(func(ctx context.Context) {
		g.listenEvents(ctx, sub)
	})

	return nil
}

// StartAuthed is called when the Steam Client successfully logs in.
// It synchronizes time and starts the confirmation polling loop.
func (g *Guardian) StartAuthed(ctx context.Context, authCtx module.AuthContext) error {
	if g.State.Load() == int32(StatePolling) {
		g.Logger.Debug("Re-authentication detected, stopping old polling loop")
		g.StopPolling()
	}

	g.steamID = authCtx.SteamID()

	communityClient := authCtx.Community()
	if communityClient == nil {
		return errors.New("guard: community client is required")
	}

	g.mu.Lock()
	g.service = NewMobileConf(communityClient)

	if g.twoFactorSvc != nil {
		offset, err := g.twoFactorSvc.QueryTimeOffset(ctx)
		if err == nil {
			g.clock.SetOffset(offset)
		}
	}
	g.mu.Unlock()

	return g.StartPolling()
}

// Close gracefully shuts down the module and waits for all goroutines.
func (g *Guardian) Close() error {
	g.State.Store(StateClosed)
	return g.Base.Close()
}

// StartPolling begins automatic confirmation polling in a background goroutine.
func (g *Guardian) StartPolling() error {
	if !g.State.CompareAndSwap(int32(StateStopped), int32(StatePolling)) {
		return ErrGuardPolling
	}

	g.pollingWg.Add(1)

	g.Go(func(ctx context.Context) {
		defer g.pollingWg.Done()
		g.pollingLoop(ctx)
	})

	g.Logger.Info("Polling started", log.Duration("interval", g.config.PollInterval))
	g.Bus.Publish(&StateEvent{New: State(StatePolling)})
	return nil
}

// StopPolling halts the automatic polling loop.
func (g *Guardian) StopPolling() {
	if g.State.CompareAndSwap(int32(StatePolling), int32(StateStopped)) {
		g.pollingWg.Wait()
		g.Logger.Info("Polling loop fully terminated")
		g.Bus.Publish(&StateEvent{New: State(StateStopped)})
	}
}

// FetchConfirmations requests the list of active confirmations from Steam.
func (g *Guardian) FetchConfirmations(ctx context.Context) ([]*Confirmation, error) {
	if g.service == nil {
		return nil, ErrNotAuthenticated
	}

	if err := g.rateLimiter.Wait(ctx); err != nil {
		return nil, err
	}

	timestamp := g.clock.Now().Unix()
	key, err := crypto.GenerateConfirmationKey(g.config.IdentitySecret, timestamp, "conf")
	if err != nil {
		return nil, fmt.Errorf("guard: key generation: %w", err)
	}

	resp, err := g.service.GetConfirmations(ctx, g.config.DeviceID, g.steamID, key, timestamp)
	if err != nil {
		g.metrics.TotalErrors.Add(1)
		return nil, err
	}

	if !resp.Success {
		g.metrics.TotalErrors.Add(1)
		if resp.NeedAuth {
			g.Bus.Publish(&NeedAuthEvent{Message: resp.Message})
		}
		return nil, fmt.Errorf("guard: steam rejected request: %s", resp.Message)
	}

	g.metrics.TotalFetched.Add(int64(len(resp.Confirmations)))
	return resp.Confirmations, nil
}

// Accept approves a single confirmation.
func (g *Guardian) Accept(ctx context.Context, conf *Confirmation) error {
	return g.respond(ctx, conf, true)
}

// AcceptMultiple accepts multiple confirmations at once (uses multiajaxop).
func (g *Guardian) AcceptMultiple(ctx context.Context, confs []*Confirmation) error {
	return g.respondMultiple(ctx, confs, true)
}

// Cancel declines a single confirmation.
func (g *Guardian) Cancel(ctx context.Context, conf *Confirmation) error {
	return g.respond(ctx, conf, false)
}

// CancelMultiple rejects multiple confirmations at once.
func (g *Guardian) CancelMultiple(ctx context.Context, confs []*Confirmation) error {
	return g.respondMultiple(ctx, confs, false)
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
	key, err := crypto.GenerateConfirmationKey(g.config.IdentitySecret, timestamp, tag)
	if err != nil {
		return err
	}

	err = g.service.RespondToConfirmation(ctx, conf, accept, g.config.DeviceID, g.steamID, key, timestamp)
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

func (g *Guardian) respondMultiple(ctx context.Context, confs []*Confirmation, accept bool) error {
	if g.service == nil {
		return ErrNotAuthenticated
	}
	if len(confs) == 0 {
		return nil
	}

	if err := g.rateLimiter.Wait(ctx); err != nil {
		return err
	}

	tag := "allow"
	if !accept {
		tag = "cancel"
	}

	timestamp := time.Now().Unix()
	key, err := crypto.GenerateConfirmationKey(g.config.IdentitySecret, timestamp, tag)
	if err != nil {
		return err
	}

	err = g.service.RespondToMultiple(ctx, confs, accept, g.config.DeviceID, g.steamID, key, timestamp)
	if err != nil {
		g.metrics.TotalErrors.Add(1)
		return err
	}

	g.mu.Lock()
	for _, conf := range confs {
		delete(g.confirmations, conf.ID)
	}
	g.mu.Unlock()

	if accept {
		g.metrics.TotalAccepted.Add(int64(len(confs)))
	} else {
		g.metrics.TotalRejected.Add(int64(len(confs)))
	}
	return nil
}

// pollingLoop handles the ticker logic and exponential backoff on failures.
func (g *Guardian) pollingLoop(ctx context.Context) {
	interval := g.config.PollInterval
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	consecutiveFailures := 0

	for {
		if g.State.Load() != int32(StatePolling) {
			return
		}

		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			// Check if we should still be polling based on module state
			if g.State.Load() != int32(StatePolling) {
				g.Logger.Debug("Exiting polling loop as state is no longer Polling")
				return
			}

			confs, err := g.FetchConfirmations(ctx)
			if err != nil {
				consecutiveFailures++
				if consecutiveFailures > g.config.MaxPollFailures {
					interval = min(interval*2, g.config.MaxBackoff)
					ticker.Reset(interval)
					g.Logger.Warn("Backed off polling due to errors", log.Duration("new_interval", interval))
				}
				continue
			}

			// Success - Reset backoff and interval
			if consecutiveFailures > 0 {
				consecutiveFailures = 0
				interval = g.config.PollInterval
				ticker.Reset(interval)
			}

			g.processFetchedConfirmations(ctx, confs)
			g.cleanupSeenIDs()
		}
	}
}

func (g *Guardian) processFetchedConfirmations(ctx context.Context, confs []*Confirmation) {
	var toAutoAccept []*Confirmation

	for _, conf := range confs {
		g.mu.Lock()
		if _, seen := g.seenIDs[conf.ID]; seen {
			g.mu.Unlock()
			continue
		}
		g.seenIDs[conf.ID] = time.Now()
		g.confirmations[conf.ID] = conf
		g.mu.Unlock()

		g.Logger.Info("New confirmation received", log.String("title", conf.Title))
		g.Bus.Publish(&ConfirmationReceivedEvent{Confirmation: conf})

		if g.config.AutoAccept && slices.Contains(g.config.AutoAcceptTypes, conf.Type) {
			toAutoAccept = append(toAutoAccept, conf)
		}
	}

	if len(toAutoAccept) > 0 {
		g.Go(func(workerCtx context.Context) {
			if len(toAutoAccept) == 1 {
				conf := toAutoAccept[0]
				if err := g.Accept(workerCtx, conf); err != nil {
					g.Logger.Error("Auto-accept failed", log.Err(err), log.Uint64("id", conf.ID))
				} else {
					g.Logger.Info("Auto-accepted confirmation", log.Uint64("id", conf.ID))
				}
			} else {
				if err := g.AcceptMultiple(workerCtx, toAutoAccept); err != nil {
					g.Logger.Error("Auto-accept multiple failed", log.Err(err), log.Int("count", len(toAutoAccept)))
				} else {
					g.Logger.Info("Auto-accepted multiple confirmations", log.Int("count", len(toAutoAccept)))
				}
			}
		})
	}
}

// listenEvents handles background event processing for auth state changes.
func (g *Guardian) listenEvents(ctx context.Context, sub *bus.Subscription) {
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
					g.StopPolling()
				}
			}
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

func (g *Guardian) handleStateChange(e *auth.StateEvent) {
	switch e.New {
	case auth.StateLoggedOn:
	case auth.StateDisconnected:
		g.Logger.Warn("Disconnected, stopping session context")
		g.StopPolling()
	}
}

func maskDeviceID(deviceID string) string {
	if len(deviceID) <= 8 {
		return "****"
	}
	return deviceID[:4] + "..." + deviceID[len(deviceID)-4:]
}

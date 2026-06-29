// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package guard

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/lemon4ksan/miyako/batto"
	"github.com/lemon4ksan/miyako/generic"
	"github.com/lemon4ksan/miyako/sync/lazy"
	"golang.org/x/time/rate"

	"github.com/lemon4ksan/g-man/pkg/crypto"
	"github.com/lemon4ksan/g-man/pkg/log"
	pb "github.com/lemon4ksan/g-man/pkg/protobuf/steam"
	"github.com/lemon4ksan/g-man/pkg/steam"
	"github.com/lemon4ksan/g-man/pkg/steam/id"
	"github.com/lemon4ksan/g-man/pkg/steam/module"
)

// ModuleName is the unique identifier for the guard module.
const ModuleName string = "guard"

// WithModule returns a steam.Option that registers the guardian module in the client.
func WithModule(config Config) steam.Option {
	m, err := New(config)
	if err != nil {
		return func(client *steam.Client) {
			client.Logger().Error("Failed to register guardian", log.Err(err))
		}
	}

	return steam.WithModule(m)
}

// From returns the guardian module from the client.
func From(client *steam.Client) *Guardian {
	return steam.GetModule[*Guardian](client)
}

// PollingState represents the operational polling status of the Guardian.
type PollingState int32

const (
	// PollingStopped indicates that confirmation polling is currently inactive.
	PollingStopped PollingState = iota
	// PollingActive indicates that the guardian is actively polling for confirmations.
	PollingActive
)

// String returns a human-readable representation of PollingState.
func (s PollingState) String() string {
	switch s {
	case PollingStopped:
		return "stopped"
	case PollingActive:
		return "polling"
	default:
		return "unknown"
	}
}

var (
	// ErrGuardClosed is returned when an operation is performed on a closed guardian.
	ErrGuardClosed = errors.New("guard: closed")
	// ErrNotAuthenticated is returned when the guardian is not yet linked to a session.
	ErrNotAuthenticated = errors.New("guard: not authenticated")
	// ErrNotConfigured is returned when the guardian is not configured.
	ErrNotConfigured = errors.New("guard: not configured")
)

// ConfService defines the interface for interacting with Steam's mobile confirmation endpoints.
type ConfService interface {
	GetConfirmations(
		ctx context.Context,
		deviceID string,
		steamID id.ID,
		confKey string,
		timestamp int64,
	) (*ConfirmationsList, error)
	RespondToConfirmation(
		ctx context.Context,
		conf *Confirmation,
		accept bool,
		deviceID string,
		steamID id.ID,
		confKey string,
		timestamp int64,
	) error
	RespondToMultiple(
		ctx context.Context,
		confs []*Confirmation,
		accept bool,
		deviceID string,
		steamID id.ID,
		confKey string,
		timestamp int64,
	) error
}

// Config holds all configuration options for the Guardian.
type Config struct {
	// SharedSecret is the TOTP secret used to generate 2FA codes.
	SharedSecret string
	// IdentitySecret is the TOTP secret used to generate confirmation keys.
	IdentitySecret string
	// DeviceID is the mobile device identifier (e.g., "android:...").
	DeviceID string
	// RateLimit is the minimum time between API calls to Steam.
	RateLimit time.Duration
}

// DefaultConfig returns a Config with production-ready defaults.
func DefaultConfig() Config {
	return Config{
		RateLimit: 2 * time.Second,
	}
}

// Validate checks if the configuration is valid for use.
func (c Config) Validate() error {
	if c.IdentitySecret == "" {
		return errors.New("identity secret is required")
	}

	if c.DeviceID == "" {
		return errors.New("device ID is required")
	}

	if !strings.HasPrefix(c.DeviceID, "android:") && !strings.HasPrefix(c.DeviceID, "ios:") {
		return errors.New("device ID must start with 'android:' or 'ios:'")
	}

	return nil
}

// String returns a masked representation of the config for logging.
func (c Config) String() string {
	return fmt.Sprintf("GuardConfig{DeviceID: %s}", maskDeviceID(c.DeviceID))
}

// GuardianMetrics tracks operational metrics for monitoring using atomics.
type GuardianMetrics struct {
	// TotalFetched is the total number of confirmations retrieved.
	TotalFetched atomic.Int64
	// TotalAccepted is the total number of confirmations successfully approved.
	TotalAccepted atomic.Int64
	// TotalRejected is the total number of confirmations successfully declined.
	TotalRejected atomic.Int64
	// TotalErrors is the total number of API errors encountered.
	TotalErrors atomic.Int64
}

// Guardian manages Steam Guard mobile confirmations.
// It acts as a mechanism provider, while decision-making is delegated to behaviors.
//
// Use [New] to construct new instances of Guardian. It integrates with
// [Config] to manage device credentials, and exposes [GuardianMetrics] for monitoring.
type Guardian struct {
	module.AuthBase

	service      ConfService
	config       Config
	clock        *OffsetClock
	twoFactorSvc *lazy.Lazy[*TwoFactorService]

	pollingState PollingState

	// Confirmation tracking
	mu            sync.RWMutex
	confirmations map[uint64]*Confirmation
	seenIDs       map[uint64]time.Time

	metrics     *GuardianMetrics
	rateLimiter *rate.Limiter
	fetchGroup  *batto.Group[string, []*Confirmation]
}

// New creates a new confirmation guardian instance.
func New(config Config) (*Guardian, error) {
	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("invalid guard config: %w", err)
	}

	g := &Guardian{
		AuthBase:      module.NewAuthBase(ModuleName),
		config:        config,
		clock:         new(OffsetClock),
		confirmations: make(map[uint64]*Confirmation),
		seenIDs:       make(map[uint64]time.Time),
		metrics:       new(GuardianMetrics),
		rateLimiter:   rate.NewLimiter(rate.Every(config.RateLimit), 1),
		pollingState:  PollingStopped,
		fetchGroup:    new(batto.Group[string, []*Confirmation]),
	}

	return g, nil
}

// Init initializes the module dependencies.
func (g *Guardian) Init(init module.InitContext) error {
	if err := g.Base.Init(init); err != nil {
		return err
	}

	if web := init.Service(); web != nil {
		g.twoFactorSvc = lazy.New(func() (*TwoFactorService, error) {
			return NewTwoFactorService(web), nil
		})
	}

	g.Logger = g.Logger.With(log.String("device_id", maskDeviceID(g.config.DeviceID)))

	return nil
}

// StartAuthed starts the guardian in an authenticated state.
func (g *Guardian) StartAuthed(ctx context.Context, auth module.AuthContext) error {
	if err := g.AuthBase.StartAuthed(ctx, auth); err != nil {
		return err
	}

	if g.Community() == nil {
		return errors.New("guard: community client is required")
	}

	g.mu.Lock()
	g.service = NewMobileConf(g.Community())
	g.mu.Unlock()

	g.synchronizeTimeOffset(ctx)
	g.logGuardStatus(ctx, auth)

	return nil
}

// SetConfig dynamically updates the guardian configuration.
func (g *Guardian) SetConfig(config Config) {
	g.mu.Lock()
	defer g.mu.Unlock()

	g.config = config
}

// Config returns the current guardian configuration.
func (g *Guardian) Config() Config {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return g.config
}

// Service returns the confirmation service used by the guardian.
//
// If the service is not yet initialized by [steam.Client], it returns nil.
func (g *Guardian) Service() ConfService {
	return g.service
}

// Metrics returns the operational metrics of the guardian.
func (g *Guardian) Metrics() *GuardianMetrics { return g.metrics }

// PollingState returns the current operational polling status.
func (g *Guardian) PollingState() PollingState {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return g.pollingState
}

// GenerateAuthCode generates a 5-digit Steam Guard code for the current time.
// It returns an empty string if the shared secret is not configured.
func (g *Guardian) GenerateAuthCode() (string, error) {
	if g == nil || g.config.SharedSecret == "" {
		return "", nil
	}

	return crypto.GenerateAuthCode(g.config.SharedSecret, g.clock.Now().Unix())
}

// FetchConfirmations requests the list of active confirmations from Steam.
//
// It returns an error if the request fails, if Steam rejects the request,
// or if the identity secret is invalid. It increments the TotalErrors metric
// and TotalFetched metric in [GuardianMetrics] accordingly.
func (g *Guardian) FetchConfirmations(ctx context.Context) ([]*Confirmation, error) {
	if g == nil {
		return nil, ErrNotConfigured
	}

	if g.service == nil {
		return nil, ErrNotAuthenticated
	}

	if err := g.rateLimiter.Wait(ctx); err != nil {
		return nil, err
	}

	return g.fetchGroup.Do(ctx, "fetch_confirmations", func(workerCtx context.Context) ([]*Confirmation, error) {
		return g.executeFetch(workerCtx)
	})
}

// Accept approves a single confirmation.
func (g *Guardian) Accept(ctx context.Context, confirmation *Confirmation) error {
	return g.respond(ctx, []*Confirmation{confirmation}, true)
}

// AcceptMultiple accepts multiple confirmations at once.
func (g *Guardian) AcceptMultiple(ctx context.Context, confirmations []*Confirmation) error {
	return g.respond(ctx, confirmations, true)
}

// Cancel declines a single confirmation.
func (g *Guardian) Cancel(ctx context.Context, confirmation *Confirmation) error {
	return g.respond(ctx, []*Confirmation{confirmation}, false)
}

// CancelMultiple rejects multiple confirmations at once.
func (g *Guardian) CancelMultiple(ctx context.Context, confirmations []*Confirmation) error {
	return g.respond(ctx, confirmations, false)
}

// Close shuts down the guardian module.
func (g *Guardian) Close() error {
	_ = g.Fsm.Transition(context.Background(), module.EventClose)
	return g.Base.Close()
}

func (g *Guardian) synchronizeTimeOffset(ctx context.Context) {
	if g.twoFactorSvc == nil {
		return
	}

	offsetFuture := generic.NewFuture(func() (time.Duration, error) {
		svc, err := g.twoFactorSvc.Get()
		if err != nil || svc == nil {
			return 0, err
		}

		return svc.QueryTimeOffset(ctx)
	})

	if offset, err := offsetFuture.Get(ctx); err == nil && offset != 0 {
		g.clock.SetOffset(offset)
		g.Logger.Debug("Time offset synchronized", log.Duration("offset", offset))
	}
}

func (g *Guardian) logGuardStatus(ctx context.Context, auth module.AuthContext) {
	if g.twoFactorSvc == nil {
		return
	}

	statusFuture := generic.NewFuture(func() (*pb.CTwoFactor_Status_Response, error) {
		svc, err := g.twoFactorSvc.Get()
		if err != nil || svc == nil {
			return nil, err
		}

		return svc.QueryStatus(ctx, auth.SteamID())
	})

	if status, err := statusFuture.Get(ctx); err == nil && status != nil {
		g.Logger.Info("Steam Guard Status loaded",
			log.String("device_id", status.GetDeviceIdentifier()),
		)
	}
}

func (g *Guardian) executeFetch(ctx context.Context) ([]*Confirmation, error) {
	if err := g.rateLimiter.Wait(ctx); err != nil {
		return nil, err
	}

	timestamp := g.clock.Now().Unix()

	key, err := crypto.GenerateConfirmationKey(g.config.IdentitySecret, timestamp, "conf")
	if err != nil {
		return nil, fmt.Errorf("guard: key generation: %w", err)
	}

	resp, err := g.service.GetConfirmations(ctx, g.config.DeviceID, g.SteamID(), key, timestamp)
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

func (g *Guardian) respond(ctx context.Context, confirmations []*Confirmation, accept bool) error {
	if g == nil {
		return ErrNotConfigured
	}

	if err := g.rateLimiter.Wait(ctx); err != nil {
		return err
	}

	timestamp := g.clock.Now().Unix()

	key, err := crypto.GenerateConfirmationKey(g.config.IdentitySecret, timestamp, resolveTag(accept))
	if err != nil {
		return err
	}

	if err := g.executeResponse(ctx, confirmations, accept, key, timestamp); err != nil {
		g.metrics.TotalErrors.Add(1)
		return err
	}

	g.updateMetrics(len(confirmations), accept)

	return nil
}

func resolveTag(accept bool) string {
	if accept {
		return "allow"
	}

	return "cancel"
}

func (g *Guardian) executeResponse(
	ctx context.Context,
	confirmations []*Confirmation,
	accept bool,
	key string,
	timestamp int64,
) error {
	if len(confirmations) == 1 {
		return g.service.RespondToConfirmation(
			ctx,
			confirmations[0],
			accept,
			g.config.DeviceID,
			g.SteamID(),
			key,
			timestamp,
		)
	}

	return g.service.RespondToMultiple(ctx, confirmations, accept, g.config.DeviceID, g.SteamID(), key, timestamp)
}

func (g *Guardian) updateMetrics(count int, accept bool) {
	if accept {
		g.metrics.TotalAccepted.Add(int64(count))
	} else {
		g.metrics.TotalRejected.Add(int64(count))
	}
}

func maskDeviceID(deviceID string) string {
	if len(deviceID) <= 8 {
		return "****"
	}

	return deviceID[:4] + "..." + deviceID[len(deviceID)-4:]
}

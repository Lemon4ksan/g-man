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

	"golang.org/x/time/rate"

	"github.com/lemon4ksan/g-man/pkg/crypto"
	"github.com/lemon4ksan/g-man/pkg/log"
	"github.com/lemon4ksan/g-man/pkg/steam"
	"github.com/lemon4ksan/g-man/pkg/steam/id"
	"github.com/lemon4ksan/g-man/pkg/steam/module"
)

// ModuleName is the unique identifier for the guard module.
const ModuleName string = "guard"

// State represents the lifecycle state of the Guardian module.
type State int32

const (
	// StateStopped indicates that polling is not active.
	StateStopped State = iota
	// StatePolling indicates that the module is actively checking for confirmations.
	StatePolling
	// StateClosed indicates the module has been shut down.
	StateClosed
)

// String returns a human-readable representation of the State.
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
	// ErrGuardClosed is returned when an operation is performed on a closed guardian.
	ErrGuardClosed = errors.New("guard: closed")
	// ErrNotAuthenticated is returned when the guardian is not yet linked to a session.
	ErrNotAuthenticated = errors.New("guard: not authenticated")
	// ErrNotConfigured is returned when the guardian is not configured (e.g. invalid config or missing credentials).
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

// WithModule returns a steam.Option that registers the guardian module in the client.
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

// From returns the guardian module from the client.
func From(c *steam.Client) *Guardian {
	return steam.GetModule[*Guardian](c)
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
	module.Base

	steamID      id.ID
	service      ConfService
	config       Config
	clock        *OffsetClock
	twoFactorSvc *TwoFactorService

	// Confirmation tracking
	mu            sync.RWMutex
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

	return g, nil
}

// Init initializes the module dependencies.
func (g *Guardian) Init(init module.InitContext) error {
	if err := g.Base.Init(init); err != nil {
		return err
	}

	if web := init.Service(); web != nil {
		g.twoFactorSvc = NewTwoFactorService(web)
	}

	g.Logger = g.Logger.With(log.String("device_id", maskDeviceID(g.config.DeviceID)))

	return nil
}

// StartAuthed is called when the Steam Client successfully logs in.
// It synchronizes time.
func (g *Guardian) StartAuthed(ctx context.Context, authCtx module.AuthContext) error {
	communityClient := authCtx.Community()
	if communityClient == nil {
		return errors.New("guard: community client is required")
	}

	g.mu.Lock()
	g.steamID = authCtx.SteamID()
	g.service = NewMobileConf(communityClient)

	if g.twoFactorSvc != nil {
		offset, err := g.twoFactorSvc.QueryTimeOffset(ctx)
		if err == nil {
			g.clock.SetOffset(offset)
		}
	}

	g.mu.Unlock()

	return nil
}

// Service returns the confirmation service used by the guardian.
//
// If the service is not yet initialized by [steam.Client], it returns nil.
func (g *Guardian) Service() ConfService {
	return g.service
}

// Metrics returns the operational metrics of the guardian.
func (g *Guardian) Metrics() *GuardianMetrics { return g.metrics }

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
//
// It returns an error if the approval action is rejected by Steam. On failure,
// the TotalErrors metric in [GuardianMetrics] is incremented.
func (g *Guardian) Accept(ctx context.Context, conf *Confirmation) error {
	return g.respond(ctx, []*Confirmation{conf}, true)
}

// AcceptMultiple accepts multiple confirmations at once (uses multiajaxop).
//
// It returns an error if any of the approvals fail. On failure,
// the TotalErrors metric in [GuardianMetrics] is incremented.
func (g *Guardian) AcceptMultiple(ctx context.Context, confs []*Confirmation) error {
	return g.respond(ctx, confs, true)
}

// Cancel declines a single confirmation.
//
// It returns an error if the cancel action is rejected by Steam. On failure,
// the TotalErrors metric in [GuardianMetrics] is incremented.
func (g *Guardian) Cancel(ctx context.Context, conf *Confirmation) error {
	return g.respond(ctx, []*Confirmation{conf}, false)
}

// CancelMultiple rejects multiple confirmations at once.
//
// It returns an error if any of the rejections fail. On failure,
// the TotalErrors metric in [GuardianMetrics] is incremented.
func (g *Guardian) CancelMultiple(ctx context.Context, confs []*Confirmation) error {
	return g.respond(ctx, confs, false)
}

func (g *Guardian) respond(ctx context.Context, confs []*Confirmation, accept bool) error {
	if g == nil {
		return ErrNotConfigured
	}

	if err := g.rateLimiter.Wait(ctx); err != nil {
		return err
	}

	tag := "allow"
	if !accept {
		tag = "cancel"
	}

	timestamp := g.clock.Now().Unix()

	key, err := crypto.GenerateConfirmationKey(g.config.IdentitySecret, timestamp, tag)
	if err != nil {
		return err
	}

	if len(confs) == 1 {
		err = g.service.RespondToConfirmation(ctx, confs[0], accept, g.config.DeviceID, g.steamID, key, timestamp)
	} else {
		err = g.service.RespondToMultiple(ctx, confs, accept, g.config.DeviceID, g.steamID, key, timestamp)
	}

	if err != nil {
		g.metrics.TotalErrors.Add(1)
		return err
	}

	count := int64(len(confs))
	if accept {
		g.metrics.TotalAccepted.Add(count)
	} else {
		g.metrics.TotalRejected.Add(count)
	}

	return nil
}

// Close shuts down the guardian module.
func (g *Guardian) Close() error {
	g.State.Store(int32(StateClosed))
	return g.Base.Close()
}

func maskDeviceID(deviceID string) string {
	if len(deviceID) <= 8 {
		return "****"
	}

	return deviceID[:4] + "..." + deviceID[len(deviceID)-4:]
}

// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package guard manages Steam Guard Mobile Authenticator 2FA code generation and mobile confirmation approvals.
package guard

import (
	"context"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/lemon4ksan/miyako/batto"
	"github.com/lemon4ksan/miyako/generic"
	"github.com/lemon4ksan/miyako/log"
	"github.com/lemon4ksan/miyako/sync/lazy"
	"golang.org/x/time/rate"

	"github.com/lemon4ksan/g-man/internal/bytesconv"
	"github.com/lemon4ksan/g-man/internal/clock"
	"github.com/lemon4ksan/g-man/internal/crypto"
	pb "github.com/lemon4ksan/g-man/pkg/protobuf/steam"
	"github.com/lemon4ksan/g-man/pkg/steam"
	"github.com/lemon4ksan/g-man/pkg/steam/id"
	"github.com/lemon4ksan/g-man/pkg/steam/module"
)

const ModuleName string = "guard"

// WithModule registers the Guardian module in the client.
func WithModule(config Config) steam.Option {
	m, err := New(config)
	if err != nil {
		return func(client *steam.Client) {
			client.Logger().Error("Failed to register guardian", log.Err(err))
		}
	}

	return steam.WithModule(m)
}

// From retrieves the Guardian module from the client.
func From(client *steam.Client) *Guardian {
	return steam.GetModule[*Guardian](client)
}

var (
	// ErrInvalidSecret indicates an invalid secret encoding.
	ErrInvalidSecret = errors.New("crypto: invalid secret encoding")
	// ErrEmptySecret indicates an empty secret string was provided.
	ErrEmptySecret = errors.New("crypto: secret cannot be empty")
	// ErrIdentitySecretRequired indicates identity secret parameter is missing.
	ErrIdentitySecretRequired = errors.New("guard: identity secret is required")
	// ErrDeviceIDRequired indicates device ID parameter is missing.
	ErrDeviceIDRequired = errors.New("guard: device ID is required")
	// ErrDeviceIDInvalidPrefix indicates device ID lacks required 'android:' or 'ios:' prefix.
	ErrDeviceIDInvalidPrefix = errors.New("guard: device ID must start with 'android:' or 'ios:'")
	// ErrGuardClosed indicates operation was attempted on a closed Guardian module.
	ErrGuardClosed = errors.New("guard: closed")
	// ErrNotAuthenticated indicates the Guardian module lacks an active community session.
	ErrNotAuthenticated = errors.New("guard: not authenticated")
	// ErrNotConfigured indicates Guardian was used without required configurations.
	ErrNotConfigured = errors.New("guard: not configured")
	// ErrCommunityRequired indicates community requester is unavailable.
	ErrCommunityRequired = errors.New("guard: community client is required")
)

// DecodeSecret decodes hex or Base64 secret strings into raw bytes.
func DecodeSecret(secret string) ([]byte, error) {
	return decodeSecret(secret)
}

func decodeSecret(secret string) ([]byte, error) {
	if len(secret) == 0 {
		return nil, ErrEmptySecret
	}

	if len(secret) == 40 {
		if b, err := hex.DecodeString(secret); err == nil {
			return b, nil
		}
	}

	if b, err := base64.StdEncoding.DecodeString(secret); err == nil {
		return b, nil
	}

	if b, err := base64.RawStdEncoding.DecodeString(secret); err == nil {
		return b, nil
	}

	if b, err := base64.URLEncoding.DecodeString(secret); err == nil {
		return b, nil
	}

	if b, err := base64.RawURLEncoding.DecodeString(secret); err == nil {
		return b, nil
	}

	return nil, ErrInvalidSecret
}

type PollingState int32

const (
	PollingStopped PollingState = iota
	PollingActive
)

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

// ConfService defines mobile confirmation API endpoints.
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

// Config configures shared/identity secrets and rate limits for Guardian.
type Config struct {
	SharedSecret   string
	IdentitySecret string
	DeviceID       string
	RateLimit      time.Duration
}

// DefaultConfig builds default Guardian options.
func DefaultConfig() Config {
	return Config{
		RateLimit: 2 * time.Second,
	}
}

// Validate checks configuration field constraints.
func (c Config) Validate() error {
	if c.IdentitySecret == "" {
		return ErrIdentitySecretRequired
	}

	if c.DeviceID == "" {
		return ErrDeviceIDRequired
	}

	if !strings.HasPrefix(c.DeviceID, "android:") && !strings.HasPrefix(c.DeviceID, "ios:") {
		return ErrDeviceIDInvalidPrefix
	}

	return nil
}

func (c Config) String() string {
	return fmt.Sprintf("GuardConfig{DeviceID: %s}", maskDeviceID(c.DeviceID))
}

// GuardianMetrics tracks total confirmations fetched, accepted, and rejected.
type GuardianMetrics struct {
	TotalFetched  atomic.Int64
	TotalAccepted atomic.Int64
	TotalRejected atomic.Int64
	TotalErrors   atomic.Int64
}

// Guardian generates 2FA TOTP codes and manages mobile confirmations.
//
// Thread Safety:
//   - Safe for concurrent access across goroutines.
type Guardian struct {
	module.AuthBase

	service      ConfService
	config       Config
	clock        *clock.OffsetClock
	twoFactorSvc *lazy.Lazy[*TwoFactorService]

	pollingState PollingState

	mu            sync.RWMutex
	confirmations map[uint64]*Confirmation
	seenIDs       map[uint64]time.Time

	metrics     *GuardianMetrics
	rateLimiter *rate.Limiter
	fetchGroup  *batto.Group[string, []*Confirmation]

	sharedSecretBytes   [20]byte
	identitySecretBytes [20]byte
}

// New initializes a Guardian module instance.
func New(config Config) (*Guardian, error) {
	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("invalid guard config: %w", err)
	}

	g := &Guardian{
		AuthBase:      module.NewAuthBase(ModuleName),
		config:        config,
		clock:         new(clock.OffsetClock),
		confirmations: make(map[uint64]*Confirmation),
		seenIDs:       make(map[uint64]time.Time),
		metrics:       new(GuardianMetrics),
		rateLimiter:   rate.NewLimiter(rate.Every(config.RateLimit), 1),
		pollingState:  PollingStopped,
		fetchGroup:    new(batto.Group[string, []*Confirmation]),
	}

	if config.SharedSecret != "" {
		if secretBytes, err := decodeSecret(config.SharedSecret); err == nil && len(secretBytes) == 20 {
			copy(g.sharedSecretBytes[:], secretBytes)
		}
	}

	if config.IdentitySecret != "" {
		if secretBytes, err := decodeSecret(config.IdentitySecret); err == nil && len(secretBytes) == 20 {
			copy(g.identitySecretBytes[:], secretBytes)
		}
	}

	return g, nil
}

// Init configures module dependencies.
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

// StartAuthed initializes mobile confirmation services and synchronizes time offsets.
func (g *Guardian) StartAuthed(ctx context.Context, auth module.AuthContext) error {
	if err := g.AuthBase.StartAuthed(ctx, auth); err != nil {
		return err
	}

	if g.Community() == nil {
		return ErrCommunityRequired
	}

	g.mu.Lock()
	g.service = NewMobileConf(g.Community())
	g.mu.Unlock()

	g.synchronizeTimeOffset(ctx)
	g.logGuardStatus(ctx, auth)

	return nil
}

// SetConfig updates configuration parameters.
func (g *Guardian) SetConfig(config Config) {
	g.mu.Lock()
	defer g.mu.Unlock()

	g.config = config
}

// Config returns current configuration options.
func (g *Guardian) Config() Config {
	g.mu.RLock()
	defer g.mu.RUnlock()

	return g.config
}

// Service returns the underlying mobile confirmation service.
func (g *Guardian) Service() ConfService { return g.service }

// Metrics returns operational metrics counters.
func (g *Guardian) Metrics() *GuardianMetrics { return g.metrics }

// PollingState returns active polling status.
func (g *Guardian) PollingState() PollingState {
	g.mu.RLock()
	defer mStateRUnlock(g)

	return g.pollingState
}

func mStateRUnlock(g *Guardian) { g.mu.RUnlock() }

// GenerateAuthCode generates a 5-character Steam Guard TOTP code for the current time.
func (g *Guardian) GenerateAuthCode() generic.Optional[[5]byte] {
	if g == nil || g.config.SharedSecret == "" {
		return generic.None[[5]byte]()
	}

	return generic.Some(
		crypto.GenerateAuthCode(bytesconv.S2B(g.config.SharedSecret), g.clock.Now().Unix()),
	)
}

// FetchConfirmations fetches pending mobile confirmations from Steam.
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

// AcceptMultiple approves multiple confirmations in a single request.
func (g *Guardian) AcceptMultiple(ctx context.Context, confirmations []*Confirmation) error {
	return g.respond(ctx, confirmations, true)
}

// Cancel rejects a single confirmation.
func (g *Guardian) Cancel(ctx context.Context, confirmation *Confirmation) error {
	return g.respond(ctx, []*Confirmation{confirmation}, false)
}

// CancelMultiple rejects multiple confirmations in a single request.
func (g *Guardian) CancelMultiple(ctx context.Context, confirmations []*Confirmation) error {
	return g.respond(ctx, confirmations, false)
}

// Close shuts down the module.
func (g *Guardian) Close() error {
	_ = g.Fsm.Transition(context.Background(), module.EventClose)

	return g.Base.Close()
}

func (g *Guardian) synchronizeTimeOffset(ctx context.Context) {
	if g.twoFactorSvc == nil {
		return
	}

	offsetFuture := generic.NewFutureFunc(func() (time.Duration, error) {
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

	statusFuture := generic.NewFutureFunc(func() (*pb.CTwoFactor_Status_Response, error) {
		svc, err := g.twoFactorSvc.Get()
		if err != nil || svc == nil {
			return nil, err
		}

		return svc.QueryStatus(ctx, auth.SteamID())
	})

	if status, err := statusFuture.Get(ctx); err == nil && status != nil {
		g.Logger.Info("Steam Guard Status loaded", log.String("device_id", status.GetDeviceIdentifier()))
	}
}

func (g *Guardian) executeFetch(ctx context.Context) ([]*Confirmation, error) {
	if err := g.rateLimiter.Wait(ctx); err != nil {
		return nil, err
	}

	secretBytes, err := decodeSecret(g.config.IdentitySecret)
	if err != nil {
		g.metrics.TotalErrors.Add(1)

		return nil, fmt.Errorf("key generation failed: %w", err)
	}

	timestamp := g.clock.Now().Unix()
	key := crypto.GenerateConfirmationKey(secretBytes, timestamp, "conf")

	resp, err := g.service.GetConfirmations(ctx, g.config.DeviceID, g.SteamID(), bytesconv.B2S(key[:]), timestamp)
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

	secretBytes, err := decodeSecret(g.config.IdentitySecret)
	if err != nil {
		g.metrics.TotalErrors.Add(1)

		return fmt.Errorf("key generation failed: %w", err)
	}

	timestamp := g.clock.Now().Unix()
	tag := generic.Ternary(accept, "accept", "reject")
	key := crypto.GenerateConfirmationKey(secretBytes, timestamp, tag)

	if err := g.executeResponse(ctx, confirmations, accept, key, timestamp); err != nil {
		g.metrics.TotalErrors.Add(1)

		return err
	}

	g.updateMetrics(len(confirmations), accept)

	return nil
}

func (g *Guardian) executeResponse(
	ctx context.Context,
	confirmations []*Confirmation,
	accept bool,
	key [28]byte,
	timestamp int64,
) error {
	if len(confirmations) == 1 {
		return g.service.RespondToConfirmation(
			ctx,
			confirmations[0],
			accept,
			g.config.DeviceID,
			g.SteamID(),
			bytesconv.B2S(key[:]),
			timestamp,
		)
	}

	return g.service.RespondToMultiple(
		ctx,
		confirmations,
		accept,
		g.config.DeviceID,
		g.SteamID(),
		bytesconv.B2S(key[:]),
		timestamp,
	)
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

// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package auth provides Steam authentication flows, supporting both
// the new WebAPI JWT-based login and legacy Connection Manager handshakes.
//
// Note: it's not a perfect module because the steam client relies on it.
// So it cannot be loaded the normal way.
package auth

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/lemon4ksan/g-man/pkg/log"
	"github.com/lemon4ksan/g-man/pkg/steam/bus"
	"github.com/lemon4ksan/g-man/pkg/steam/protocol"
	pb "github.com/lemon4ksan/g-man/pkg/steam/protocol/protobuf"
	"github.com/lemon4ksan/g-man/pkg/steam/socket"

	"google.golang.org/protobuf/proto"
)

const ProtocolVersion = 65580

// Config defines the configuration for the Authenticator.
type Config struct {
	ApiTimeout  time.Duration
	LogonID     uint32
	AutoRelogin bool
	MaxRetries  int
}

func DefaultConfig() Config {
	return Config{
		ApiTimeout:  30 * time.Second,
		LogonID:     uint32(time.Now().Unix()),
		AutoRelogin: true,
		MaxRetries:  3,
	}
}

// State represents the current authentication state.
type State int32

const (
	StateDisconnected   State = iota
	StateAuthenticating       // Combines Encrypting and WebAPI fetching
	StateLoggingOn            // Exchanging token with CM
	StateLoggedOn
	StateFailed
)

func (s State) String() string {
	switch s {
	case StateDisconnected:
		return "disconnected"
	case StateAuthenticating:
		return "authenticating"
	case StateLoggingOn:
		return "logging_on"
	case StateLoggedOn:
		return "logged_on"
	case StateFailed:
		return "failed"
	default:
		return "unknown"
	}
}

// SocketProvider defines the minimal socket capabilities required by the Authenticator.
// This ensures Dependency Inversion (SOLID).
type SocketProvider interface {
	RegisterMsgHandler(eMsg protocol.EMsg, handler socket.Handler)
	Connect(ctx context.Context, server socket.CMServer) error
	SendProto(ctx context.Context, eMsg protocol.EMsg, req proto.Message) error
	SendRaw(ctx context.Context, eMsg protocol.EMsg, payload []byte) error
	Session() socket.Session
	StartHeartbeat(time.Duration)
	Bus() *bus.Bus
}

// WebAuthenticator defines the interface for performing WebAPI-based authentication flows.
// It decouples the Authenticator from the concrete implementation of the service.
type WebAuthenticator interface {
	BeginAuthSessionViaCredentials(ctx context.Context, accountName, password string, authCode string) (*pb.CAuthentication_BeginAuthSessionViaCredentials_Response, error)
	PollAuthSessionStatus(ctx context.Context, clientID uint64, requestID []byte) (*pb.CAuthentication_PollAuthSessionStatus_Response, error)
	UpdateAuthSessionWithSteamGuardCode(ctx context.Context, clientID uint64, steamID uint64, code string, codeType pb.EAuthSessionGuardType) error
}

// Option defines a functional option for Authenticator.
type Option func(*Authenticator)

func WithLogger(l log.Logger) Option {
	return func(a *Authenticator) { a.logger = l }
}

// Authenticator handles the complex multi-step Steam authentication process.
type Authenticator struct {
	config Config
	state  atomic.Int32

	socket  SocketProvider
	service WebAuthenticator
	logger  log.Logger

	// Protects details from concurrent access during relogins
	mu      sync.RWMutex
	details *LogOnDetails
	tempKey []byte

	// Active login coordination
	loginCtx    context.Context
	loginCancel context.CancelCauseFunc
}

func NewAuthenticator(s SocketProvider, service WebAuthenticator, cfg Config, opts ...Option) *Authenticator {
	auth := &Authenticator{
		config:  cfg,
		socket:  s,
		service: service,
		logger:  log.Discard,
	}
	for _, opt := range opts {
		opt(auth)
	}

	auth.setState(StateDisconnected)

	// Register CM Handlers
	s.RegisterMsgHandler(protocol.EMsg_ChannelEncryptRequest, auth.handleChannelEncryptRequest)
	s.RegisterMsgHandler(protocol.EMsg_ChannelEncryptResult, auth.handleChannelEncryptResult)
	s.RegisterMsgHandler(protocol.EMsg_ClientLogOnResponse, auth.handleLogOnResponse)
	s.RegisterMsgHandler(protocol.EMsg_ClientLoggedOff, auth.handleLoggedOff)

	return auth
}

func (a *Authenticator) State() State              { return State(a.state.Load()) }
func (a *Authenticator) Service() WebAuthenticator { return a.service }

// LogOn begins the authentication sequence.
// It blocks until authentication completes successfully or fails.
func (a *Authenticator) LogOn(ctx context.Context, details *LogOnDetails, server socket.CMServer) error {
	// Thread-safe state transition (Prevents concurrent LogOn calls)
	if !a.state.CompareAndSwap(int32(StateDisconnected), int32(StateAuthenticating)) {
		if a.State() == StateFailed {
			a.state.Store(int32(StateAuthenticating)) // Allow retry if failed
		} else {
			return errors.New("authentication already in progress or completed")
		}
	}

	loginCtx, cancel := context.WithCancelCause(ctx)

	a.mu.Lock()
	a.loginCtx = loginCtx
	a.loginCancel = cancel
	a.details = details
	a.mu.Unlock()

	defer func() {
		if a.State() != StateLoggedOn {
			a.setState(StateFailed)
		}
	}()

	if err := a.validate(details); err != nil {
		return err
	}

	// WebAPI Flow: Obtain Refresh Token if not provided
	if details.RefreshToken == "" {
		token, steamID, err := a.performPasswordAuth(loginCtx, details)
		if err != nil {
			return fmt.Errorf("webapi auth failed: %w", err)
		}

		a.mu.Lock()
		a.details.RefreshToken = token
		a.details.SteamID = steamID
		a.mu.Unlock()

		a.logger.Info("Password authentication successful, obtained refresh token")
	}

	// CM Socket Flow: Connect and Authenticate
	a.setState(StateLoggingOn)
	if err := a.socket.Connect(loginCtx, server); err != nil {
		return fmt.Errorf("cm connection failed: %w", err)
	}

	a.socket.Session().SetSteamID(details.SteamID)

	// WebSockets don't need ChannelEncryptRequest, we send LogOn directly.
	if server.Type == "websockets" {
		a.logger.Debug("WebSocket detected, starting logon sequence immediately")
		a.sendLogOn(loginCtx, details, details.RefreshToken)
	}

	// Wait for CM Response (ClientLogOnResponse)
	<-loginCtx.Done()
	err := context.Cause(loginCtx)
	if err == nil || errors.Is(err, context.Canceled) {
		a.setState(StateLoggedOn)
		return nil
	}
	return err
}

func (a *Authenticator) LogOnAnonymous(ctx context.Context, server socket.CMServer) error {
	if !a.state.CompareAndSwap(int32(StateDisconnected), int32(StateAuthenticating)) {
		if a.State() == StateFailed {
			a.state.Store(int32(StateAuthenticating)) // Allow retry if failed
		} else {
			return errors.New("authentication already in progress or completed")
		}
	}
	a.setState(StateLoggingOn)

	loginCtx, cancel := context.WithCancelCause(ctx)

	a.mu.Lock()
	a.loginCtx = loginCtx
	a.loginCancel = cancel
	a.details = &LogOnDetails{
		ProtocolVersion: ProtocolVersion,
		ClientOSType:    uint32(protocol.EOSType_Windows10),
	}
	a.mu.Unlock()

	defer func() {
		if a.State() != StateLoggedOn {
			a.setState(StateFailed)
		}
	}()

	if err := a.socket.Connect(ctx, server); err != nil {
		return fmt.Errorf("cm connection failed: %w", err)
	}

	<-loginCtx.Done()
	err := context.Cause(loginCtx)
	if errors.Is(err, context.Canceled) {
		return ctx.Err()
	}
	return err
}

func (a *Authenticator) validate(details *LogOnDetails) error {
	if details == nil {
		return errors.New("nil details")
	}

	if details.ClientOSType == 0 {
		details.ClientOSType = uint32(protocol.EOSType_Windows10)
	}
	if details.ProtocolVersion == 0 {
		details.ProtocolVersion = ProtocolVersion
	}
	if details.ClientLanguage == "" {
		details.ClientLanguage = "english"
	}

	if details.RefreshToken == "" && details.AccountName == "" {
		return errors.New("either refresh token or account name required")
	}
	if details.RefreshToken == "" && details.Password == "" {
		return errors.New("password required when using account name")
	}
	return nil
}

func (a *Authenticator) performPasswordAuth(ctx context.Context, details *LogOnDetails) (string, uint64, error) {
	a.logger.Info("Starting password authentication via WebAPI")

	resp, err := a.service.BeginAuthSessionViaCredentials(ctx, details.AccountName, details.Password, details.AuthCode)
	if err != nil {
		return "", 0, fmt.Errorf("begin session: %w", err)
	}

	confirmations := resp.GetAllowedConfirmations()
	if len(confirmations) > 0 {
		for _, conf := range confirmations {
			a.resolveConfirmation(ctx, conf, resp)
		}
	}

	interval := time.Duration(resp.GetInterval()) * time.Second
	if interval <= 0 {
		interval = 2 * time.Second
	}

	return a.pollLoop(ctx, resp.GetClientId(), resp.GetRequestId(), resp.GetSteamid(), interval)
}

func (a *Authenticator) resolveConfirmation(ctx context.Context, conf *pb.CAuthentication_AllowedConfirmation, resp *pb.CAuthentication_BeginAuthSessionViaCredentials_Response) {
	confType := conf.GetConfirmationType()
	is2FA := confType == pb.EAuthSessionGuardType_k_EAuthSessionGuardType_DeviceCode

	switch confType {
	case pb.EAuthSessionGuardType_k_EAuthSessionGuardType_EmailCode, pb.EAuthSessionGuardType_k_EAuthSessionGuardType_DeviceCode:
		msg := "2FA code required"
		if !is2FA {
			msg = "Email confirmation required"
		}

		a.logger.Info(msg, log.String("associated_message", conf.GetAssociatedMessage()))

		a.socket.Bus().Publish(&SteamGuardRequiredEvent{
			Is2FA:       is2FA,
			EmailDomain: conf.GetAssociatedMessage(),
			Callback: func(code string) {
				if code == "" {
					return
				}
				go func() {
					err := a.service.UpdateAuthSessionWithSteamGuardCode(ctx, resp.GetClientId(), resp.GetSteamid(), code, confType)
					if err != nil {
						a.logger.Error("Failed to submit guard code", log.Err(err))
						a.failLogin(fmt.Errorf("steam guard rejected: %w", err))
					}
				}()
			},
		})

	case pb.EAuthSessionGuardType_k_EAuthSessionGuardType_DeviceConfirmation:
		a.logger.Info("Mobile app confirmation required (Accept prompt on phone)")
		a.socket.Bus().Publish(&SteamGuardRequiredEvent{Is2FA: true})
	}
}

func (a *Authenticator) pollLoop(ctx context.Context, clientID uint64, requestID []byte, steamID uint64, interval time.Duration) (string, uint64, error) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			// Either parent timeout, OR user submitted wrong 2FA code and triggered failLogin()
			return "", 0, context.Cause(ctx)

		case <-ticker.C:
			pollRes, err := a.service.PollAuthSessionStatus(ctx, clientID, requestID)
			if err != nil {
				if !strings.Contains(err.Error(), "DuplicateRequest") {
					a.logger.Debug("Poll status error", log.Err(err))
				}
				continue
			}

			if token := pollRes.GetRefreshToken(); token != "" {
				return token, steamID, nil
			}
		}
	}
}

func (a *Authenticator) setState(state State) {
	old := State(a.state.Swap(int32(state)))
	if old != state {
		a.socket.Bus().Publish(&StateEvent{Old: old, New: state})
	}
}

func (a *Authenticator) succeedLogin() {
	a.mu.RLock()
	cancel := a.loginCancel
	a.mu.RUnlock()

	if cancel != nil {
		cancel(nil)
	}
}

func (a *Authenticator) failLogin(err error) {
	a.mu.RLock()
	cancel := a.loginCancel
	a.mu.RUnlock()

	if cancel != nil {
		cancel(err)
	}
}

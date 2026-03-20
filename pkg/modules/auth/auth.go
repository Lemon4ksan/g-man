// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package auth

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"time"

	"github.com/lemon4ksan/g-man/pkg/log"
	"github.com/lemon4ksan/g-man/pkg/steam/bus"
	pb "github.com/lemon4ksan/g-man/pkg/steam/protobuf"
	"github.com/lemon4ksan/g-man/pkg/steam/protocol"
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
	StateAuthenticating       // Fetching WebAPI tokens
	StateLoggingOn            // Exchanging token with CM via Socket
	StateLoggedOn             // Authentication complete
	StateFailed               // Terminal failure state
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
type SocketProvider interface {
	RegisterMsgHandler(eMsg protocol.EMsg, handler socket.Handler)
	Connect(ctx context.Context, server socket.CMServer) error
	SendProto(ctx context.Context, eMsg protocol.EMsg, req proto.Message, opts ...socket.SendOption) error
	SendRaw(ctx context.Context, eMsg protocol.EMsg, payload []byte, opts ...socket.SendOption) error
	Session() socket.Session
	StartHeartbeat(time.Duration)
	Bus() *bus.Bus
}

// WebAuthenticator defines the interface for WebAPI-based authentication flows.
type WebAuthenticator interface {
	BeginAuthSessionViaCredentials(ctx context.Context, accountName, password string, authCode string) (*pb.CAuthentication_BeginAuthSessionViaCredentials_Response, error)
	PollAuthSessionStatus(ctx context.Context, clientID uint64, requestID []byte) (*pb.CAuthentication_PollAuthSessionStatus_Response, error)
	UpdateAuthSessionWithSteamGuardCode(ctx context.Context, clientID, steamID uint64, code string, codeType pb.EAuthSessionGuardType) error
}

// Option defines a functional option for Authenticator.
type Option func(*Authenticator)

func WithLogger(l log.Logger) Option {
	return func(a *Authenticator) { a.logger = l.WithModule("auth") }
}

// Authenticator handles the complex multi-step Steam authentication process.
// It manages the transition from HTTP JWT fetch to CM Socket Logon.
type Authenticator struct {
	config Config
	state  atomic.Int32

	socket  SocketProvider
	service WebAuthenticator
	logger  log.Logger

	// Atomic pointers to avoid locking during frequent reads.
	activeDetails atomic.Pointer[LogOnDetails]
	tempKey       atomic.Pointer[[]byte]

	// Active login coordination
	loginCtx    atomic.Pointer[context.Context]
	loginCancel atomic.Pointer[context.CancelCauseFunc]
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

// LogOn begins the authentication sequence. It blocks until completion.
func (a *Authenticator) LogOn(ctx context.Context, details *LogOnDetails, server socket.CMServer) error {
	if !a.tryAcquireState() {
		return errors.New("auth: authentication already in progress")
	}
	defer a.ensureTerminalState()

	if err := a.validate(details); err != nil {
		return err
	}

	// Setup coordination context
	loginCtx, cancel := context.WithCancelCause(ctx)
	a.loginCtx.Store(&loginCtx)
	a.loginCancel.Store(&cancel)
	a.activeDetails.Store(details)

	if details.RefreshToken == "" {
		token, steamID, err := a.performPasswordAuth(loginCtx, details)
		if err != nil {
			return fmt.Errorf("webapi auth failed: %w", err)
		}

		// Update details with new credentials
		details.RefreshToken = token
		details.SteamID = steamID
		a.activeDetails.Store(details)
		a.logger.Info("Password authentication successful, obtained refresh token")
	}

	a.setState(StateLoggingOn)
	if err := a.socket.Connect(loginCtx, server); err != nil {
		return fmt.Errorf("cm connection failed: %w", err)
	}

	sess := a.socket.Session()
	if sess == nil {
		return errors.New("cm socket returned nil session")
	}
	sess.SetSteamID(details.SteamID)
	sess.SetAccessToken(details.RefreshToken)

	if server.Type == "websockets" {
		a.logger.Debug("WebSocket detected, starting logon sequence immediately")
		a.sendLogOn(loginCtx, details, details.RefreshToken)
	}

	<-loginCtx.Done()
	err := context.Cause(loginCtx)

	if err == nil || errors.Is(err, context.Canceled) {
		a.setState(StateLoggedOn)
		return nil
	}

	a.setState(StateFailed)
	return err
}

// LogOnAnonymous performs a login without user credentials.
func (a *Authenticator) LogOnAnonymous(ctx context.Context, server socket.CMServer) error {
	if !a.tryAcquireState() {
		return errors.New("auth: authentication already in progress")
	}
	defer a.ensureTerminalState()

	a.setState(StateLoggingOn)

	loginCtx, cancel := context.WithCancelCause(ctx)
	a.loginCtx.Store(&loginCtx)
	a.loginCancel.Store(&cancel)

	anonDetails := &LogOnDetails{
		ProtocolVersion: ProtocolVersion,
		ClientOSType:    uint32(protocol.EOSType_Windows10),
	}
	a.activeDetails.Store(anonDetails)

	if err := a.socket.Connect(ctx, server); err != nil {
		return fmt.Errorf("cm connection failed: %w", err)
	}

	<-loginCtx.Done()
	err := context.Cause(loginCtx)

	if err == nil || errors.Is(err, context.Canceled) {
		a.setState(StateLoggedOn)
		return nil
	}

	a.setState(StateFailed)
	return err
}

func (a *Authenticator) tryAcquireState() bool {
	for {
		current := a.state.Load()
		if current == int32(StateAuthenticating) || current == int32(StateLoggingOn) || current == int32(StateLoggedOn) {
			return false
		}
		if a.state.CompareAndSwap(current, int32(StateAuthenticating)) {
			return true
		}
	}
}

func (a *Authenticator) ensureTerminalState() {
	if a.State() != StateLoggedOn {
		a.setState(StateFailed)
	}
}

func (a *Authenticator) validate(details *LogOnDetails) error {
	if details == nil {
		return errors.New("auth: nil details provided")
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
		return errors.New("auth: either refresh token or account name required")
	}
	if details.RefreshToken == "" && details.Password == "" {
		return errors.New("auth: password required when using account name")
	}
	return nil
}

func (a *Authenticator) performPasswordAuth(ctx context.Context, details *LogOnDetails) (string, uint64, error) {
	a.logger.Info("Starting password authentication via WebAPI")

	resp, err := a.service.BeginAuthSessionViaCredentials(ctx, details.AccountName, details.Password, details.AuthCode)
	if err != nil {
		return "", 0, fmt.Errorf("begin session failed: %w", err)
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

	return a.pollAuthStatus(ctx, resp.GetClientId(), resp.GetRequestId(), resp.GetSteamid(), interval)
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

func (a *Authenticator) pollAuthStatus(ctx context.Context, clientID uint64, requestID []byte, steamID uint64, interval time.Duration) (string, uint64, error) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return "", 0, context.Cause(ctx)

		case <-ticker.C:
			pollRes, err := a.service.PollAuthSessionStatus(ctx, clientID, requestID)
			if err != nil {
				// Ignore duplicate requests/timeouts to keep polling
				if !strings.Contains(err.Error(), "DuplicateRequest") {
					a.logger.Debug("Poll status warning", log.Err(err))
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
	if cancelPtr := a.loginCancel.Load(); cancelPtr != nil {
		(*cancelPtr)(nil)
	}
}

func (a *Authenticator) failLogin(err error) {
	if cancelPtr := a.loginCancel.Load(); cancelPtr != nil {
		(*cancelPtr)(err)
	}
}

// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package auth

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/lemon4ksan/g-man/pkg/bus"
	"github.com/lemon4ksan/g-man/pkg/log"
	pb "github.com/lemon4ksan/g-man/pkg/protobuf/steam"
	"github.com/lemon4ksan/g-man/pkg/steam/api"
	"github.com/lemon4ksan/g-man/pkg/steam/id"
	"github.com/lemon4ksan/g-man/pkg/steam/socket"
	"github.com/lemon4ksan/g-man/pkg/steam/socket/protocol"
	"github.com/lemon4ksan/g-man/pkg/storage"
	"github.com/lemon4ksan/g-man/pkg/storage/memory"
	"google.golang.org/protobuf/proto"
)

const ProtocolVersion = 65580

// Config defines the configuration for the Authenticator.
type Config struct {
	LogonID      uint32
	LogonTimeout time.Duration
}

func DefaultConfig() Config {
	return Config{
		LogonID:      uint32(time.Now().Unix()),
		LogonTimeout: 30 * time.Second,
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
	GenerateAccessTokenForApp(ctx context.Context, refreshToken string, steamID uint64) (*pb.CAuthentication_AccessToken_GenerateForApp_Response, error)
}

// Option defines a functional option for Authenticator.
type Option func(*Authenticator)

func WithLogger(l log.Logger) Option {
	return func(a *Authenticator) { a.logger = l.With(log.Module("auth")) }
}

func WithStorage(store storage.AuthStore) Option {
	return func(a *Authenticator) { a.store = store }
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
	loginCtx    atomic.Value
	loginCancel atomic.Value
	loginResult chan error
	store       storage.AuthStore
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
	if auth.store == nil {
		auth.store = memory.New().AuthStore()
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

// LogOn initiates the login sequence.
// It is a blocking call that coordinates:
//  1. RSA Password encryption (if no token provided).
//  2. WebAPI authentication session start.
//  3. Steam Guard challenge handling (via event bus).
//  4. TCP Channel encryption handshake with the CM.
//  5. Final EMsg_ClientLogon exchange.
//
// Returns nil on success (StateLoggedOn) or a terminal error.
func (a *Authenticator) LogOn(ctx context.Context, details *LogOnDetails, server socket.CMServer) error {
	if !a.tryAcquireState() {
		return errors.New("auth: authentication already in progress")
	}
	defer a.ensureTerminalState()

	if err := a.validate(details); err != nil {
		return err
	}

	a.loginResult = make(chan error, 1)

	if len(details.MachineID) == 0 {
		if savedMachineID, err := a.store.GetMachineID(ctx, details.AccountName); err == nil {
			a.logger.Debug("Found saved MachineID in storage")
			details.MachineID = savedMachineID
		} else {
			a.logger.Info("Generating new MachineID for account (First run on this device)")
			details.MachineID = generateMachineID()
			_ = a.store.SaveMachineID(ctx, details.AccountName, details.MachineID)
		}
	}

	loginCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	a.loginCtx.Store(loginCtx)
	a.loginCancel.Store(cancel)
	a.activeDetails.Store(details)

	if details.RefreshToken == "" {
		if token, err := a.store.GetRefreshToken(ctx, details.AccountName); err == nil && token != "" {
			a.logger.Info("Found saved refresh token in storage")
			details.RefreshToken = token
		}
	}

	if details.RefreshToken != "" && details.SteamID == 0 {
		details.SteamID = ExtractSteamIDFromJWT(details.RefreshToken)
		if details.SteamID != 0 {
			a.logger.Debug("Extracted SteamID from saved token", log.SteamID(details.SteamID.Uint64()))
		}
	}

	if details.RefreshToken == "" {
		a.logger.Info("No saved token, performing password authentication via WebAPI")
		refresh, access, steamID, err := a.performPasswordAuth(loginCtx, details)
		if err != nil {
			return err
		}
		details.RefreshToken = refresh
		details.AccessToken = access
		details.SteamID = id.ID(steamID)
		_ = a.store.SaveRefreshToken(ctx, details.AccountName, refresh)
	}

	a.setState(StateLoggingOn)
	if err := a.socket.Connect(loginCtx, server); err != nil {
		return fmt.Errorf("cm connection failed: %w", err)
	}

	sess := a.socket.Session()
	if sess == nil {
		return errors.New("cm socket returned nil session")
	}
	sess.SetSteamID(details.SteamID.Uint64())
	sess.SetRefreshToken(details.RefreshToken)

	if server.Type == "websockets" {
		a.logger.Debug("WebSocket detected, starting logon sequence immediately")
		a.sendLogOn(loginCtx, details)
	}

	var resultErr error
	select {
	case resultErr = <-a.loginResult:
		// Received response from CM (success or failure)
	case <-loginCtx.Done():
		resultErr = loginCtx.Err()
	}

	if resultErr == nil {
		a.setState(StateLoggedOn)
		return nil
	}

	var eResErr api.EResultError
	if errors.As(resultErr, &eResErr) && eResErr.EResult == protocol.EResult_InvalidPassword {
		a.logger.Warn("Session rejected by CM (Invalid Password/Token), clearing local storage")
		_ = a.store.Clear(ctx, details.AccountName)
	}

	a.setState(StateFailed)
	return resultErr
}

// LogOnAnonymous performs a login without user credentials.
func (a *Authenticator) LogOnAnonymous(ctx context.Context, server socket.CMServer) error {
	if !a.tryAcquireState() {
		return errors.New("auth: authentication already in progress")
	}
	defer a.ensureTerminalState()

	a.setState(StateLoggingOn)

	loginCtx, cancel := context.WithCancelCause(ctx)
	a.loginCtx.Store(loginCtx)
	a.loginCancel.Store(cancel)

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
		return errors.New("auth: account name or refresh token is required")
	}

	if details.RefreshToken == "" && details.Password == "" {
		return errors.New("auth: password is required when refresh token is missing")
	}

	return nil
}

func (a *Authenticator) performPasswordAuth(ctx context.Context, details *LogOnDetails) (string, string, uint64, error) {
	resp, err := a.service.BeginAuthSessionViaCredentials(ctx, details.AccountName, details.Password, details.AuthCode)
	if err != nil {
		return "", "", 0, fmt.Errorf("begin session failed: %w", err)
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
		a.socket.Bus().Publish(&SteamGuardRequiredEvent{IsAppConfirm: true})
	}
}

func (a *Authenticator) pollAuthStatus(ctx context.Context, clientID uint64, requestID []byte, steamID uint64, interval time.Duration) (string, string, uint64, error) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return "", "", 0, context.Cause(ctx)

		case <-ticker.C:
			pollRes, err := a.service.PollAuthSessionStatus(ctx, clientID, requestID)
			if err != nil {
				if !strings.Contains(err.Error(), "DuplicateRequest") {
					a.logger.Debug("Poll status warning", log.Err(err))
				}
				continue
			}

			if refresh := pollRes.GetRefreshToken(); refresh != "" {
				return refresh, pollRes.GetAccessToken(), steamID, nil
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
	select {
	case a.loginResult <- nil:
	default:
	}
}

func (a *Authenticator) failLogin(err error) {
	if cancelFunc, ok := a.loginCancel.Load().(context.CancelFunc); ok {
		cancelFunc()
	}
	select {
	case a.loginResult <- err:
	default:
	}
}

func ExtractSteamIDFromJWT(token string) id.ID {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return 0
	}

	payloadStr := parts[1]
	if pad := len(payloadStr) % 4; pad != 0 {
		payloadStr += strings.Repeat("=", 4-pad)
	}

	payload, err := base64.URLEncoding.DecodeString(payloadStr)
	if err != nil {
		payload, err = base64.RawURLEncoding.DecodeString(parts[1])
		if err != nil {
			return 0
		}
	}

	var claims struct {
		Sub string `json:"sub"`
	}
	if err := json.Unmarshal(payload, &claims); err != nil {
		return 0
	}

	steamID, _ := strconv.ParseUint(claims.Sub, 10, 64)
	return id.ID(steamID)
}

func generateMachineID() []byte {
	var b [42]byte
	_, _ = rand.Read(b[:])
	return b[:]
}

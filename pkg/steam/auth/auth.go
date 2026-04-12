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

	"google.golang.org/protobuf/proto"

	"github.com/lemon4ksan/g-man/pkg/bus"
	"github.com/lemon4ksan/g-man/pkg/log"
	pb "github.com/lemon4ksan/g-man/pkg/protobuf/steam"
	"github.com/lemon4ksan/g-man/pkg/steam/api"
	"github.com/lemon4ksan/g-man/pkg/steam/id"
	"github.com/lemon4ksan/g-man/pkg/steam/protocol/enums"
	"github.com/lemon4ksan/g-man/pkg/steam/socket"
)

// ProtocolVersion is the current version of the Steam client protocol used for logon.
const ProtocolVersion = 65580

// Config defines the configuration for the Authenticator.
type Config struct {
	LogonID      uint32
	LogonTimeout time.Duration
}

// DefaultConfig returns the recommended baseline configuration for the Authenticator.
func DefaultConfig() Config {
	return Config{
		LogonID:      uint32(time.Now().Unix()),
		LogonTimeout: 30 * time.Second,
	}
}

// State represents the current lifecycle stage of the authentication process.
type State int32

const (
	// StateDisconnected indicates the authenticator is idle.
	StateDisconnected State = iota
	// StateAuthenticating indicates WebAPI tokens are being fetched.
	StateAuthenticating
	// StateLoggingOn indicates the token is being exchanged with the CM via Socket.
	StateLoggingOn
	// StateLoggedOn indicates the authentication is fully complete.
	StateLoggedOn
	// StateFailed indicates a terminal failure in the logon process.
	StateFailed
)

// String returns a human-readable representation of the authentication state.
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
	RegisterMsgHandler(eMsg enums.EMsg, handler socket.Handler)
	Connect(ctx context.Context, server socket.CMServer) error
	SendProto(ctx context.Context, eMsg enums.EMsg, req proto.Message, opts ...socket.SendOption) error
	SendRaw(ctx context.Context, eMsg enums.EMsg, payload []byte, opts ...socket.SendOption) error
	Session() socket.Session
	StartHeartbeat(time.Duration)
	Bus() *bus.Bus
}

// WebAuthenticator defines the interface for WebAPI-based authentication flows.
type WebAuthenticator interface {
	BeginAuthSessionViaCredentials(
		ctx context.Context,
		accountName, password, authCode string,
	) (*pb.CAuthentication_BeginAuthSessionViaCredentials_Response, error)
	PollAuthSessionStatus(
		ctx context.Context,
		clientID uint64,
		requestID []byte,
	) (*pb.CAuthentication_PollAuthSessionStatus_Response, error)
	UpdateAuthSessionWithSteamGuardCode(
		ctx context.Context,
		clientID, steamID uint64,
		code string,
		codeType pb.EAuthSessionGuardType,
	) error
	GenerateAccessTokenForApp(
		ctx context.Context,
		refreshToken string,
		steamID uint64,
	) (*pb.CAuthentication_AccessToken_GenerateForApp_Response, error)
}

// Store handles persisting the Steam authentication state, such as tokens and machine IDs.
type Store interface {
	SaveRefreshToken(ctx context.Context, accountName, token string) error
	GetRefreshToken(ctx context.Context, accountName string) (string, error)
	SaveMachineID(ctx context.Context, accountName string, machineID []byte) error
	GetMachineID(ctx context.Context, accountName string) ([]byte, error)
	Clear(ctx context.Context, accountName string) error
}

// Option defines a functional configuration option for Authenticator.
type Option func(*Authenticator)

// WithLogger sets a custom logger for the Authenticator.
func WithLogger(l log.Logger) Option {
	return func(a *Authenticator) { a.logger = l.With(log.Module("auth")) }
}

// WithStorage sets a persistent storage provider for authentication data.
func WithStorage(store Store) Option {
	return func(a *Authenticator) { a.store = store }
}

// ExtractSteamIDFromJWT parses a Steam JWT and returns the embedded SteamID.
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

// Authenticator orchestrates the process of logging into Steam.
type Authenticator struct {
	config Config
	state  atomic.Int32

	socket  SocketProvider
	service WebAuthenticator
	logger  log.Logger

	activeDetails atomic.Pointer[LogOnDetails]
	tempKey       atomic.Pointer[[]byte]

	loginCtx    atomic.Value
	loginCancel atomic.Value
	loginResult chan error
	store       Store
}

// NewAuthenticator creates a new instance of Authenticator.
func NewAuthenticator(s SocketProvider, service WebAuthenticator, cfg Config, opts ...Option) *Authenticator {
	auth := &Authenticator{
		config:  cfg,
		socket:  s,
		service: service,
		logger:  log.Discard,
		store:   nopStore{},
	}
	for _, opt := range opts {
		opt(auth)
	}

	auth.setState(StateDisconnected)

	s.RegisterMsgHandler(enums.EMsg_ChannelEncryptRequest, auth.handleChannelEncryptRequest)
	s.RegisterMsgHandler(enums.EMsg_ChannelEncryptResult, auth.handleChannelEncryptResult)
	s.RegisterMsgHandler(enums.EMsg_ClientLogOnResponse, auth.handleLogOnResponse)
	s.RegisterMsgHandler(enums.EMsg_ClientLoggedOff, auth.handleLoggedOff)

	return auth
}

// State returns the current authentication state.
func (a *Authenticator) State() State { return State(a.state.Load()) }

// Service returns the underlying WebAuthenticator.
func (a *Authenticator) Service() WebAuthenticator { return a.service }

// LogOn initiates the login sequence. It blocks until authentication is complete or fails.
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
		savedMachineID, err := a.store.GetMachineID(ctx, details.AccountName)
		if err == nil && len(savedMachineID) > 0 {
			a.logger.Debug("Found saved MachineID in storage")

			details.MachineID = savedMachineID
		} else {
			a.logger.Info("Generating new MachineID for account")

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
	case <-loginCtx.Done():
		resultErr = loginCtx.Err()
	}

	if resultErr == nil {
		a.setState(StateLoggedOn)
		return nil
	}

	var eResErr api.EResultError
	if errors.As(resultErr, &eResErr) && eResErr.EResult == enums.EResult_InvalidPassword {
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

	a.loginResult = make(chan error, 1)

	loginCtx, cancel := context.WithCancel(ctx)
	a.loginCtx.Store(loginCtx)

	a.loginCancel.Store(cancel)
	defer cancel()

	anonDetails := &LogOnDetails{
		ProtocolVersion: ProtocolVersion,
		ClientOSType:    uint32(enums.EOSType_Windows10),
	}
	a.activeDetails.Store(anonDetails)

	a.setState(StateLoggingOn)

	if err := a.socket.Connect(ctx, server); err != nil {
		return fmt.Errorf("cm connection failed: %w", err)
	}

	var resultErr error
	select {
	case resultErr = <-a.loginResult:
	case <-loginCtx.Done():
		resultErr = loginCtx.Err()
	}

	if resultErr != nil {
		a.setState(StateFailed)
		return resultErr
	}

	a.setState(StateLoggedOn)

	return nil
}

func (a *Authenticator) tryAcquireState() bool {
	for {
		current := a.state.Load()
		if current == int32(StateAuthenticating) || current == int32(StateLoggingOn) ||
			current == int32(StateLoggedOn) {
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
		details.ClientOSType = uint32(enums.EOSType_Windows10)
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

func (a *Authenticator) performPasswordAuth(
	ctx context.Context,
	details *LogOnDetails,
) (string, string, uint64, error) {
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

func (a *Authenticator) resolveConfirmation(
	ctx context.Context,
	conf *pb.CAuthentication_AllowedConfirmation,
	resp *pb.CAuthentication_BeginAuthSessionViaCredentials_Response,
) {
	confType := conf.GetConfirmationType()
	is2FA := confType == pb.EAuthSessionGuardType_k_EAuthSessionGuardType_DeviceCode

	switch confType {
	case pb.EAuthSessionGuardType_k_EAuthSessionGuardType_EmailCode,
		pb.EAuthSessionGuardType_k_EAuthSessionGuardType_DeviceCode:
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
					err := a.service.UpdateAuthSessionWithSteamGuardCode(
						ctx,
						resp.GetClientId(),
						resp.GetSteamid(),
						code,
						confType,
					)
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

func (a *Authenticator) pollAuthStatus(
	ctx context.Context,
	clientID uint64,
	requestID []byte,
	steamID uint64,
	interval time.Duration,
) (string, string, uint64, error) {
	// Safety timeout: don't poll forever even if context is long-lived
	timeout := time.NewTimer(5 * time.Minute)
	defer timeout.Stop()

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return "", "", 0, context.Cause(ctx)

		case <-timeout.C:
			return "", "", 0, errors.New("auth: polling session timed out after 5 minutes")

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

func generateMachineID() []byte {
	var b [42]byte
	if _, err := rand.Read(b[:]); err != nil {
		// rand.Read rarely fails, but if it does, we return a zeroed ID.
		return make([]byte, 42)
	}

	return b[:]
}

type nopStore struct{}

func (nopStore) SaveRefreshToken(ctx context.Context, acc, tok string) error     { return nil }
func (nopStore) GetRefreshToken(ctx context.Context, acc string) (string, error) { return "", nil }
func (nopStore) SaveMachineID(ctx context.Context, acc string, id []byte) error  { return nil }
func (nopStore) GetMachineID(ctx context.Context, acc string) ([]byte, error)    { return nil, nil }
func (nopStore) Clear(ctx context.Context, acc string) error                     { return nil }

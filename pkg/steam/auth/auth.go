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
	"sync"
	"sync/atomic"
	"time"

	"github.com/lemon4ksan/miyako/bus"
	"github.com/lemon4ksan/miyako/kata"
	"google.golang.org/protobuf/proto"

	"github.com/lemon4ksan/g-man/pkg/crypto"
	"github.com/lemon4ksan/g-man/pkg/log"
	pb "github.com/lemon4ksan/g-man/pkg/protobuf/steam"
	"github.com/lemon4ksan/g-man/pkg/steam/id"
	"github.com/lemon4ksan/g-man/pkg/steam/protocol/enums"
	"github.com/lemon4ksan/g-man/pkg/steam/service"
	"github.com/lemon4ksan/g-man/pkg/steam/socket"
	"github.com/lemon4ksan/g-man/pkg/steam/socket/connector"
	"github.com/lemon4ksan/g-man/pkg/steam/socket/dispatcher"
	"github.com/lemon4ksan/g-man/pkg/storage"
)

// ProtocolVersion is the current version of the Steam client protocol used for logon.
const ProtocolVersion = 65580

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

// Event represents a trigger that drives a state transition.
type Event int32

const (
	// EventBegin initiates a new login attempt.
	EventBegin Event = iota
	// EventLoggingOn transitions from authenticating to logging on.
	EventLoggingOn
	// EventSuccess completes the login process.
	EventSuccess
	// EventFail indicates a failure during authentication.
	EventFail
	// EventDisconnect returns to disconnected state.
	EventDisconnect
)

// SocketProvider defines the minimal socket capabilities required by the Authenticator.
type SocketProvider interface {
	SetEncryptionKey(key []byte) bool
	RegisterMsgHandler(eMsg enums.EMsg, handler dispatcher.Handler)
	Connect(ctx context.Context, server connector.CMServer) error
	SendProto(ctx context.Context, eMsg enums.EMsg, req proto.Message, opts ...socket.SendOption) error
	SendRaw(ctx context.Context, eMsg enums.EMsg, payload []byte, opts ...socket.SendOption) error
	Session() socket.Session
	StartHeartbeat(time.Duration) error
}

// WebAuthenticator defines the interface for interacting with Steam's mobile confirmation endpoints.
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

// KVStore wraps a KV store to satisfy the Store interface.
type KVStore struct {
	kv storage.KV
}

// NewKVStore creates a new Store backed by a generic KV store.
func NewKVStore(kv storage.KV) Store {
	return &KVStore{kv: kv}
}

// SaveRefreshToken saves the refresh token for the given account.
func (s *KVStore) SaveRefreshToken(ctx context.Context, accountName, token string) error {
	return s.kv.Set(ctx, "refresh_token:"+accountName, []byte(token))
}

// GetRefreshToken retrieves the refresh token for the given account.
func (s *KVStore) GetRefreshToken(ctx context.Context, accountName string) (string, error) {
	tokenBytes, err := s.kv.Get(ctx, "refresh_token:"+accountName)
	if err != nil {
		return "", err
	}

	return string(tokenBytes), nil
}

// SaveMachineID saves the machine ID for the given account.
func (s *KVStore) SaveMachineID(ctx context.Context, accountName string, machineID []byte) error {
	return s.kv.Set(ctx, "machine_id:"+accountName, machineID)
}

// GetMachineID retrieves the machine ID for the given account.
func (s *KVStore) GetMachineID(ctx context.Context, accountName string) ([]byte, error) {
	return s.kv.Get(ctx, "machine_id:"+accountName)
}

// Clear removes all stored credentials for the given account.
func (s *KVStore) Clear(ctx context.Context, accountName string) error {
	return s.kv.Delete(ctx, "refresh_token:"+accountName)
}

// Option defines a functional configuration option for Authenticator.
type Option func(*Authenticator)

// WithLogger sets a custom logger for the Authenticator.
func WithLogger(l log.Logger) Option {
	return func(a *Authenticator) { a.setLogger(l.With(log.Module("auth"))) }
}

// WithStorage sets a persistent storage provider for authentication data.
func WithStorage(store Store) Option {
	return func(a *Authenticator) { a.store = store }
}

// ExtractSteamIDFromJWT parses a Steam JWT and returns the embedded SteamID.
func ExtractSteamIDFromJWT(token string) id.ID {
	payload, err := decodeJWTPayload(token)
	if err != nil {
		return 0
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

func decodeJWTPayload(token string) ([]byte, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return nil, errors.New("auth: invalid jwt segment count")
	}

	payloadStr := parts[1]
	if pad := len(payloadStr) % 4; pad != 0 {
		payloadStr += strings.Repeat("=", 4-pad)
	}

	payload, err := base64.URLEncoding.DecodeString(payloadStr)
	if err == nil {
		return payload, nil
	}

	return base64.RawURLEncoding.DecodeString(parts[1])
}

// Authenticator orchestrates the process of logging into Steam.
type Authenticator struct {
	fsm *kata.FSM[State, Event]

	loggerMu sync.RWMutex
	logger   log.Logger
	bus      *bus.Bus
	socket   SocketProvider
	service  WebAuthenticator

	activeDetails atomic.Pointer[LogOnDetails]
	tempKey       atomic.Pointer[[]byte]

	loginCancel atomic.Value
	loginResult atomic.Value
	store       Store
}

// NewAuthenticator creates a new instance of Authenticator.
func NewAuthenticator(s SocketProvider, svc WebAuthenticator, bus *bus.Bus, opts ...Option) *Authenticator {
	fsm := kata.NewFSM[State, Event](StateDisconnected)
	fsm.AddRules(
		kata.TransitionRule[State, Event]{From: StateDisconnected, Event: EventBegin, To: StateAuthenticating},
		kata.TransitionRule[State, Event]{From: StateFailed, Event: EventBegin, To: StateAuthenticating},
		kata.TransitionRule[State, Event]{From: StateAuthenticating, Event: EventLoggingOn, To: StateLoggingOn},
		kata.TransitionRule[State, Event]{From: StateLoggingOn, Event: EventSuccess, To: StateLoggedOn},
		kata.TransitionRule[State, Event]{From: StateAuthenticating, Event: EventFail, To: StateFailed},
		kata.TransitionRule[State, Event]{From: StateLoggingOn, Event: EventFail, To: StateFailed},
		kata.TransitionRule[State, Event]{From: StateLoggedOn, Event: EventDisconnect, To: StateDisconnected},
		kata.TransitionRule[State, Event]{From: StateFailed, Event: EventDisconnect, To: StateDisconnected},
	)

	auth := &Authenticator{
		fsm:     fsm,
		bus:     bus,
		socket:  s,
		service: svc,
		logger:  log.Discard,
		store:   nopStore{},
	}
	for _, opt := range opts {
		opt(auth)
	}

	publishState := func(_ context.Context, from State, _ Event, to State) error {
		auth.bus.Publish(&StateEvent{Old: from, New: to})
		return nil
	}
	for _, ev := range []Event{EventBegin, EventLoggingOn, EventSuccess, EventFail, EventDisconnect} {
		fsm.OnAfter(ev, publishState)
	}

	s.RegisterMsgHandler(enums.EMsg_ChannelEncryptRequest, auth.handleChannelEncryptRequest)
	s.RegisterMsgHandler(enums.EMsg_ChannelEncryptResult, auth.handleChannelEncryptResult)
	s.RegisterMsgHandler(enums.EMsg_ClientLogOnResponse, auth.handleLogOnResponse)
	s.RegisterMsgHandler(enums.EMsg_ClientLoggedOff, auth.handleLoggedOff)

	return auth
}

// State returns the current authentication state.
func (a *Authenticator) State() State { return a.fsm.CurrentState() }

// LogOn initiates the login sequence.
func (a *Authenticator) LogOn(ctx context.Context, details *LogOnDetails, server connector.CMServer) error {
	if err := a.fsm.Transition(ctx, EventBegin); err != nil {
		return errors.New("auth: authentication already in progress")
	}

	defer a.ensureTerminalState()

	loginCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	a.enrichLogger(details)

	if err := a.prepareCredentials(loginCtx, details); err != nil {
		return err
	}

	a.setState(StateLoggingOn)

	resultChan := make(chan error, 1)
	a.loginResult.Store(resultChan)
	a.loginCancel.Store(cancel)
	a.activeDetails.Store(details)

	if err := a.socket.Connect(loginCtx, server); err != nil {
		return fmt.Errorf("cm connection failed: %w", err)
	}

	a.configureSession(details)

	if server.Type == "websockets" {
		a.getLogger().Debug("WebSocket detected, starting logon sequence immediately")
		a.sendLogOn(loginCtx, details)
	}

	return a.waitForLogOn(loginCtx, resultChan, details.AccountName)
}

// LogOnAnonymous performs a login without user credentials.
func (a *Authenticator) LogOnAnonymous(ctx context.Context, server connector.CMServer) error {
	if err := a.fsm.Transition(ctx, EventBegin); err != nil {
		return errors.New("auth: authentication already in progress")
	}

	defer a.ensureTerminalState()

	loginCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	anonDetails := &LogOnDetails{
		ProtocolVersion: ProtocolVersion,
		ClientOSType:    uint32(enums.EOSType_Windows10),
	}

	_ = a.fsm.Transition(context.Background(), EventLoggingOn)

	resultChan := make(chan error, 1)
	a.loginResult.Store(resultChan)
	a.loginCancel.Store(cancel)
	a.activeDetails.Store(anonDetails)

	if err := a.socket.Connect(ctx, server); err != nil {
		return fmt.Errorf("cm connection failed: %w", err)
	}

	if server.Type == "websockets" {
		a.sendLogOn(loginCtx, anonDetails)
	}

	return a.waitForLogOn(loginCtx, resultChan, "")
}

func (a *Authenticator) ensureTerminalState() {
	if a.State() != StateLoggedOn {
		_ = a.fsm.Transition(context.Background(), EventFail)
	}
}

func (a *Authenticator) enrichLogger(details *LogOnDetails) {
	if details == nil {
		return
	}

	var logFields []log.Field
	if details.AccountName != "" {
		logFields = append(logFields, log.String("account", details.AccountName))
	}

	if details.SteamID != 0 {
		logFields = append(logFields, log.SteamID(details.SteamID.Uint64()))
	}

	if len(logFields) > 0 {
		a.setLogger(a.getLogger().With(logFields...))
	}
}

func (a *Authenticator) prepareCredentials(ctx context.Context, details *LogOnDetails) error {
	if err := a.validate(details); err != nil {
		return err
	}

	if len(details.MachineID) == 0 {
		a.acquireMachineID(ctx, details)
	}

	return a.acquireAuthToken(ctx, details)
}

func (a *Authenticator) configureSession(details *LogOnDetails) {
	sess := a.socket.Session()
	if sess == nil {
		return
	}

	sess.SetSteamID(details.SteamID.Uint64())
	sess.SetRefreshToken(details.RefreshToken)
}

func (a *Authenticator) waitForLogOn(ctx context.Context, resultChan chan error, accountName string) error {
	var err error
	select {
	case err = <-resultChan:
	case <-ctx.Done():
		err = ctx.Err()
	}

	if err == nil {
		_ = a.fsm.Transition(context.Background(), EventSuccess)
		if details := a.activeDetails.Load(); details != nil {
			details.Wipe()
		}

		return nil
	}

	var resultErr *service.EResultError
	if errors.As(err, &resultErr) && resultErr.Result == enums.EResult_InvalidPassword {
		a.getLogger().Warn("Session rejected by CM (Invalid Password/Token), clearing local storage")
		_ = a.store.Clear(ctx, accountName)
	}

	return err
}

func (a *Authenticator) validate(details *LogOnDetails) error {
	if details == nil {
		return errors.New("auth: nil details provided")
	}

	return details.Validate()
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

	switch confType {
	case pb.EAuthSessionGuardType_k_EAuthSessionGuardType_EmailCode,
		pb.EAuthSessionGuardType_k_EAuthSessionGuardType_DeviceCode:
		a.handleGuardCodeConfirmation(ctx, conf, resp, confType)
	case pb.EAuthSessionGuardType_k_EAuthSessionGuardType_DeviceConfirmation:
		a.getLogger().Info("Mobile app confirmation required (Accept prompt on phone)")
		a.bus.Publish(&SteamGuardRequiredEvent{IsAppConfirm: true})
	}
}

func (a *Authenticator) handleGuardCodeConfirmation(
	ctx context.Context,
	conf *pb.CAuthentication_AllowedConfirmation,
	resp *pb.CAuthentication_BeginAuthSessionViaCredentials_Response,
	confType pb.EAuthSessionGuardType,
) {
	is2FA := confType == pb.EAuthSessionGuardType_k_EAuthSessionGuardType_DeviceCode

	msg := "2FA code required"
	if !is2FA {
		msg = "Email confirmation required"
	}

	a.getLogger().Info(msg, log.String("associated_message", conf.GetAssociatedMessage()))

	a.bus.Publish(&SteamGuardRequiredEvent{
		Is2FA:       is2FA,
		EmailDomain: conf.GetAssociatedMessage(),
		Callback: func(code string) {
			if code == "" {
				return
			}

			go a.submitGuardCode(ctx, resp.GetClientId(), resp.GetSteamid(), code, confType)
		},
	})
}

func (a *Authenticator) submitGuardCode(
	ctx context.Context,
	clientID, steamID uint64,
	code string,
	confType pb.EAuthSessionGuardType,
) {
	err := a.service.UpdateAuthSessionWithSteamGuardCode(ctx, clientID, steamID, code, confType)
	if err != nil {
		a.getLogger().Error("Failed to submit guard code", log.Err(err))
		a.failLogin(fmt.Errorf("steam guard rejected: %w", err))
	}
}

func (a *Authenticator) pollAuthStatus(
	ctx context.Context,
	clientID uint64,
	requestID []byte,
	steamID uint64,
	interval time.Duration,
) (string, string, uint64, error) {
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
					a.getLogger().Debug("Poll status warning", log.Err(err))
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
	var event Event
	switch state {
	case StateLoggingOn:
		event = EventLoggingOn
	case StateLoggedOn:
		event = EventSuccess
	case StateFailed:
		event = EventFail
	case StateDisconnected:
		event = EventDisconnect
	default:
		return
	}

	_ = a.fsm.Transition(context.Background(), event)
}

func (a *Authenticator) succeedLogin() {
	if ch, ok := a.loginResult.Load().(chan error); ok && ch != nil {
		select {
		case ch <- nil:
		default:
		}
	}
}

func (a *Authenticator) failLogin(err error) {
	if cancelFunc, ok := a.loginCancel.Load().(context.CancelFunc); ok {
		cancelFunc()
	}

	if ch, ok := a.loginResult.Load().(chan error); ok && ch != nil {
		select {
		case ch <- err:
		default:
		}
	}
}

func (a *Authenticator) acquireMachineID(ctx context.Context, details *LogOnDetails) {
	saved, err := a.store.GetMachineID(ctx, details.AccountName)
	if err == nil && len(saved) > 0 {
		a.getLogger().Debug("Found saved MachineID in storage")

		details.MachineID = saved
	} else {
		a.getLogger().Info("Generating new MachineID for account")

		details.MachineID = generateMachineID(details.AccountName)
		if err := a.store.SaveMachineID(ctx, details.AccountName, details.MachineID); err != nil {
			a.getLogger().Error("Storage save failed", log.Err(err))
		}
	}
}

func (a *Authenticator) acquireAuthToken(ctx context.Context, details *LogOnDetails) error {
	if details.RefreshToken == "" {
		token, err := a.store.GetRefreshToken(ctx, details.AccountName)
		if err == nil && token != "" {
			a.getLogger().Info("Found saved refresh token in storage")

			details.RefreshToken = token
		}
	}

	if details.SteamID == 0 {
		details.SteamID = ExtractSteamIDFromJWT(details.RefreshToken)
		if details.SteamID != 0 {
			a.getLogger().Debug("Extracted SteamID from saved token", log.SteamID(details.SteamID.Uint64()))
		}
	}

	if details.RefreshToken == "" {
		a.getLogger().Info("No saved token, performing password authentication via WebAPI")

		refresh, access, steamID, err := a.performPasswordAuth(ctx, details)
		if err != nil {
			return err
		}

		details.RefreshToken = refresh
		details.AccessToken = access
		details.SteamID = id.ID(steamID)

		if err := a.store.SaveRefreshToken(ctx, details.AccountName, refresh); err != nil {
			a.getLogger().Error("Storage save failed", log.Err(err))
		}
	}

	return nil
}

func (a *Authenticator) getLogger() log.Logger {
	a.loggerMu.RLock()
	defer a.loggerMu.RUnlock()

	if a.logger == nil {
		return log.Discard
	}

	return a.logger
}

func (a *Authenticator) setLogger(l log.Logger) {
	a.loggerMu.Lock()
	defer a.loggerMu.Unlock()

	a.logger = l
}

func generateMachineID(accountName string) []byte {
	if accountName == "" {
		var b [42]byte

		_, _ = rand.Read(b[:])

		return b[:]
	}

	return crypto.GenerateAccountMachineID(accountName)
}

type nopStore struct{}

func (nopStore) SaveRefreshToken(ctx context.Context, acc, tok string) error     { return nil }
func (nopStore) GetRefreshToken(ctx context.Context, acc string) (string, error) { return "", nil }
func (nopStore) SaveMachineID(ctx context.Context, acc string, id []byte) error  { return nil }
func (nopStore) GetMachineID(ctx context.Context, acc string) ([]byte, error)    { return nil, nil }
func (nopStore) Clear(ctx context.Context, acc string) error                     { return nil }

func (a *Authenticator) setLoginResult(ch chan error) {
	a.loginResult.Store(ch)
}

func (a *Authenticator) getLoginResult() chan error {
	if val := a.loginResult.Load(); val != nil {
		if ch, ok := val.(chan error); ok {
			return ch
		}
	}

	return nil
}

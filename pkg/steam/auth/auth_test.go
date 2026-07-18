// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package auth

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/lemon4ksan/miyako/bus"
	"github.com/lemon4ksan/miyako/log"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/suite"
	"google.golang.org/protobuf/proto"

	pb "github.com/lemon4ksan/g-man/pkg/protobuf/steam"
	"github.com/lemon4ksan/g-man/pkg/steam/id"
	"github.com/lemon4ksan/g-man/pkg/steam/protocol"
	"github.com/lemon4ksan/g-man/pkg/steam/protocol/enums"
	"github.com/lemon4ksan/g-man/pkg/steam/socket"
)

type AuthenticatorSuite struct {
	suite.Suite
	bus     *bus.Bus
	socket  *MockSocketProvider
	webAPI  *MockWebAuthenticator
	store   *MockStore
	auth    *Authenticator
	session *mockSession
}

func (s *AuthenticatorSuite) SetupTest() {
	s.bus = bus.New()
	s.socket = NewMockSocket()
	s.webAPI = new(MockWebAuthenticator)
	s.store = new(MockStore)
	s.session = &mockSession{}
	s.socket.On("Session").Return(s.session).Maybe()
	s.auth = NewAuthenticator(s.socket, s.webAPI, s.bus, WithStorage(s.store), WithLogger(log.Discard))
}

func TestAuthenticatorSuite(t *testing.T) {
	suite.Run(t, new(AuthenticatorSuite))
}

func (s *AuthenticatorSuite) TestState_String() {
	s.Equal("disconnected", StateDisconnected.String())
	s.Equal("authenticating", StateAuthenticating.String())
	s.Equal("logging_on", StateLoggingOn.String())
	s.Equal("logged_on", StateLoggedOn.String())
	s.Equal("failed", StateFailed.String())
	s.Equal("unknown", State(999).String())
}

func (s *AuthenticatorSuite) TestExtractSteamIDFromJWT_Coverage() {
	valid := "a." + base64.RawURLEncoding.EncodeToString([]byte(`{"sub":"76561197960287930"}`)) + ".c"
	s.Equal(id.ID(76561197960287930), ExtractSteamIDFromJWT(valid))

	s.Equal(id.ID(0), ExtractSteamIDFromJWT("one.two"))
	s.Equal(id.ID(0), ExtractSteamIDFromJWT("a.!!!.c"))

	badJSON := base64.StdEncoding.EncodeToString([]byte(`{not-json}`))
	s.Equal(id.ID(0), ExtractSteamIDFromJWT("a."+badJSON+".c"))
}

func (s *AuthenticatorSuite) TestLogOn_Validation() {
	s.ErrorContains(s.auth.LogOn(s.T().Context(), nil, socket.CMServer{}), "nil details")
	s.ErrorContains(
		s.auth.LogOn(s.T().Context(), &LogOnDetails{}, socket.CMServer{}),
		"account name or refresh token is required",
	)
	s.ErrorContains(
		s.auth.LogOn(s.T().Context(), &LogOnDetails{AccountName: "a"}, socket.CMServer{}),
		"password is required",
	)

	details := &LogOnDetails{AccountName: "a", Password: "p"}

	s.store.On("GetMachineID", mock.Anything, "a").Return([]byte("id"), nil)
	s.store.On("GetRefreshToken", mock.Anything, "a").Return("", nil)
	s.webAPI.On("BeginAuthSessionViaCredentials", mock.Anything, "a", "p", "").Return(nil, errors.New("stop"))

	_ = s.auth.LogOn(s.T().Context(), details, socket.CMServer{})
	s.Equal(uint32(ProtocolVersion), details.ProtocolVersion)
	s.Equal("english", details.ClientLanguage)
}

func (s *AuthenticatorSuite) TestAcquireMachineId_Generation() {
	details := &LogOnDetails{AccountName: "new"}

	s.store.On("GetMachineID", mock.Anything, "new").Return(nil, errors.New("not found"))
	s.store.On("SaveMachineID", mock.Anything, "new", mock.Anything).Return(errors.New("log coverage error"))

	s.auth.acquireMachineID(s.T().Context(), details)
	s.True(len(details.MachineID) > 0)
}

func (s *AuthenticatorSuite) TestLogOnAnonymous_Coverage() {
	server := socket.CMServer{Type: "websockets"}

	s.auth.fsm.ForceSet(StateLoggingOn)
	s.ErrorContains(s.auth.LogOnAnonymous(s.T().Context(), server), "already in progress")
	s.auth.fsm.ForceSet(StateDisconnected)

	s.socket.On("Connect", server).Return(errors.New("dial timeout")).Once()
	s.ErrorContains(s.auth.LogOnAnonymous(s.T().Context(), server), "dial timeout")

	s.socket.On("Connect", server).Return(nil)
	s.socket.On("SendProto", mock.Anything, mock.Anything, mock.Anything).Return(nil).Maybe()

	ctx, cancel := context.WithCancel(s.T().Context())
	cancel()
	s.ErrorIs(s.auth.LogOnAnonymous(ctx, server), context.Canceled)
}

func (s *AuthenticatorSuite) TestResolveConfirmation_Coverage() {
	resp := &pb.CAuthentication_BeginAuthSessionViaCredentials_Response{
		ClientId: proto.Uint64(1),
		Steamid:  proto.Uint64(123),
	}

	sub := s.bus.Subscribe(&SteamGuardRequiredEvent{})
	defer sub.Unsubscribe()

	ctx, cancel := context.WithCancelCause(s.T().Context())
	defer cancel(nil)

	s.auth.resolveConfirmation(ctx, cancel, &pb.CAuthentication_AllowedConfirmation{
		ConfirmationType:  pb.EAuthSessionGuardType_k_EAuthSessionGuardType_EmailCode.Enum(),
		AssociatedMessage: proto.String("email.com"),
	}, resp)

	ev := (<-sub.C()).(*SteamGuardRequiredEvent)
	s.False(ev.Is2FA)

	s.webAPI.On("UpdateAuthSessionWithSteamGuardCode", mock.Anything, mock.Anything, mock.Anything, "code", mock.Anything).
		Return(errors.New("fail"))

	ev.Callback("code")

	<-ctx.Done()

	err := context.Cause(ctx)
	s.ErrorContains(err, "fail")

	ctx, cancel = context.WithCancelCause(s.T().Context())
	defer cancel(nil)

	s.auth.resolveConfirmation(ctx, cancel, &pb.CAuthentication_AllowedConfirmation{
		ConfirmationType: pb.EAuthSessionGuardType_k_EAuthSessionGuardType_DeviceConfirmation.Enum(),
	}, resp)

	ev2 := (<-sub.C()).(*SteamGuardRequiredEvent)
	s.True(ev2.IsAppConfirm)
}

func (s *AuthenticatorSuite) TestPollAuthStatus_Coverage() {
	ctx, cancel := context.WithCancelCause(s.T().Context())
	cancel(errors.New("dead"))

	_, _, _, err := s.auth.pollAuthStatus(ctx, 1, nil, 0, time.Millisecond)
	s.ErrorContains(err, "dead")

	ctx2, cancel2 := context.WithTimeout(s.T().Context(), 50*time.Millisecond)
	defer cancel2()

	s.webAPI.On("PollAuthSessionStatus", mock.Anything, mock.Anything, mock.Anything).
		Return(nil, errors.New("DuplicateRequest")).Maybe()
	s.webAPI.On("PollAuthSessionStatus", mock.Anything, mock.Anything, mock.Anything).
		Return(nil, errors.New("OtherError")).Maybe()
	_, _, _, _ = s.auth.pollAuthStatus(ctx2, 1, nil, 0, time.Millisecond)
}

func (s *AuthenticatorSuite) TestHandlers_Coverage() {
	// handleChannelEncryptRequest
	buf := new(bytes.Buffer)
	binary.Write(buf, binary.LittleEndian, uint32(ProtocolVersion))
	binary.Write(buf, binary.LittleEndian, uint32(enums.EUniverse_Public))
	buf.Write(make([]byte, 16))
	s.socket.On("SendRaw", mock.Anything, enums.EMsg_ChannelEncryptResponse, mock.Anything).Return(nil)
	s.socket.SimulatePacketRaw(enums.EMsg_ChannelEncryptRequest, buf.Bytes())

	// handleChannelEncryptResult success
	key := []byte("12345678901234567890123456789012")
	s.auth.tempKey.Store(&key)
	s.auth.activeDetails.Store(&LogOnDetails{AccountName: "u"})
	s.socket.On("SendProto", mock.Anything, enums.EMsg_ClientLogon, mock.Anything).Return(nil)

	res := make([]byte, 4)
	binary.LittleEndian.PutUint32(res, uint32(enums.EResult_OK))
	s.socket.SimulatePacketRaw(enums.EMsg_ChannelEncryptResult, res)
	s.Nil(s.auth.tempKey.Load())

	// handleLogOnResponse failure (clear coverage)
	s.auth.setLoginResult(make(chan error, 1))
	s.socket.SimulatePacket(
		enums.EMsg_ClientLogOnResponse,
		&pb.CMsgClientLogonResponse{Eresult: proto.Int32(int32(enums.EResult_NoConnection))},
	)
	s.Error(<-s.auth.getLoginResult())
}

func (s *AuthenticatorSuite) TestAcquireAuthToken_Coverage() {
	details := &LogOnDetails{AccountName: "u", RefreshToken: "a.eyJzdWIiOiIxMjMifQ.c"}
	ctx, cancel := context.WithCancelCause(s.T().Context())

	s.auth.acquireAuthToken(ctx, cancel, details)
	s.Equal(id.ID(123), details.SteamID) // Test SteamID logging branch

	details2 := &LogOnDetails{AccountName: "u2"}

	s.store.On("GetRefreshToken", mock.Anything, "u2").Return("", nil)
	s.webAPI.On("BeginAuthSessionViaCredentials", mock.Anything, mock.Anything, mock.Anything, mock.Anything).
		Return(&pb.CAuthentication_BeginAuthSessionViaCredentials_Response{Interval: proto.Float32(0.01)}, nil)
	s.webAPI.On("PollAuthSessionStatus", mock.Anything, mock.Anything, mock.Anything).
		Return(&pb.CAuthentication_PollAuthSessionStatus_Response{RefreshToken: proto.String("rt")}, nil)
	s.store.On("SaveRefreshToken", mock.Anything, "u2", "rt").Return(errors.New("fail"))

	ctx, cancel = context.WithCancelCause(s.T().Context())
	defer cancel(nil)

	s.auth.acquireAuthToken(ctx, cancel, details2)
}

func (s *AuthenticatorSuite) TestNopStore() {
	n := nopStore{}
	ctx := s.T().Context()
	_ = n.SaveRefreshToken(ctx, "", "")
	_ = n.SaveMachineID(ctx, "", nil)
	_ = n.Clear(ctx, "")
	_, _ = n.GetRefreshToken(ctx, "")
	_, _ = n.GetMachineID(ctx, "")
}

func (s *AuthenticatorSuite) TestNewLogOnDetails() {
	details := NewLogOnDetails("acc", "pwd")
	s.Equal("acc", details.AccountName)
	s.Equal("pwd", details.Password)
	s.Equal("english", details.ClientLanguage)
	s.Equal(uint32(ProtocolVersion), details.ProtocolVersion)
}

func (s *AuthenticatorSuite) TestLogOn_WebSocket_Success() {
	server := socket.CMServer{Type: "websockets", Endpoint: "localhost"}
	details := &LogOnDetails{
		RefreshToken: "a.eyJzdWIiOiIxMjMifQ.c",
		MachineID:    []byte("machid"),
	}

	connected := make(chan struct{})
	s.socket.On("Connect", server).Return(nil).Run(func(args mock.Arguments) {
		close(connected)
	}).Once()

	s.socket.On("SendProto", mock.Anything, enums.EMsg_ClientLogon, mock.Anything).Return(nil).Once()
	s.socket.On("StartHeartbeat", 10*time.Second).Return(nil).Once()

	done := make(chan struct{})

	var logonErr error
	go func() {
		logonErr = s.auth.LogOn(s.T().Context(), details, server)

		close(done)
	}()

	<-connected

	packet := &protocol.Packet{
		EMsg: enums.EMsg_ClientLogOnResponse,
		Payload: func() []byte {
			b, _ := proto.Marshal(&pb.CMsgClientLogonResponse{
				Eresult:          proto.Int32(int32(enums.EResult_OK)),
				HeartbeatSeconds: proto.Int32(10),
			})

			return b
		}(),
		Header: &mockAuthorizedHeader{steamID: 123, sessionID: 456},
	}
	s.auth.handleLogOnResponse(packet)

	<-done
	s.NoError(logonErr)
	s.Equal(StateLoggedOn, s.auth.State())
}

func (s *AuthenticatorSuite) TestLogOn_InvalidPassword_ClearStore() {
	server := socket.CMServer{Type: "websockets", Endpoint: "localhost"}
	details := &LogOnDetails{
		RefreshToken: "a.eyJzdWIiOiIxMjMifQ.c",
		MachineID:    []byte("machid"),
		AccountName:  "myuser",
	}

	connected := make(chan struct{})
	s.socket.On("Connect", server).Return(nil).Run(func(args mock.Arguments) {
		close(connected)
	}).Once()

	s.socket.On("SendProto", mock.Anything, enums.EMsg_ClientLogon, mock.Anything).Return(nil).Once()
	s.store.On("Clear", mock.Anything, "myuser").Return(nil).Once()

	done := make(chan struct{})

	var logonErr error
	go func() {
		logonErr = s.auth.LogOn(s.T().Context(), details, server)

		close(done)
	}()

	<-connected

	s.socket.SimulatePacket(enums.EMsg_ClientLogOnResponse, &pb.CMsgClientLogonResponse{
		Eresult: proto.Int32(int32(enums.EResult_InvalidPassword)),
	})

	<-done
	s.Error(logonErr)
	s.Equal(StateFailed, s.auth.State())
}

func (s *AuthenticatorSuite) TestHandleLogOnResponse_HeartbeatFailure() {
	s.auth.setLoginResult(make(chan error, 1))
	s.socket.On("StartHeartbeat", mock.Anything).Return(errors.New("heartbeat err")).Once()

	packet := &protocol.Packet{
		EMsg: enums.EMsg_ClientLogOnResponse,
		Payload: func() []byte {
			b, _ := proto.Marshal(&pb.CMsgClientLogonResponse{
				Eresult:          proto.Int32(int32(enums.EResult_OK)),
				HeartbeatSeconds: proto.Int32(10),
			})

			return b
		}(),
		Header: &mockAuthorizedHeader{steamID: 123, sessionID: 456},
	}
	s.auth.handleLogOnResponse(packet)

	err := <-s.auth.getLoginResult()
	s.ErrorContains(err, "failed to start heartbeat")
}

func (s *AuthenticatorSuite) TestFailLogin_DoubleChannelSend() {
	s.auth.setLoginResult(make(chan error, 1))

	err1 := errors.New("err1")
	err2 := errors.New("err2")

	s.auth.failLogin(err1)
	s.auth.failLogin(err2)

	s.Equal(err1, <-s.auth.getLoginResult())
}

type MockSocketProvider struct {
	mock.Mock
	handlers map[enums.EMsg]socket.Handler
}

func NewMockSocket() *MockSocketProvider {
	return &MockSocketProvider{handlers: make(map[enums.EMsg]socket.Handler)}
}
func (m *MockSocketProvider) RegisterMsgHandler(e enums.EMsg, h socket.Handler) { m.handlers[e] = h }
func (m *MockSocketProvider) Connect(ctx context.Context, s socket.CMServer) error {
	return m.Called(s).Error(0)
}

func (m *MockSocketProvider) Session() socket.Session { return m.Called().Get(0).(socket.Session) }
func (m *MockSocketProvider) StartHeartbeat(d time.Duration) error {
	args := m.Called(d)
	if len(args) > 0 {
		return args.Error(0)
	}

	return nil
}

func (m *MockSocketProvider) SetEncryptionKey(key []byte) bool { return true }

func (m *MockSocketProvider) SendProto(
	ctx context.Context,
	e enums.EMsg,
	msg proto.Message,
	opts ...socket.SendOption,
) error {
	return m.Called(ctx, e, msg).Error(0)
}

func (m *MockSocketProvider) SendRaw(ctx context.Context, e enums.EMsg, p []byte, opts ...socket.SendOption) error {
	return m.Called(ctx, e, p).Error(0)
}

func (m *MockSocketProvider) SimulatePacket(e enums.EMsg, msg proto.Message) {
	data, _ := proto.Marshal(msg)
	m.SimulatePacketRaw(e, data)
}

func (m *MockSocketProvider) SimulatePacketRaw(e enums.EMsg, data []byte) {
	if h, ok := m.handlers[e]; ok {
		h(&protocol.Packet{EMsg: e, Payload: data})
	}
}

type mockSession struct {
	mock.Mock
	mu      sync.Mutex
	steamID uint64
	token   string
	access  string
}

func (m *mockSession) SteamID() uint64                    { m.mu.Lock(); defer m.mu.Unlock(); return m.steamID }
func (m *mockSession) SetSteamID(id uint64)               { m.mu.Lock(); defer m.mu.Unlock(); m.steamID = id }
func (m *mockSession) SetRefreshToken(t string)           { m.mu.Lock(); defer m.mu.Unlock(); m.token = t }
func (m *mockSession) RefreshToken() string               { m.mu.Lock(); defer m.mu.Unlock(); return m.token }
func (m *mockSession) SetAccessToken(t string)            { m.mu.Lock(); defer m.mu.Unlock(); m.access = t }
func (m *mockSession) AccessToken() string                { m.mu.Lock(); defer m.mu.Unlock(); return m.access }
func (m *mockSession) SetSessionID(int32)                 {}
func (m *mockSession) Send(context.Context, []byte) error { return nil }
func (m *mockSession) Close() error                       { return nil }
func (m *mockSession) IsEncrypted() bool                  { return true }
func (m *mockSession) IsAuthenticated() bool              { return true }
func (m *mockSession) SessionID() int32                   { return 0 }

type MockWebAuthenticator struct{ mock.Mock }

func (m *MockWebAuthenticator) BeginAuthSessionViaCredentials(
	ctx context.Context,
	n, p, c string,
) (*pb.CAuthentication_BeginAuthSessionViaCredentials_Response, error) {
	args := m.Called(ctx, n, p, c)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}

	return args.Get(0).(*pb.CAuthentication_BeginAuthSessionViaCredentials_Response), args.Error(1)
}

func (m *MockWebAuthenticator) PollAuthSessionStatus(
	ctx context.Context,
	id uint64,
	r []byte,
) (*pb.CAuthentication_PollAuthSessionStatus_Response, error) {
	args := m.Called(ctx, id, r)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}

	return args.Get(0).(*pb.CAuthentication_PollAuthSessionStatus_Response), args.Error(1)
}

func (m *MockWebAuthenticator) UpdateAuthSessionWithSteamGuardCode(
	ctx context.Context,
	cid, sid uint64,
	c string,
	t pb.EAuthSessionGuardType,
) error {
	return m.Called(ctx, cid, sid, c, t).Error(0)
}

func (m *MockWebAuthenticator) GenerateAccessTokenForApp(
	ctx context.Context,
	t string,
	id uint64,
) (*pb.CAuthentication_AccessToken_GenerateForApp_Response, error) {
	return nil, nil
}

type MockStore struct{ mock.Mock }

func (m *MockStore) SaveRefreshToken(ctx context.Context, a, t string) error {
	return m.Called(ctx, a, t).Error(0)
}

func (m *MockStore) GetRefreshToken(ctx context.Context, a string) (string, error) {
	args := m.Called(ctx, a)
	return args.String(0), args.Error(1)
}

func (m *MockStore) SaveMachineID(ctx context.Context, a string, id []byte) error {
	return m.Called(ctx, a, id).Error(0)
}

func (m *MockStore) GetMachineID(ctx context.Context, a string) ([]byte, error) {
	args := m.Called(ctx, a)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}

	return args.Get(0).([]byte), args.Error(1)
}

func (m *MockStore) Clear(ctx context.Context, a string) error {
	return m.Called(ctx, a).Error(0)
}

func TestLogOnDetails_Wipe(t *testing.T) {
	t.Parallel()

	details := &LogOnDetails{
		AccountName:   "testuser",
		Password:      "secretpassword",
		RefreshToken:  "refresh_token_123",
		AccessToken:   "access_token_456",
		AuthCode:      "ABC123",
		TwoFactorCode: "123456",
		SteamID:       76561198000000001,
	}

	details.Wipe()

	if details.Password != "" {
		t.Errorf("expected Password to be empty after Wipe(), got %q", details.Password)
	}

	if details.AuthCode != "" {
		t.Errorf("expected AuthCode to be empty after Wipe(), got %q", details.AuthCode)
	}

	if details.TwoFactorCode != "" {
		t.Errorf("expected TwoFactorCode to be empty after Wipe(), got %q", details.TwoFactorCode)
	}

	if details.AccountName != "testuser" {
		t.Errorf("expected AccountName to be preserved, got %q", details.AccountName)
	}

	if details.RefreshToken != "refresh_token_123" {
		t.Errorf("expected RefreshToken to be preserved, got %q", details.RefreshToken)
	}

	if details.AccessToken != "access_token_456" {
		t.Errorf("expected AccessToken to be preserved, got %q", details.AccessToken)
	}

	if details.SteamID != 76561198000000001 {
		t.Errorf("expected SteamID to be preserved, got %d", details.SteamID)
	}
}

func TestLogOnDetails_Wipe_EmptyFields(t *testing.T) {
	t.Parallel()

	details := &LogOnDetails{
		AccountName: "testuser",
	}

	assert.NotPanics(t, func() {
		details.Wipe()
	})

	if details.Password != "" {
		t.Errorf("expected Password to remain empty, got %q", details.Password)
	}
}

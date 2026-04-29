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
	"testing"
	"time"

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/suite"
	"google.golang.org/protobuf/proto"

	"github.com/lemon4ksan/g-man/pkg/bus"
	"github.com/lemon4ksan/g-man/pkg/log"
	pb "github.com/lemon4ksan/g-man/pkg/protobuf/steam"
	"github.com/lemon4ksan/g-man/pkg/steam/id"
	"github.com/lemon4ksan/g-man/pkg/steam/protocol"
	"github.com/lemon4ksan/g-man/pkg/steam/protocol/enums"
	"github.com/lemon4ksan/g-man/pkg/steam/socket"
)

type AuthenticatorSuite struct {
	suite.Suite
	socket  *MockSocketProvider
	webAPI  *MockWebAuthenticator
	store   *MockStore
	auth    *Authenticator
	session *mockSession
}

func (s *AuthenticatorSuite) SetupTest() {
	s.socket = NewMockSocket()
	s.webAPI = new(MockWebAuthenticator)
	s.store = new(MockStore)
	s.session = &mockSession{}
	s.socket.On("Session").Return(s.session).Maybe()
	s.auth = NewAuthenticator(s.socket, s.webAPI, WithStorage(s.store), WithLogger(log.Discard))
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
	s.ErrorContains(s.auth.LogOn(context.Background(), nil, socket.CMServer{}), "nil details")
	s.ErrorContains(
		s.auth.LogOn(context.Background(), &LogOnDetails{}, socket.CMServer{}),
		"account name or refresh token is required",
	)
	s.ErrorContains(
		s.auth.LogOn(context.Background(), &LogOnDetails{AccountName: "a"}, socket.CMServer{}),
		"password is required",
	)

	details := &LogOnDetails{AccountName: "a", Password: "p"}

	s.store.On("GetMachineID", mock.Anything, "a").Return([]byte("id"), nil)
	s.store.On("GetRefreshToken", mock.Anything, "a").Return("", nil)
	s.webAPI.On("BeginAuthSessionViaCredentials", mock.Anything, "a", "p", "").Return(nil, errors.New("stop"))

	_ = s.auth.LogOn(context.Background(), details, socket.CMServer{})
	s.Equal(uint32(ProtocolVersion), details.ProtocolVersion)
	s.Equal("english", details.ClientLanguage)
}

func (s *AuthenticatorSuite) TestAcquireMachineId_Generation() {
	details := &LogOnDetails{AccountName: "new"}

	s.store.On("GetMachineID", mock.Anything, "new").Return(nil, errors.New("not found"))
	s.store.On("SaveMachineID", mock.Anything, "new", mock.Anything).Return(errors.New("log coverage error"))

	s.auth.acquireMachineId(context.Background(), details)
	s.Len(details.MachineID, 42)
}

func (s *AuthenticatorSuite) TestLogOn_InvalidPassword_ClearsStorage() {
	details := &LogOnDetails{AccountName: "user", RefreshToken: "token", SteamID: 123}
	server := socket.CMServer{Type: "websockets"}

	s.store.On("GetMachineID", mock.Anything, "user").Return([]byte("id"), nil)
	s.store.On("Clear", mock.Anything, "user").Return(nil).Once()
	s.socket.On("Connect", server).Return(nil)
	s.socket.On("SendProto", mock.Anything, mock.Anything, mock.Anything).Run(func(args mock.Arguments) {
		s.socket.SimulatePacket(enums.EMsg_ClientLogOnResponse, &pb.CMsgClientLogonResponse{
			Eresult: proto.Int32(int32(enums.EResult_InvalidPassword)),
		})
	}).Return(nil)

	err := s.auth.LogOn(context.Background(), details, server)
	s.Error(err)
	s.store.AssertExpectations(s.T())
}

func (s *AuthenticatorSuite) TestLogOnAnonymous_Coverage() {
	server := socket.CMServer{Type: "websockets"}

	s.auth.setState(StateLoggingOn)
	s.ErrorContains(s.auth.LogOnAnonymous(context.Background(), server), "already in progress")
	s.auth.setState(StateDisconnected)

	s.socket.On("Connect", server).Return(errors.New("dial timeout")).Once()
	s.ErrorContains(s.auth.LogOnAnonymous(context.Background(), server), "dial timeout")

	s.socket.On("Connect", server).Return(nil)
	s.socket.On("SendProto", mock.Anything, mock.Anything, mock.Anything).Return(nil).Maybe()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	s.ErrorIs(s.auth.LogOnAnonymous(ctx, server), context.Canceled)
}

func (s *AuthenticatorSuite) TestResolveConfirmation_Coverage() {
	resp := &pb.CAuthentication_BeginAuthSessionViaCredentials_Response{
		ClientId: proto.Uint64(1),
		Steamid:  proto.Uint64(123),
	}
	sub := s.socket.Bus().Subscribe(&SteamGuardRequiredEvent{})

	s.auth.resolveConfirmation(context.Background(), &pb.CAuthentication_AllowedConfirmation{
		ConfirmationType:  pb.EAuthSessionGuardType_k_EAuthSessionGuardType_EmailCode.Enum(),
		AssociatedMessage: proto.String("email.com"),
	}, resp)

	ev := (<-sub.C()).(*SteamGuardRequiredEvent)
	s.False(ev.Is2FA)

	s.webAPI.On("UpdateAuthSessionWithSteamGuardCode", mock.Anything, mock.Anything, mock.Anything, "code", mock.Anything).
		Return(errors.New("fail"))
	s.auth.loginResult = make(chan error, 1)

	ev.Callback("code") // Triggers goroutine
	time.Sleep(50 * time.Millisecond)

	s.auth.resolveConfirmation(context.Background(), &pb.CAuthentication_AllowedConfirmation{
		ConfirmationType: pb.EAuthSessionGuardType_k_EAuthSessionGuardType_DeviceConfirmation.Enum(),
	}, resp)

	ev2 := (<-sub.C()).(*SteamGuardRequiredEvent)
	s.True(ev2.IsAppConfirm)
}

func (s *AuthenticatorSuite) TestPollAuthStatus_Coverage() {
	ctx, cancel := context.WithCancelCause(context.Background())
	cancel(errors.New("dead"))

	_, _, _, err := s.auth.pollAuthStatus(ctx, 1, nil, 0, time.Millisecond)
	s.ErrorContains(err, "dead")

	ctx2, cancel2 := context.WithTimeout(context.Background(), 50*time.Millisecond)
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
	s.auth.loginResult = make(chan error, 1)
	s.socket.SimulatePacket(
		enums.EMsg_ClientLogOnResponse,
		&pb.CMsgClientLogonResponse{Eresult: proto.Int32(int32(enums.EResult_NoConnection))},
	)
	s.Error(<-s.auth.loginResult)
}

func (s *AuthenticatorSuite) TestAcquireAuthToken_Coverage() {
	details := &LogOnDetails{AccountName: "u", RefreshToken: "a.eyJzdWIiOiIxMjMifQ.c"}
	s.auth.acquireAuthToken(context.Background(), details)
	s.Equal(id.ID(123), details.SteamID) // Test SteamID logging branch

	details2 := &LogOnDetails{AccountName: "u2"}

	s.store.On("GetRefreshToken", mock.Anything, "u2").Return("", nil)
	s.webAPI.On("BeginAuthSessionViaCredentials", mock.Anything, mock.Anything, mock.Anything, mock.Anything).
		Return(&pb.CAuthentication_BeginAuthSessionViaCredentials_Response{Interval: proto.Float32(0.01)}, nil)
	s.webAPI.On("PollAuthSessionStatus", mock.Anything, mock.Anything, mock.Anything).
		Return(&pb.CAuthentication_PollAuthSessionStatus_Response{RefreshToken: proto.String("rt")}, nil)
	s.store.On("SaveRefreshToken", mock.Anything, "u2", "rt").Return(errors.New("fail"))
	s.auth.acquireAuthToken(context.Background(), details2)
}

func (s *AuthenticatorSuite) TestNopStore() {
	n := nopStore{}
	_ = n.SaveRefreshToken(nil, "", "")
	_ = n.SaveMachineID(nil, "", nil)
	_ = n.Clear(nil, "")
	_, _ = n.GetRefreshToken(nil, "")
	_, _ = n.GetMachineID(nil, "")
}

type MockSocketProvider struct {
	mock.Mock
	bus      *bus.Bus
	handlers map[enums.EMsg]socket.Handler
}

func NewMockSocket() *MockSocketProvider {
	return &MockSocketProvider{bus: bus.New(), handlers: make(map[enums.EMsg]socket.Handler)}
}
func (m *MockSocketProvider) RegisterMsgHandler(e enums.EMsg, h socket.Handler) { m.handlers[e] = h }
func (m *MockSocketProvider) Connect(s socket.CMServer) error                   { return m.Called(s).Error(0) }

func (m *MockSocketProvider) Session() socket.Session        { return m.Called().Get(0).(socket.Session) }
func (m *MockSocketProvider) Bus() *bus.Bus                  { return m.bus }
func (m *MockSocketProvider) StartHeartbeat(d time.Duration) { m.Called(d) }

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
	steamID uint64
	token   string
	access  string
}

func (m *mockSession) SteamID() uint64                    { return m.steamID }
func (m *mockSession) SetSteamID(id uint64)               { m.steamID = id }
func (m *mockSession) SetRefreshToken(t string)           { m.token = t }
func (m *mockSession) RefreshToken() string               { return m.token }
func (m *mockSession) SetAccessToken(t string)            { m.access = t }
func (m *mockSession) AccessToken() string                { return m.access }
func (m *mockSession) SetSessionID(int32)                 {}
func (m *mockSession) SetEncryptionKey([]byte) bool       { return true }
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

// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package steam

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/mock"
	"google.golang.org/protobuf/proto"

	pb "github.com/lemon4ksan/g-man/pkg/protobuf/steam"
	"github.com/lemon4ksan/g-man/pkg/rest"
	"github.com/lemon4ksan/g-man/pkg/steam/auth"
	"github.com/lemon4ksan/g-man/pkg/steam/module"
	"github.com/lemon4ksan/g-man/pkg/steam/protocol"
	"github.com/lemon4ksan/g-man/pkg/steam/protocol/enums"
	"github.com/lemon4ksan/g-man/pkg/steam/service"
	"github.com/lemon4ksan/g-man/pkg/steam/socket"
	"github.com/lemon4ksan/g-man/pkg/steam/socket/connector"
	tr "github.com/lemon4ksan/g-man/pkg/steam/transport"
	"github.com/lemon4ksan/g-man/pkg/storage/memory"
)

type testMocks struct {
	http *mockHTTPDoer
	sock *mockSocket
	auth *mockAuthenticator
	web  *mockWebSession
	comm *mockCommunity
}

func setupTestClient(t *testing.T) (*Client, *testMocks) {
	m := &testMocks{
		http: new(mockHTTPDoer),
		sock: new(mockSocket),
		auth: new(mockAuthenticator),
		web:  new(mockWebSession),
		comm: new(mockCommunity),
	}

	m.sock.On("Disconnect").Return(nil).Maybe()
	m.sock.On("Close").Return(nil).Maybe()
	m.sock.On("IsConnected").Return(false).Maybe()

	cfg := Config{
		Storage: memory.New(),
		HTTP:    m.http,
		Device:  &auth.DeviceConfig{},
	}

	c, _ := NewClient(cfg)

	// Injecting mocks deep into dependencies
	c.socket = m.sock
	c.session.socket = m.sock
	c.router.socket = m.sock

	testTransport := tr.NewHTTPTransport(m.http, service.WebAPIBase)
	c.session.unified = service.New(testTransport)
	c.session.socketAPI = service.New(testTransport)

	c.session.auth = m.auth
	c.session.web = m.web
	c.session.community = m.comm

	t.Cleanup(func() { _ = c.Close() })

	return c, m
}

type mockSession struct{ mock.Mock }

func (m *mockSession) SteamID() uint64          { return m.Called().Get(0).(uint64) }
func (m *mockSession) RefreshToken() string     { return m.Called().String(0) }
func (m *mockSession) AccessToken() string      { return m.Called().String(0) }
func (m *mockSession) SetAccessToken(t string)  { m.Called(t) }
func (m *mockSession) SetRefreshToken(t string) { m.Called(t) }
func (m *mockSession) SessionID() int32         { return m.Called().Get(0).(int32) }
func (m *mockSession) IsAuthenticated() bool    { return m.Called().Bool(0) }
func (m *mockSession) SetSteamID(sid uint64)    { m.Called(sid) }
func (m *mockSession) SetSessionID(sid int32)   { m.Called(sid) }

type mockHTTPDoer struct{ mock.Mock }

func (m *mockHTTPDoer) Do(req *http.Request) (*http.Response, error) {
	args := m.Called(req)

	var resp *http.Response
	if args.Get(0) != nil {
		resp = args.Get(0).(*http.Response)
	}

	return resp, args.Error(1)
}

type mockSocket struct{ mock.Mock }

func (m *mockSocket) SetEncryptionKey(key []byte) bool                             { return false }
func (m *mockSocket) Connect(ctx context.Context, server connector.CMServer) error { return nil }

func (m *mockSocket) SendProto(
	ctx context.Context,
	eMsg enums.EMsg,
	req proto.Message,
	opts ...socket.SendOption,
) error {
	return nil
}

func (m *mockSocket) SendRaw(ctx context.Context, eMsg enums.EMsg, payload []byte, opts ...socket.SendOption) error {
	return nil
}
func (m *mockSocket) StartHeartbeat(duration time.Duration) error { return nil }

func (m *mockSocket) Send(
	ctx context.Context,
	build socket.PayloadBuilder,
	opts ...socket.SendOption,
) error {
	return nil
}

func (m *mockSocket) SendSync(
	ctx context.Context,
	build socket.PayloadBuilder,
	opts ...socket.SendOption,
) (*protocol.Packet, error) {
	return nil, nil
}
func (m *mockSocket) IsConnected() bool { return m.Called().Bool(0) }
func (m *mockSocket) Session() socket.Session {
	args := m.Called()
	if args.Get(0) == nil {
		return nil
	}

	return args.Get(0).(socket.Session)
}
func (m *mockSocket) RegisterMsgHandler(e enums.EMsg, h socket.Handler)    { m.Called(e, h) }
func (m *mockSocket) RegisterServiceHandler(meth string, h socket.Handler) { m.Called(meth, h) }
func (m *mockSocket) Disconnect() error                                    { return m.Called().Error(0) }
func (m *mockSocket) Close() error                                         { return m.Called().Error(0) }

type mockAuthenticator struct{ mock.Mock }

func (m *mockAuthenticator) LogOn(ctx context.Context, d *auth.LogOnDetails, s socket.CMServer) error {
	return m.Called(ctx, d, s).Error(0)
}

type mockWebSession struct{ mock.Mock }

func (m *mockWebSession) HTTP() *http.Client        { return &http.Client{} }
func (m *mockWebSession) SessionID(b string) string { return m.Called(b).String(0) }
func (m *mockWebSession) Verify(ctx context.Context) (bool, error) {
	args := m.Called(ctx)
	return args.Bool(0), args.Error(1)
}

func (m *mockWebSession) Authenticate(ctx context.Context, p pb.EAuthTokenPlatformType, r, a string) error {
	return m.Called(ctx, p, r, a).Error(0)
}
func (m *mockWebSession) IsAuthenticated() bool { return m.Called().Bool(0) }

type mockCommunity struct{ mock.Mock }

func (m *mockCommunity) Request(
	ctx context.Context,
	method, path string,
	body any,
	query any,
	mods ...rest.RequestModifier,
) (*http.Response, error) {
	args := m.Called(ctx, method, path, body, query, mods)
	return args.Get(0).(*http.Response), args.Error(1)
}
func (m *mockCommunity) SessionID(baseURL string) string { return m.Called(baseURL).String(0) }
func (m *mockCommunity) GetOrRegisterAPIKey(ctx context.Context, domain string) (string, error) {
	args := m.Called(ctx, domain)
	return args.String(0), args.Error(1)
}

type mockModule struct{ mock.Mock }

func (m *mockModule) Name() string                      { return m.Called().String(0) }
func (m *mockModule) Init(ctx module.InitContext) error { return m.Called(ctx).Error(0) }
func (m *mockModule) Start(ctx context.Context) error   { return m.Called(ctx).Error(0) }
func (m *mockModule) Close() error                      { return m.Called().Error(0) }

type mockAuthModule struct{ mockModule }

func (m *mockAuthModule) StartAuthed(ctx context.Context, actx module.AuthContext) error {
	return m.Called(ctx, actx).Error(0)
}

type mockTarget struct{ path string }

func (m *mockTarget) String() string     { return "mock" }
func (m *mockTarget) HTTPPath() string   { return m.path }
func (m *mockTarget) HTTPMethod() string { return "GET" }

type mockSocketTarget struct{ path string }

func (m *mockSocketTarget) String() string              { return "mock_socket" }
func (m *mockSocketTarget) HTTPPath() string            { return m.path }
func (m *mockSocketTarget) HTTPMethod() string          { return "POST" }
func (m *mockSocketTarget) EMsg(isAuth bool) enums.EMsg { return enums.EMsg_ClientHeartBeat }
func (m *mockSocketTarget) ObjectName() string          { return "obj" }

type mockSessionRefresher struct{ mock.Mock }

func (m *mockSessionRefresher) Refresh(ctx context.Context) error { return m.Called(ctx).Error(0) }
func (m *mockSessionRefresher) Clients() (*service.Client, *service.Client) {
	args := m.Called()
	return args.Get(0).(*service.Client), args.Get(1).(*service.Client)
}

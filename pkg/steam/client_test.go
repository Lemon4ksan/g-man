// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package steam

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"google.golang.org/protobuf/proto"

	pb "github.com/lemon4ksan/g-man/pkg/protobuf/steam"
	"github.com/lemon4ksan/g-man/pkg/rest"
	"github.com/lemon4ksan/g-man/pkg/steam/api"
	"github.com/lemon4ksan/g-man/pkg/steam/auth"
	"github.com/lemon4ksan/g-man/pkg/steam/module"
	"github.com/lemon4ksan/g-man/pkg/steam/protocol/enums"
	"github.com/lemon4ksan/g-man/pkg/steam/service"
	"github.com/lemon4ksan/g-man/pkg/steam/socket"
	"github.com/lemon4ksan/g-man/pkg/steam/socket/session"
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

	c := NewClient(cfg)

	c.socket = m.sock

	testTransport := tr.NewHTTPTransport(m.http, service.WebAPIBase)
	c.unifiedClient = service.New(testTransport)
	c.socketAPIClient = service.New(testTransport)

	c.auth = m.auth
	c.webSession = m.web
	c.community = m.comm

	t.Cleanup(func() { _ = c.Close() })

	return c, m
}

func TestClient_Initialization(t *testing.T) {
	t.Run("Default Storage Assignment", func(t *testing.T) {
		client := NewClient(Config{})
		assert.NotNil(t, client.Storage())
		client.Close()
	})

	t.Run("Module Lifecycle Events", func(t *testing.T) {
		client := NewClient(Config{Storage: memory.New()})
		m := new(mockModule)
		m.On("Name").Return("test_mod")
		m.On("Init", mock.Anything).Return(nil).Once()
		m.On("Start", mock.Anything).Return(nil).Once()

		client.RegisterModule(m)

		err := m.Init(client)
		assert.NoError(t, err)

		assert.Eventually(t, func() bool {
			return client.State() == StateRunning
		}, time.Second, 10*time.Millisecond)
	})
}

func TestClient_LifecycleState(t *testing.T) {
	client := NewClient(Config{Storage: memory.New()})
	defer client.Close()

	assert.Eventually(t, func() bool {
		return client.State() == StateRunning
	}, time.Second, 10*time.Millisecond)

	assert.Equal(t, "running", client.State().String())

	client.Close()
	assert.Equal(t, StateClosed, client.State())
	assert.ErrorIs(t, client.ConnectAndLogin(context.Background(), socket.CMServer{}, nil), module.ErrClientClosed)
}

func TestClient_Do_TransportSelection(t *testing.T) {
	c, m := setupTestClient(t)
	ctx := context.Background()

	t.Run("Fallback to HTTP when Socket Disconnected", func(t *testing.T) {
		m.sock.On("IsConnected").Return(false)
		m.http.On("Do", mock.Anything).Return(&http.Response{
			StatusCode: 200,
			Body:       io.NopCloser(bytes.NewBufferString(`{}`)),
		}, nil).Once()

		req := tr.NewRequest(&mockSocketTarget{path: "p"}, nil)
		_, err := c.Do(ctx, req)
		assert.NoError(t, err)
		m.http.AssertExpectations(t)
	})

	t.Run("Silent Refresh on Session Expired", func(t *testing.T) {
		m.web.On("Verify", mock.Anything).Return(false, nil).Once()

		sess := &session.Session{}
		sess.SetRefreshToken("rt")
		sess.SetSteamID(12345)
		m.sock.On("Session").Return(sess).Maybe()

		m.http.On("Do", mock.MatchedBy(func(r *http.Request) bool {
			return r.URL.Path == "/mock_path"
		})).Return(nil, api.ErrSessionExpired).Once()

		tokenPb, _ := proto.Marshal(&pb.CAuthentication_AccessToken_GenerateForApp_Response{
			AccessToken: proto.String("new_at"),
		})
		m.http.On("Do", mock.MatchedBy(func(r *http.Request) bool {
			return r.URL.Path == "/IAuthenticationService/GenerateAccessTokenForApp/v1"
		})).Return(&http.Response{
			StatusCode: 200,
			Header:     http.Header{"x-eresult": {"1"}},
			Body:       io.NopCloser(bytes.NewBuffer(tokenPb)),
		}, nil).Once()

		m.web.On("Authenticate", mock.Anything, mock.Anything, "rt", "new_at").Return(nil).Once()

		m.http.On("Do", mock.MatchedBy(func(r *http.Request) bool {
			return r.URL.Path == "/mock_path"
		})).Return(&http.Response{
			StatusCode: 200,
			Body:       io.NopCloser(bytes.NewBufferString(`{"ok":true}`)),
		}, nil).Once()

		target := &mockTarget{path: "mock_path"}
		_, err := c.Do(ctx, tr.NewRequest(target, nil))

		assert.NoError(t, err)
		m.http.AssertExpectations(t)
		m.web.AssertExpectations(t)
	})
}

func TestClient_ConnectAndLogin(t *testing.T) {
	c, m := setupTestClient(t)

	server := socket.CMServer{Endpoint: "cm1.steam.com", Type: "tcp"}
	details := &auth.LogOnDetails{SteamID: 12345}

	m.auth.On("LogOn", mock.Anything, details, server).Return(nil).Once()

	m.web.On("Verify", mock.Anything).Return(true, nil)
	m.comm.On("GetOrRegisterAPIKey", mock.Anything, "g-man-bot.dev").Return("key_123", nil)

	err := c.ConnectAndLogin(context.Background(), server, details)
	assert.NoError(t, err)

	assert.Eventually(t, func() bool {
		return c.unifiedClient.APIKey() == "key_123"
	}, time.Second, 10*time.Millisecond)
}

type mockHTTPDoer struct{ mock.Mock }

func (m *mockHTTPDoer) Do(req *http.Request) (*http.Response, error) {
	args := m.Called(req)
	resp, _ := args.Get(0).(*http.Response)
	return resp, args.Error(1)
}

type mockSocket struct{ mock.Mock }

func (m *mockSocket) IsConnected() bool { return m.Called().Bool(0) }
func (m *mockSocket) Session() socket.Session {
	res := m.Called().Get(0)
	if res == nil {
		return nil
	}

	return res.(socket.Session)
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
	body []byte,
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

type mockTarget struct {
	path string
}

func (m *mockTarget) String() string     { return "mock" }
func (m *mockTarget) HTTPPath() string   { return m.path }
func (m *mockTarget) HTTPMethod() string { return "GET" }

type mockSocketTarget struct {
	path string
}

func (m *mockSocketTarget) String() string              { return "mock_socket" }
func (m *mockSocketTarget) HTTPPath() string            { return m.path }
func (m *mockSocketTarget) HTTPMethod() string          { return "POST" }
func (m *mockSocketTarget) EMsg(isAuth bool) enums.EMsg { return enums.EMsg_ClientHeartBeat }
func (m *mockSocketTarget) ObjectName() string          { return "obj" }

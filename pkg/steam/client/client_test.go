// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package client_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"testing"

	"github.com/lemon4ksan/miyako/log"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"

	"github.com/lemon4ksan/g-man/pkg/steam/auth"
	"github.com/lemon4ksan/g-man/pkg/steam/client"
	"github.com/lemon4ksan/g-man/pkg/steam/module"
	"github.com/lemon4ksan/g-man/pkg/steam/protocol"
	"github.com/lemon4ksan/g-man/pkg/steam/protocol/enums"
	"github.com/lemon4ksan/g-man/pkg/steam/socket"
	tr "github.com/lemon4ksan/g-man/pkg/steam/transport"
	steammock "github.com/lemon4ksan/g-man/test/mock"
)

type Authenticator struct {
	mock.Mock
}

func (m *Authenticator) LogOn(ctx context.Context, details *auth.LogOnDetails, server socket.CMServer) error {
	args := m.Called(ctx, details, server)
	return args.Error(0)
}

type mockTarget struct {
	tr.Target
}

func (mockTarget) HTTPPath() string   { return "/test" }
func (mockTarget) HTTPMethod() string { return "GET" }

// newMockModule is a helper to construct a generic mocked Module.
func newMockModule(t *testing.T, name string, initErr, startErr error) *steammock.Module {
	t.Helper()

	mod := new(steammock.Module)
	mod.On("Name").Return(name)
	mod.On("Init", mock.Anything).Return(initErr).Once()

	if initErr == nil {
		mod.On("Start", mock.Anything).Return(startErr).Once()
	}

	return mod
}

// newMockAuthModule is a helper to construct a generic mocked AuthModule.
func newMockAuthModule(t *testing.T, name string, initErr, startErr, startAuthedErr error) *steammock.AuthModule {
	t.Helper()

	mod := new(steammock.AuthModule)
	mod.On("Name").Return(name)
	mod.On("Init", mock.Anything).Return(initErr).Once()

	if initErr == nil {
		mod.On("Start", mock.Anything).Return(startErr).Once()

		if startErr == nil {
			mod.On("StartAuthed", mock.Anything, mock.Anything).Return(startAuthedErr).Once()
		}
	}

	return mod
}

func TestConfig_ResolveDefaults_VariousScenarios_BehavesCorrectly(t *testing.T) {
	t.Parallel()

	t.Run("proxy_url_copied", func(t *testing.T) {
		t.Parallel()

		cfg := client.Config{
			ProxyURL: "http://my-proxy",
		}
		cfg.ResolveDefaults()
		assert.Equal(t, "http://my-proxy", cfg.Socket.Connector.ProxyURL)
	})

	t.Run("proxy_url_not_overwritten", func(t *testing.T) {
		t.Parallel()

		cfg := client.Config{
			ProxyURL: "http://my-proxy",
		}
		cfg.Socket.Connector.ProxyURL = "http://socket-proxy"
		cfg.ResolveDefaults()
		assert.Equal(t, "http://socket-proxy", cfg.Socket.Connector.ProxyURL)
	})
}

func TestClient_LifecycleState_StateTransitions_MatchesExpected(t *testing.T) {
	t.Parallel()

	c, _ := client.New(client.Config{})

	_ = c.Run()

	assert.Equal(t, client.StateRunning, c.State())
	assert.Equal(t, "running", c.State().String())

	c.Close()
	c.Close()

	assert.Equal(t, client.StateClosed, c.State())
	assert.ErrorIs(t, c.ConnectAndLogin(t.Context(), socket.CMServer{}, nil), module.ErrClosed)
}

func TestClient_Initialization_VariousConfigs_InitializesCorrectly(t *testing.T) {
	t.Parallel()

	assert.NotNil(t, client.DefaultConfig().Socket)

	t.Run("default_storage_assignment", func(t *testing.T) {
		t.Parallel()

		c, _ := client.New(client.Config{DisableSocket: true})
		assert.NotNil(t, c.Storage())
		c.Close()
	})

	t.Run("options", func(t *testing.T) {
		t.Parallel()

		l := log.Discard
		mod := newMockModule(t, "opt_mod", nil, nil)

		c, err := client.New(client.Config{DisableSocket: true}, client.WithLogger(l), client.WithModule(mod))
		assert.NoError(t, err)
		assert.Equal(t, c.Logger(), l)
		assert.NotNil(t, c.Module("opt_mod"))
		c.Close()
	})
}

func TestClient_Run_VariousFailures_ReturnsError(t *testing.T) {
	t.Parallel()

	t.Run("init_fails", func(t *testing.T) {
		t.Parallel()
		mod := newMockModule(t, "bad_init", errors.New("init fail"), nil)

		clientObj, err := client.New(client.Config{}, client.WithModule(mod))
		assert.NoError(t, err)
		assert.NotNil(t, clientObj)

		err = clientObj.Run()
		assert.ErrorContains(t, err, "init fail")
	})

	t.Run("start_fails", func(t *testing.T) {
		t.Parallel()
		mod := newMockModule(t, "bad_start", nil, errors.New("start fail"))

		clientObj, err := client.New(client.Config{}, client.WithModule(mod))
		assert.NoError(t, err)
		assert.NotNil(t, clientObj)

		err = clientObj.Run()
		assert.ErrorContains(t, err, "start fail")
	})
}

func TestClient_StateString_VariousStates_ReturnsExpectedString(t *testing.T) {
	t.Parallel()

	tests := []struct {
		state client.State
		want  string
	}{
		{client.StateNew, "new"},
		{client.StateRunning, "running"},
		{client.StateClosed, "closed"},
		{client.State(999), "unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, tt.state.String())
		})
	}
}

func TestClient_SteamID_VariousSessionStates_ReturnsExpectedSteamID(t *testing.T) {
	t.Parallel()

	t.Run("session_exists", func(t *testing.T) {
		t.Parallel()
		c, m := steammock.SetupTestClient(t)

		msess := new(steammock.Session)
		msess.On("SteamID").Return(uint64(123456))
		m.Sock.On("Session").Return(msess).Once()
		assert.Equal(t, uint64(123456), c.Session().SteamID().Uint64())
	})

	t.Run("no_session", func(t *testing.T) {
		t.Parallel()
		c, m := steammock.SetupTestClient(t)

		m.Sock.On("Session").Return(nil).Once()
		assert.Equal(t, uint64(0), c.Session().SteamID().Uint64())
	})
}

func TestClient_Do_StateNotRunning_ReturnsError(t *testing.T) {
	t.Parallel()

	c, _ := steammock.SetupTestClient(t)
	c.ForceState(client.StateClosed)

	_, err := c.Do(t.Context(), tr.NewRequest(&mockTarget{}, nil))
	assert.ErrorIs(t, err, client.ErrNotRunning)
}

func TestClient_Do_VariousStates_BehavesCorrectly(t *testing.T) {
	t.Parallel()

	t.Run("not_running", func(t *testing.T) {
		t.Parallel()

		c, _ := steammock.SetupTestClient(t)
		defer c.Close()

		req := tr.NewRequest(&mockTarget{}, nil)
		_, err := c.Do(t.Context(), req)
		assert.ErrorIs(t, err, client.ErrNotRunning)
	})

	t.Run("running", func(t *testing.T) {
		t.Parallel()

		c, m := steammock.SetupTestClient(t)
		defer c.Close()

		req := tr.NewRequest(&mockTarget{}, nil)

		c.ForceState(client.StateRunning)

		m.Sock.On("IsConnected").Return(false)
		m.Http.On("Do", mock.Anything).Return(&http.Response{StatusCode: 200}, nil).Once()

		resp, err := c.Do(t.Context(), req)
		assert.NoError(t, err)
		assert.NotNil(t, resp)
	})
}

func TestClient_RegisterModule_VariousInputs_RegistersSuccessfully(t *testing.T) {
	t.Parallel()

	c, _ := steammock.SetupTestClient(t)
	defer c.Close()

	c.RegisterModule(nil)

	mod := newMockAuthModule(t, "mod-dup", nil, nil, nil)

	c.RegisterModule(mod)
	c.RegisterModule(mod)
}

func TestClient_DisableSocket_SocketDisabled_ReturnsSocketDisabledError(t *testing.T) {
	t.Parallel()

	cfg := client.Config{
		DisableSocket: true,
	}
	c, err := client.New(cfg)
	assert.NoError(t, err)

	defer c.Close()

	err = c.ConnectAndLogin(t.Context(), socket.CMServer{}, &auth.LogOnDetails{})
	assert.ErrorIs(t, err, client.ErrSocketDisabled)
}

func TestClient_SetPersonaState_ValidState_SetsStateSuccessfully(t *testing.T) {
	t.Parallel()

	c, m := steammock.SetupTestClient(t)
	defer c.Close()

	ctx := t.Context()

	m.Sock.On("SendProto", ctx, enums.EMsg_ClientChangeStatus, mock.Anything, mock.Anything).Return(nil).Once()

	err := c.SetPersonaState(ctx, enums.EPersonaState_Online)
	assert.NoError(t, err)
	assert.Equal(t, enums.EPersonaState_Online, c.PersonaState())
}

func TestClient_ConnectAndLogin_VariousFailures_ReturnsExpectedError(t *testing.T) {
	t.Parallel()

	server := socket.CMServer{}
	details := &auth.LogOnDetails{}

	t.Run("already_closed", func(t *testing.T) {
		t.Parallel()
		c, _ := steammock.SetupTestClient(t)
		c.ForceState(client.StateClosed)

		err := c.ConnectAndLogin(t.Context(), server, details)
		assert.ErrorIs(t, err, module.ErrClosed)
	})

	t.Run("logon_fails", func(t *testing.T) {
		t.Parallel()
		c, m := steammock.SetupTestClient(t)
		c.ForceState(client.StateRunning)

		m.Auth.On("LogOn", mock.Anything, details, server).Return(errors.New("logon fail")).Once()
		err := c.ConnectAndLogin(t.Context(), server, details)
		assert.ErrorContains(t, err, "logon fail")
	})

	t.Run("start_authed_all_fails", func(t *testing.T) {
		t.Parallel()
		c, m := steammock.SetupTestClient(t)
		c.ForceState(client.StateRunning)

		m.Auth.On("LogOn", mock.Anything, details, server).Return(nil).Once()
		m.Web.On("Verify", mock.Anything).Return(true, nil)
		m.Comm.On("GetOrRegisterAPIKey", mock.Anything, mock.Anything).Return("key_123", nil)
		m.Sock.On("SendProto", mock.Anything, enums.EMsg_ClientChangeStatus, mock.Anything, mock.Anything).
			Return(nil).
			Maybe()
		m.Sock.On("Session").Return(nil)

		mod := newMockAuthModule(t, "auth_mod", nil, nil, errors.New("start authed fail"))

		c.RegisterModule(mod)

		err := c.ConnectAndLogin(t.Context(), server, details)
		assert.ErrorContains(t, err, "start authed fail")
	})
}

func TestClient_ConnectAndLogin_EdgeCases_BehavesCorrectly(t *testing.T) {
	t.Parallel()

	server := socket.CMServer{Endpoint: "cm.test"}
	details := &auth.LogOnDetails{AccountName: "acc", SteamID: 123}

	t.Run("details_is_nil", func(t *testing.T) {
		t.Parallel()

		c, _ := steammock.SetupTestClient(t)
		defer c.Close()

		err := c.ConnectAndLogin(t.Context(), server, nil)
		assert.ErrorIs(t, err, client.ErrNilLogOnDetails)
	})

	t.Run("set_persona_state_fails", func(t *testing.T) {
		t.Parallel()

		c, m := steammock.SetupTestClient(t)
		defer c.Close()

		ctx := t.Context()

		c.ForceState(client.StateRunning)
		m.Auth.On("LogOn", ctx, details, server).Return(nil).Once()
		m.Web.On("Verify", mock.Anything).Return(true, nil).Once()
		m.Sock.On("Session").Return(nil)
		m.Sock.On("SendProto", ctx, enums.EMsg_ClientChangeStatus, mock.Anything, mock.Anything).
			Return(errors.New("proto err")).
			Once()

		err := c.ConnectAndLogin(ctx, server, details)
		assert.NoError(t, err)
		assert.Equal(t, client.StateAuthorized, c.State())
	})
}

func TestClient_Reconnect_SuccessfulDiscovery_ReconnectsSuccessfully(t *testing.T) {
	t.Parallel()

	c, m := steammock.SetupTestClient(t)
	defer c.Close()

	ctx := t.Context()

	m.Sock.On("Disconnect").Return(nil).Once()

	m.Http.On("Do", mock.MatchedBy(func(r *http.Request) bool {
		return r.URL.Path == "/ISteamDirectory/GetCMListForConnect/v1" ||
			r.URL.Path == "/ISteamDirectory/GetCMListForConnect/v1/" ||
			r.URL.Path == "/ISteamDirectory/GetCMList/v1" ||
			r.URL.Path == "/ISteamDirectory/GetCMList/v1/"
	})).Return(&http.Response{
		StatusCode: 200,
		Body: io.NopCloser(
			bytes.NewBufferString(`{"response":{"serverlist":["cm1.steampowered.com:27017"],"success":true}}`),
		),
	}, nil).Once()

	details := &auth.LogOnDetails{AccountName: "acc", SteamID: 123}

	c.Session().SetLogonServer(socket.CMServer{Endpoint: "stored.cm"})

	m.Auth.On("LogOn", ctx, details, mock.Anything).Return(nil)
	m.Sock.On("SendProto", ctx, enums.EMsg_ClientChangeStatus, mock.Anything, mock.Anything).Return(nil)

	c.ForceState(client.StateRunning)
	err := c.ConnectAndLogin(ctx, socket.CMServer{Endpoint: "stored.cm"}, details)
	assert.NoError(t, err)

	m.Sock.On("Disconnect").Return(nil).Once()

	err = c.Reconnect(ctx)
	assert.NoError(t, err)
}

func TestClient_Reconnect_DiscoveryFails_CompletesQuietly(t *testing.T) {
	t.Parallel()

	c, m := steammock.SetupTestClient(t)
	defer c.Close()

	ctx := t.Context()

	details := &auth.LogOnDetails{AccountName: "acc", SteamID: 123}
	m.Auth.On("LogOn", ctx, details, mock.Anything).Return(nil)
	m.Sock.On("SendProto", ctx, enums.EMsg_ClientChangeStatus, mock.Anything, mock.Anything).Return(nil)

	c.ForceState(client.StateRunning)
	err := c.ConnectAndLogin(ctx, socket.CMServer{Endpoint: "stored.cm"}, details)
	assert.NoError(t, err)

	m.Sock.On("Disconnect").Return(errors.New("disc err")).Once()
	m.Http.On("Do", mock.Anything).Return(nil, errors.New("http err")).Once()

	err = c.Reconnect(ctx)
	assert.NoError(t, err)
}

func TestClient_Reconnect_LogOnFails_ReturnsReconnectError(t *testing.T) {
	t.Parallel()

	c, m := steammock.SetupTestClient(t)
	defer c.Close()

	ctx := t.Context()

	details := &auth.LogOnDetails{AccountName: "acc", SteamID: 123}
	m.Auth.On("LogOn", ctx, details, mock.Anything).Return(nil).Once()
	m.Sock.On("SendProto", ctx, enums.EMsg_ClientChangeStatus, mock.Anything, mock.Anything).Return(nil)

	c.ForceState(client.StateRunning)
	err := c.ConnectAndLogin(ctx, socket.CMServer{Endpoint: "stored.cm"}, details)
	assert.NoError(t, err)

	m.Sock.On("Disconnect").Return(nil).Once()
	m.Http.On("Do", mock.Anything).Return(nil, errors.New("http err")).Once()

	m.Auth.On("LogOn", ctx, details, mock.Anything).Return(errors.New("logon fail")).Once()

	err = c.Reconnect(ctx)
	assert.ErrorContains(t, err, "reconnect failed")
}

func TestClient_Reconnect_ClientClosed_ReturnsClosedError(t *testing.T) {
	t.Parallel()

	c, _ := steammock.SetupTestClient(t)
	c.ForceState(client.StateClosed)
	err := c.Reconnect(t.Context())
	assert.ErrorIs(t, err, module.ErrClosed)
}

func TestClient_Disconnect_SocketFails_ReturnsError(t *testing.T) {
	t.Parallel()

	c, m := steammock.SetupTestClient(t)
	defer c.Close()

	m.Sock.On("Disconnect").Return(errors.New("disc err")).Once()

	err := c.Disconnect()
	assert.ErrorContains(t, err, "disc err")
}

func TestNoopSocketProvider_VariousMethods_ReturnsDisabledError(t *testing.T) {
	t.Parallel()

	p := client.NoopSocketProvider{}
	ctx := t.Context()

	assert.False(t, p.IsConnected())
	assert.Nil(t, p.Session())
	assert.ErrorIs(t, p.Connect(ctx, socket.CMServer{}), client.ErrSocketDisabled)
	assert.ErrorIs(t, p.LogOn(ctx, nil), client.ErrSocketDisabled)
	assert.False(t, p.SetEncryptionKey(nil))
	assert.ErrorIs(t, p.Send(ctx, nil), client.ErrSocketDisabled)

	pkt, err := p.SendSync(ctx, nil)
	assert.Nil(t, pkt)
	assert.ErrorIs(t, err, client.ErrSocketDisabled)

	assert.ErrorIs(t, p.SendProto(ctx, enums.EMsg_Invalid, nil), client.ErrSocketDisabled)
	assert.ErrorIs(t, p.SendRaw(ctx, enums.EMsg_Invalid, nil), client.ErrSocketDisabled)

	p.RegisterMsgHandler(enums.EMsg_Invalid, nil)
	p.RegisterServiceHandler("", nil)

	assert.ErrorIs(t, p.StartHeartbeat(0), client.ErrSocketDisabled)
	assert.NoError(t, p.Disconnect())
	assert.NoError(t, p.Close())
	p.UpdateLogger(log.Discard)
	p.UpdateServers(nil)
}

func TestInitContext_VariousMethods_DelegatesCorrectly(t *testing.T) {
	t.Parallel()

	c, m := steammock.SetupTestClient(t)
	defer c.Close()

	ctx := &client.InitContext{Client: c}

	assert.Equal(t, c.Storage(), ctx.Storage())
	assert.Equal(t, c.Bus(), ctx.Bus())
	assert.Equal(t, c.Logger(), ctx.Logger())
	assert.Equal(t, c, ctx.Service())
	assert.Equal(t, c.Rest(), ctx.Rest())
	assert.Equal(t, c.Module("test"), ctx.Module("test"))

	m.Sock.On("RegisterMsgHandler", enums.EMsg_ClientLogOnResponse, mock.Anything).Return().Times(2)
	ctx.RegisterPacketHandler(enums.EMsg_ClientLogOnResponse, func(p *protocol.Packet) {})
	ctx.UnregisterPacketHandler(enums.EMsg_ClientLogOnResponse)

	m.Sock.On("RegisterServiceHandler", "method", mock.Anything).Return().Times(2)
	ctx.RegisterServiceHandler("method", func(p *protocol.Packet) {})
	ctx.UnregisterServiceHandler("method")

	m.Sock.AssertExpectations(t)
}

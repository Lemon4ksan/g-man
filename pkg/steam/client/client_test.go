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

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"

	"github.com/lemon4ksan/g-man/pkg/log"
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

func TestConfig_ResolveDefaults(t *testing.T) {
	t.Run("ProxyURL copied", func(t *testing.T) {
		cfg := client.Config{
			ProxyURL: "http://my-proxy",
		}
		cfg.ResolveDefaults()
		assert.Equal(t, "http://my-proxy", cfg.Socket.Connector.ProxyURL)
	})

	t.Run("ProxyURL not overwritten", func(t *testing.T) {
		cfg := client.Config{
			ProxyURL: "http://my-proxy",
		}
		cfg.Socket.Connector.ProxyURL = "http://socket-proxy"
		cfg.ResolveDefaults()
		assert.Equal(t, "http://socket-proxy", cfg.Socket.Connector.ProxyURL)
	})
}

func TestClient_LifecycleState(t *testing.T) {
	c, _ := client.New(client.Config{})

	_ = c.Run()

	assert.Equal(t, client.StateRunning, c.State())
	assert.Equal(t, "running", c.State().String())

	c.Close()
	c.Close()

	assert.Equal(t, client.StateClosed, c.State())
	assert.ErrorIs(t, c.ConnectAndLogin(context.Background(), socket.CMServer{}, nil), module.ErrClosed)
}

func TestClient_Initialization(t *testing.T) {
	assert.NotNil(t, client.DefaultConfig().Socket)

	t.Run("Default Storage Assignment", func(t *testing.T) {
		c, _ := client.New(client.Config{DisableSocket: true})
		assert.NotNil(t, c.Storage())
		c.Close()
	})

	t.Run("Options", func(t *testing.T) {
		l := log.Discard
		mod := new(steammock.Module)
		mod.On("Name").Return("opt_mod")
		mod.On("Init", mock.Anything).Return(nil).Once()
		mod.On("Start", mock.Anything).Return(nil).Once()

		c, err := client.New(client.Config{DisableSocket: true}, client.WithLogger(l), client.WithModule(mod))
		assert.NoError(t, err)
		assert.Equal(t, c.Logger(), l)
		assert.NotNil(t, c.Module("opt_mod"))
		c.Close()
	})
}

func TestClient_RunFailures(t *testing.T) {
	t.Run("Init Fails", func(t *testing.T) {
		mod := new(steammock.Module)
		mod.On("Name").Return("bad_init")
		mod.On("Init", mock.Anything).Return(errors.New("init fail")).Once()

		client, err := client.New(client.Config{}, client.WithModule(mod))
		assert.NoError(t, err)
		assert.NotNil(t, client)

		err = client.Run()
		assert.ErrorContains(t, err, "init fail")
	})

	t.Run("Start Fails", func(t *testing.T) {
		mod := new(steammock.Module)
		mod.On("Name").Return("bad_start")
		mod.On("Init", mock.Anything).Return(nil).Once()
		mod.On("Start", mock.Anything).Return(errors.New("start fail")).Once()

		client, err := client.New(client.Config{}, client.WithModule(mod))
		assert.NoError(t, err)
		assert.NotNil(t, client)

		err = client.Run()
		assert.ErrorContains(t, err, "start fail")
	})
}

func TestClient_StateString(t *testing.T) {
	assert.Equal(t, "new", client.StateNew.String())
	assert.Equal(t, "running", client.StateRunning.String())
	assert.Equal(t, "closed", client.StateClosed.String())
	assert.Equal(t, "unknown", client.State(999).String())
}

func TestClient_SteamID(t *testing.T) {
	c, m := steammock.SetupTestClient(t)

	t.Run("Session exists", func(t *testing.T) {
		msess := new(steammock.Session)
		msess.On("SteamID").Return(uint64(123456))
		m.Sock.On("Session").Return(msess).Once()
		assert.Equal(t, uint64(123456), c.Session().SteamID().Uint64())
	})

	t.Run("No session", func(t *testing.T) {
		m.Sock.On("Session").Return(nil).Once()
		assert.Equal(t, uint64(0), c.Session().SteamID().Uint64())
	})
}

func TestClient_Do_State(t *testing.T) {
	c, _ := steammock.SetupTestClient(t)
	c.ForceState(client.StateClosed)

	_, err := c.Do(context.Background(), tr.NewRequest(&mockTarget{}, nil))
	assert.ErrorIs(t, err, client.ErrNotRunning)
}

func TestClient_Do(t *testing.T) {
	c, m := steammock.SetupTestClient(t)
	defer c.Close()

	req := tr.NewRequest(&mockTarget{}, nil)

	t.Run("Not running", func(t *testing.T) {
		_, err := c.Do(context.Background(), req)
		assert.ErrorIs(t, err, client.ErrNotRunning)
	})

	t.Run("Running", func(t *testing.T) {
		c.ForceState(client.StateRunning)

		m.Sock.On("IsConnected").Return(false)
		m.Http.On("Do", mock.Anything).Return(&http.Response{StatusCode: 200}, nil).Once()

		resp, err := c.Do(context.Background(), req)
		assert.NoError(t, err)
		assert.NotNil(t, resp)
	})
}

func TestClient_RegisterModule(t *testing.T) {
	c, _ := steammock.SetupTestClient(t)
	defer c.Close()

	c.RegisterModule(nil)

	mod := new(steammock.AuthModule)
	mod.On("Name").Return("mod-dup")
	mod.On("Init", mock.Anything).Return(nil).Once()
	mod.On("Start", mock.Anything).Return(nil).Once()

	c.RegisterModule(mod)
	c.RegisterModule(mod)
}

func TestClient_DisableSocket(t *testing.T) {
	cfg := client.Config{
		DisableSocket: true,
	}
	c, err := client.New(cfg)
	assert.NoError(t, err)

	defer c.Close()

	err = c.ConnectAndLogin(context.Background(), socket.CMServer{}, &auth.LogOnDetails{})
	assert.ErrorIs(t, err, client.ErrSocketDisabled)
}

func TestClient_SetPersonaState(t *testing.T) {
	c, m := steammock.SetupTestClient(t)
	defer c.Close()

	ctx := context.Background()

	m.Sock.On("SendProto", ctx, enums.EMsg_ClientChangeStatus, mock.Anything, mock.Anything).Return(nil).Once()

	err := c.SetPersonaState(ctx, enums.EPersonaState_Online)
	assert.NoError(t, err)
	assert.Equal(t, enums.EPersonaState_Online, c.GetPersonaState())
}

func TestClient_ConnectAndLogin_Failures(t *testing.T) {
	c, m := steammock.SetupTestClient(t)
	server := socket.CMServer{}
	details := &auth.LogOnDetails{}

	t.Run("Already Closed", func(t *testing.T) {
		c.ForceState(client.StateClosed)
		err := c.ConnectAndLogin(context.Background(), server, details)
		assert.ErrorIs(t, err, module.ErrClosed)
	})

	c.ForceState(client.StateRunning)

	t.Run("LogOn Fails", func(t *testing.T) {
		m.Auth.On("LogOn", mock.Anything, details, server).Return(errors.New("logon fail")).Once()
		err := c.ConnectAndLogin(context.Background(), server, details)
		assert.ErrorContains(t, err, "logon fail")
	})

	t.Run("StartAuthedAll Fails", func(t *testing.T) {
		m.Auth.On("LogOn", mock.Anything, details, server).Return(nil).Once()
		m.Web.On("Verify", mock.Anything).Return(true, nil)
		m.Comm.On("GetOrRegisterAPIKey", mock.Anything, mock.Anything).Return("key_123", nil)
		m.Sock.On("SendProto", mock.Anything, enums.EMsg_ClientChangeStatus, mock.Anything, mock.Anything).
			Return(nil).
			Maybe()
		m.Sock.On("Session").Return(nil)

		mod := new(steammock.AuthModule)
		mod.On("Name").Return("auth_mod")
		mod.On("Init", mock.Anything).Return(nil).Once()
		mod.On("Start", mock.Anything).Return(nil).Once()
		mod.On("StartAuthed", mock.Anything, mock.Anything).Return(errors.New("start authed fail")).Once()

		c.RegisterModule(mod)

		err := c.ConnectAndLogin(context.Background(), server, details)
		assert.ErrorContains(t, err, "start authed fail")
	})
}

func TestClient_ConnectAndLogin_EdgeCases(t *testing.T) {
	c, m := steammock.SetupTestClient(t)
	defer c.Close()

	ctx := context.Background()
	server := socket.CMServer{Endpoint: "cm.test"}
	details := &auth.LogOnDetails{AccountName: "acc", SteamID: 123}

	t.Run("details is nil", func(t *testing.T) {
		err := c.ConnectAndLogin(ctx, server, nil)
		assert.ErrorIs(t, err, client.ErrNilLogOnDetails)
	})

	t.Run("SetPersonaState fails", func(t *testing.T) {
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

func TestClient_Reconnect_OptimalCMDiscovery_Success(t *testing.T) {
	c, m := steammock.SetupTestClient(t)
	defer c.Close()

	ctx := context.Background()

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

	err := c.ConnectAndLogin(ctx, socket.CMServer{Endpoint: "stored.cm"}, details)
	assert.NoError(t, err)

	m.Sock.On("Disconnect").Return(nil).Once()

	err = c.Reconnect(ctx)
	assert.NoError(t, err)
}

func TestClient_Reconnect_OptimalCMDiscovery_Failure(t *testing.T) {
	c, m := steammock.SetupTestClient(t)
	defer c.Close()

	ctx := context.Background()

	details := &auth.LogOnDetails{AccountName: "acc", SteamID: 123}
	m.Auth.On("LogOn", ctx, details, mock.Anything).Return(nil)
	m.Sock.On("SendProto", ctx, enums.EMsg_ClientChangeStatus, mock.Anything, mock.Anything).Return(nil)

	err := c.ConnectAndLogin(ctx, socket.CMServer{Endpoint: "stored.cm"}, details)
	assert.NoError(t, err)

	m.Sock.On("Disconnect").Return(errors.New("disc err")).Once()
	m.Http.On("Do", mock.Anything).Return(nil, errors.New("http err")).Once()

	err = c.Reconnect(ctx)
	assert.NoError(t, err)
}

func TestClient_Reconnect_ReconnectFailure(t *testing.T) {
	c, m := steammock.SetupTestClient(t)
	defer c.Close()

	ctx := context.Background()

	details := &auth.LogOnDetails{AccountName: "acc", SteamID: 123}
	m.Auth.On("LogOn", ctx, details, mock.Anything).Return(nil).Once()
	m.Sock.On("SendProto", ctx, enums.EMsg_ClientChangeStatus, mock.Anything, mock.Anything).Return(nil)

	err := c.ConnectAndLogin(ctx, socket.CMServer{Endpoint: "stored.cm"}, details)
	assert.NoError(t, err)

	m.Sock.On("Disconnect").Return(nil).Once()
	m.Http.On("Do", mock.Anything).Return(nil, errors.New("http err")).Once()

	m.Auth.On("LogOn", ctx, details, mock.Anything).Return(errors.New("logon fail")).Once()

	err = c.Reconnect(ctx)
	assert.ErrorContains(t, err, "reconnect failed")
}

func TestClient_Reconnect_Closed(t *testing.T) {
	c, _ := steammock.SetupTestClient(t)
	c.ForceState(client.StateClosed)
	err := c.Reconnect(context.Background())
	assert.ErrorIs(t, err, module.ErrClosed)
}

func TestClient_Disconnect(t *testing.T) {
	c, m := steammock.SetupTestClient(t)
	defer c.Close()

	m.Sock.On("Disconnect").Return(errors.New("disc err")).Once()

	err := c.Disconnect()
	assert.ErrorContains(t, err, "disc err")
}

func TestNoopSocketProvider(t *testing.T) {
	p := client.NoopSocketProvider{}
	ctx := context.Background()

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

func TestInitContext(t *testing.T) {
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

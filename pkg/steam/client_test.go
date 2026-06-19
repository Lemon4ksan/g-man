// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package steam

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"

	"github.com/lemon4ksan/g-man/pkg/log"
	"github.com/lemon4ksan/g-man/pkg/steam/auth"
	"github.com/lemon4ksan/g-man/pkg/steam/module"
	"github.com/lemon4ksan/g-man/pkg/steam/protocol"
	"github.com/lemon4ksan/g-man/pkg/steam/protocol/enums"
	"github.com/lemon4ksan/g-man/pkg/steam/socket"
	tr "github.com/lemon4ksan/g-man/pkg/steam/transport"
)

func TestClient_LifecycleState(t *testing.T) {
	client, _ := NewClient(Config{})

	_ = client.Run()

	assert.Equal(t, StateRunning, client.State())
	assert.Equal(t, "running", client.State().String())

	client.Close() // Idempotent close
	client.Close()

	assert.Equal(t, StateClosed, client.State())
	assert.ErrorIs(t, client.ConnectAndLogin(context.Background(), socket.CMServer{}, nil), module.ErrClosed)
}

func TestClient_Initialization(t *testing.T) {
	assert.NotNil(t, DefaultConfig().Socket)

	t.Run("Default Storage Assignment", func(t *testing.T) {
		client, _ := NewClient(Config{})
		assert.NotNil(t, client.Storage())
		client.Close()
	})

	t.Run("Options", func(t *testing.T) {
		l := log.Discard
		mod := new(mockModule)
		mod.On("Name").Return("opt_mod")
		mod.On("Init", mock.Anything).Return(nil).Once()
		mod.On("Start", mock.Anything).Return(nil).Once()

		client, err := NewClient(Config{}, WithLogger(l), WithModule(mod))
		assert.NoError(t, err)
		assert.Equal(t, client.Logger(), l)
		assert.NotNil(t, client.Module("opt_mod"))
		client.Close()
	})
}

func TestClient_RunFailures(t *testing.T) {
	t.Run("Init Fails", func(t *testing.T) {
		mod := new(mockModule)
		mod.On("Name").Return("bad_init")
		mod.On("Init", mock.Anything).Return(errors.New("init fail")).Once()

		client, err := NewClient(Config{}, WithModule(mod))
		assert.NoError(t, err)
		assert.NotNil(t, client)

		err = client.Run()
		assert.ErrorContains(t, err, "init fail")
	})

	t.Run("Start Fails", func(t *testing.T) {
		mod := new(mockModule)
		mod.On("Name").Return("bad_start")
		mod.On("Init", mock.Anything).Return(nil).Once()
		mod.On("Start", mock.Anything).Return(errors.New("start fail")).Once()

		client, err := NewClient(Config{}, WithModule(mod))
		assert.NoError(t, err)
		assert.NotNil(t, client)

		err = client.Run()
		assert.ErrorContains(t, err, "start fail")
	})
}

func TestClient_StateString(t *testing.T) {
	assert.Equal(t, "new", StateNew.String())
	assert.Equal(t, "running", StateRunning.String())
	assert.Equal(t, "closed", StateClosed.String())
	assert.Equal(t, "unknown", State(999).String())
}

func TestClient_GettersAndHandlers(t *testing.T) {
	c, m := setupTestClient(t)

	assert.NotNil(t, c.Bus())
	assert.NotNil(t, c.Rest())
	assert.NotNil(t, c.Service())
	assert.Equal(t, c.Socket(), m.sock)

	// Register/Unregister Packets
	m.sock.On("RegisterMsgHandler", enums.EMsg_ClientLogOnResponse, mock.Anything).Return().Times(2)
	c.RegisterPacketHandler(enums.EMsg_ClientLogOnResponse, func(p *protocol.Packet) {})
	c.UnregisterPacketHandler(enums.EMsg_ClientLogOnResponse)

	// Register/Unregister Services
	m.sock.On("RegisterServiceHandler", "Player.GetGameBadgeLevels#1", mock.Anything).Return().Times(2)
	c.RegisterServiceHandler("Player.GetGameBadgeLevels#1", func(p *protocol.Packet) {})
	c.UnregisterServiceHandler("Player.GetGameBadgeLevels#1")

	m.sock.AssertExpectations(t)
}

func TestClient_SteamID(t *testing.T) {
	c, m := setupTestClient(t)

	t.Run("Session exists", func(t *testing.T) {
		msess := new(mockSession)
		msess.On("SteamID").Return(uint64(123456))
		m.sock.On("Session").Return(msess).Once()
		assert.Equal(t, uint64(123456), uint64(c.SteamID()))
	})

	t.Run("No session", func(t *testing.T) {
		m.sock.On("Session").Return(nil).Once()
		assert.Equal(t, uint64(0), uint64(c.SteamID()))
	})
}

func TestClient_Do_State(t *testing.T) {
	c, _ := setupTestClient(t)
	c.fsm.ForceSet(StateClosed)

	_, err := c.Do(context.Background(), tr.NewRequest(&mockTarget{}, nil))
	assert.ErrorIs(t, err, ErrNotRunning)
}

func TestClient_ConnectAndLogin_Failures(t *testing.T) {
	c, m := setupTestClient(t)
	server := socket.CMServer{}
	details := &auth.LogOnDetails{}

	t.Run("Already Closed", func(t *testing.T) {
		c.fsm.ForceSet(StateClosed)
		err := c.ConnectAndLogin(context.Background(), server, details)
		assert.ErrorIs(t, err, module.ErrClosed)
	})

	c.fsm.ForceSet(StateRunning)

	t.Run("LogOn Fails", func(t *testing.T) {
		m.auth.On("LogOn", mock.Anything, details, server).Return(errors.New("logon fail")).Once()
		err := c.ConnectAndLogin(context.Background(), server, details)
		assert.ErrorContains(t, err, "logon fail")
	})

	t.Run("StartAuthedAll Fails", func(t *testing.T) {
		m.auth.On("LogOn", mock.Anything, details, server).Return(nil).Once()
		m.web.On("Verify", mock.Anything).Return(true, nil)
		m.comm.On("GetOrRegisterAPIKey", mock.Anything, mock.Anything).Return("key_123", nil)

		mod := new(mockAuthModule)
		mod.On("Name").Return("auth_mod")
		mod.On("Init", mock.Anything).Return(nil).Once()
		mod.On("Start", mock.Anything).Return(nil).Once()
		mod.On("StartAuthed", mock.Anything, mock.Anything).Return(errors.New("start authed fail")).Once()

		c.RegisterModule(mod)

		err := c.ConnectAndLogin(context.Background(), server, details)
		assert.ErrorContains(t, err, "start authed fail")
	})
}

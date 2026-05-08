// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package connector_test

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/lemon4ksan/g-man/pkg/bus"
	"github.com/lemon4ksan/g-man/pkg/log"
	"github.com/lemon4ksan/g-man/pkg/steam/socket/connector"
	"github.com/lemon4ksan/g-man/pkg/steam/socket/network"
)

type mockConnection struct {
	network.BaseConnection
	sendFunc   func(ctx context.Context, data []byte) error
	closeFunc  func() error
	setKeyFunc func(key []byte) bool
}

func (m *mockConnection) Send(ctx context.Context, data []byte) error { return m.sendFunc(ctx, data) }
func (m *mockConnection) Close() error                                { return m.closeFunc() }
func (m *mockConnection) Name() string                                { return "mock" }
func (m *mockConnection) SetEncryptionKey(key []byte) bool            { return m.setKeyFunc(key) }

type mockDataHandler struct {
	lastMsg atomic.Pointer[network.NetMessage]
}

func (m *mockDataHandler) OnNetMessage(msg network.NetMessage) { m.lastMsg.Store(&msg) }
func (m *mockDataHandler) OnNetError(err error)                {}
func (m *mockDataHandler) OnNetClose()                         {}

func TestConnector_Initialization(t *testing.T) {
	dh := &mockDataHandler{}

	c := connector.New(context.Background(), connector.DefaultConfig(), nil,
		connector.WithLogger(log.Discard),
		connector.WithDataHandler(dh),
	)
	defer c.Close()

	assert.NotNil(t, c)
}

func TestConnector_Connect(t *testing.T) {
	t.Run("Successful Connection", func(t *testing.T) {
		b := bus.New()
		sub := b.Subscribe(connector.ConnectedEvent{})

		conn := &mockConnection{
			closeFunc: func() error { return nil },
		}

		dialers := map[string]connector.Dialer{
			"mock": func(ctx context.Context, nh network.Handler, l log.Logger, ep string) (network.Connection, error) {
				return conn, nil
			},
		}

		cfg := connector.DefaultConfig()
		cfg.Dialers = dialers

		c := connector.New(context.Background(), cfg, b)

		err := c.Connect(context.Background(), connector.CMServer{Type: "mock", Endpoint: "localhost"})
		assert.NoError(t, err)

		// Check event
		select {
		case ev := <-sub.C():
			assert.Equal(t, "localhost", ev.(*connector.ConnectedEvent).Server)
		case <-time.After(100 * time.Millisecond):
			t.Fatal("ConnectedEvent not received")
		}

		// Re-connect should close previous
		closed := atomic.Bool{}
		conn.closeFunc = func() error { closed.Store(true); return nil }

		err = c.Connect(context.Background(), connector.CMServer{Type: "mock", Endpoint: "localhost:2"})
		assert.NoError(t, err)
		assert.True(t, closed.Load())
	})

	t.Run("Unsupported Type", func(t *testing.T) {
		c := connector.New(context.Background(), connector.DefaultConfig(), nil)
		err := c.Connect(context.Background(), connector.CMServer{Type: "invalid"})
		assert.ErrorIs(t, err, connector.ErrUnsupportedType)
	})

	t.Run("Dialer Error", func(t *testing.T) {
		dialers := map[string]connector.Dialer{
			"fail": func(ctx context.Context, nh network.Handler, l log.Logger, ep string) (network.Connection, error) {
				return nil, errors.New("dial failed")
			},
		}
		cfg := connector.DefaultConfig()
		cfg.Dialers = dialers
		c := connector.New(context.Background(), cfg, nil)

		err := c.Connect(context.Background(), connector.CMServer{Type: "fail"})
		assert.ErrorContains(t, err, "dial failed")
	})

	t.Run("Concurrent Connection Attempt", func(t *testing.T) {
		start := make(chan struct{})
		dialers := map[string]connector.Dialer{
			"slow": func(ctx context.Context, nh network.Handler, l log.Logger, ep string) (network.Connection, error) {
				<-start
				return &mockConnection{closeFunc: func() error { return nil }}, nil
			},
		}
		cfg := connector.DefaultConfig()
		cfg.Dialers = dialers
		c := connector.New(context.Background(), cfg, nil)

		go func() { _ = c.Connect(context.Background(), connector.CMServer{Type: "slow"}) }()

		time.Sleep(20 * time.Millisecond) // Let it enter the dialer

		err := c.Connect(context.Background(), connector.CMServer{Type: "slow"})
		assert.ErrorIs(t, err, connector.ErrAlreadyConnecting)

		close(start) // Cleanup
	})
}

func TestConnector_Send(t *testing.T) {
	c := connector.New(context.Background(), connector.DefaultConfig(), nil)

	// Send when disconnected
	err := c.Send(context.Background(), []byte("hi"))
	assert.ErrorIs(t, err, connector.ErrDisconnected)

	// Send when connected
	sent := atomic.Bool{}
	conn := &mockConnection{
		sendFunc: func(ctx context.Context, data []byte) error {
			sent.Store(true)
			return nil
		},
	}
	// Inject connection manually for testing
	dialers := map[string]connector.Dialer{
		"mock": func(ctx context.Context, nh network.Handler, l log.Logger, ep string) (network.Connection, error) {
			return conn, nil
		},
	}
	cfg := connector.DefaultConfig()
	cfg.Dialers = dialers
	c = connector.New(context.Background(), cfg, nil)
	_ = c.Connect(context.Background(), connector.CMServer{Type: "mock"})

	err = c.Send(context.Background(), []byte("payload"))
	assert.NoError(t, err)
	assert.True(t, sent.Load())
}

func TestConnector_Encryption(t *testing.T) {
	keyReceived := atomic.Pointer[[]byte]{}
	conn := &mockConnection{
		setKeyFunc: func(key []byte) bool {
			keyReceived.Store(&key)
			return true
		},
	}

	dialers := map[string]connector.Dialer{
		"mock": func(ctx context.Context, nh network.Handler, l log.Logger, ep string) (network.Connection, error) {
			return conn, nil
		},
	}
	cfg := connector.DefaultConfig()
	cfg.Dialers = dialers
	c := connector.New(context.Background(), cfg, nil)
	_ = c.Connect(context.Background(), connector.CMServer{Type: "mock"})

	ok := c.SetEncryptionKey([]byte("secret"))
	assert.True(t, ok)
	assert.Equal(t, []byte("secret"), *keyReceived.Load())
}

func TestConnector_NetworkHandlers(t *testing.T) {
	dh := &mockDataHandler{}
	b := bus.New()
	c := connector.New(context.Background(), connector.DefaultConfig(), b, connector.WithDataHandler(dh))

	// OnNetMessage
	c.OnNetMessage([]byte("data"))
	assert.Equal(t, network.NetMessage("data"), *dh.lastMsg.Load())

	// OnNetError (should just log, no crash)
	assert.NotPanics(t, func() { c.OnNetError(errors.New("some error")) })

	// OnNetClose should trigger reconnect
	sub := b.Subscribe(connector.DisconnectedEvent{})

	c.OnNetClose()

	select {
	case <-sub.C():
		// success
	case <-time.After(100 * time.Millisecond):
		t.Fatal("DisconnectedEvent not emitted")
	}
}

func TestConnector_ReconnectLoop(t *testing.T) {
	t.Run("Exhaust Attempts", func(t *testing.T) {
		b := bus.New()
		dialCount := atomic.Int32{}

		dialers := map[string]connector.Dialer{
			"fail": func(ctx context.Context, nh network.Handler, l log.Logger, ep string) (network.Connection, error) {
				dialCount.Add(1)
				return nil, errors.New("perma-fail")
			},
		}

		policy := connector.DefaultReconnectPolicy()
		policy.MaxAttempts = 2
		policy.InitialBackoff = time.Millisecond
		policy.BackoffFactor = 1.0

		cfg := connector.Config{
			Dialers:         dialers,
			ReconnectPolicy: policy,
			ConnectTimeout:  time.Second,
		}

		c := connector.New(context.Background(), cfg, b)
		c.UpdateServers([]connector.CMServer{{Type: "fail", Endpoint: "ep1"}})

		// Initial connect to set "lastServer"
		_ = c.Connect(context.Background(), connector.CMServer{Type: "fail", Endpoint: "ep1"})

		sub := b.Subscribe(connector.DisconnectedEvent{})

		// Trigger reconnect
		c.OnNetClose()

		// Wait for permanant failure event
		timeout := time.After(200 * time.Millisecond)
		for {
			select {
			case ev := <-sub.C():
				if ev.(*connector.DisconnectedEvent).Error != nil {
					assert.Equal(t, int32(3), dialCount.Load()) // 1 (initial) + 2 (retries)
					return
				}
			case <-timeout:
				t.Fatal("Permanent DisconnectedEvent not received")
			}
		}
	})

	t.Run("Context Cancel During Backoff", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cfg := connector.DefaultConfig()
		cfg.ReconnectPolicy.InitialBackoff = time.Hour // Long sleep

		c := connector.New(ctx, cfg, nil)
		c.UpdateServers([]connector.CMServer{{Type: "tcp", Endpoint: "localhost"}})

		go c.OnNetClose() // Starts loop

		time.Sleep(50 * time.Millisecond)
		cancel() // Abort loop

		// If we are here and test didn't hang, select case <-c.ctx.Done() worked
	})
}

func TestConnector_Lifecycle(t *testing.T) {
	c := connector.New(context.Background(), connector.DefaultConfig(), nil)

	// Close should be idempotent
	err := c.Close()
	assert.NoError(t, err)
	err = c.Close()
	assert.NoError(t, err)

	// Send after close
	err = c.Send(context.Background(), []byte("fail"))
	assert.ErrorIs(t, err, connector.ErrClosed)
}

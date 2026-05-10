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

func TestConnector_Initialization(t *testing.T) {
	c := connector.New(connector.DefaultConfig(), log.Discard)
	defer c.Close()

	assert.NotNil(t, c)
}

func TestConnector_Connect(t *testing.T) {
	t.Run("Successful Connection", func(t *testing.T) {
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

		c := connector.New(cfg, log.Discard)

		err := c.Connect(context.Background(), connector.CMServer{Type: "mock", Endpoint: "localhost"})
		assert.NoError(t, err)

		// Re-connect should close previous
		closed := atomic.Bool{}
		conn.closeFunc = func() error { closed.Store(true); return nil }

		err = c.Connect(context.Background(), connector.CMServer{Type: "mock", Endpoint: "localhost:2"})
		assert.NoError(t, err)
		assert.True(t, closed.Load())
	})

	t.Run("Unsupported Type", func(t *testing.T) {
		c := connector.New(connector.DefaultConfig(), log.Discard)
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
		c := connector.New(cfg, log.Discard)

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
		c := connector.New(cfg, log.Discard)

		go func() { _ = c.Connect(context.Background(), connector.CMServer{Type: "slow"}) }()

		time.Sleep(20 * time.Millisecond) // Let it enter the dialer

		err := c.Connect(context.Background(), connector.CMServer{Type: "slow"})
		assert.ErrorIs(t, err, connector.ErrAlreadyConnecting)

		close(start) // Cleanup
	})
}

func TestConnector_Send(t *testing.T) {
	c := connector.New(connector.DefaultConfig(), log.Discard)

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
	c = connector.New(cfg, log.Discard)
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
	c := connector.New(cfg, log.Discard)
	_ = c.Connect(context.Background(), connector.CMServer{Type: "mock"})

	ok := c.SetEncryptionKey([]byte("secret"))
	assert.True(t, ok)
	assert.Equal(t, []byte("secret"), *keyReceived.Load())
}

func TestConnector_ReconnectLoop(t *testing.T) {
	t.Run("Exhaust Attempts", func(t *testing.T) {
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

		c := connector.New(cfg, log.Discard)
		c.UpdateServers([]connector.CMServer{{Type: "fail", Endpoint: "ep1"}})

		// Initial connect to set "lastServer"
		_ = c.Connect(context.Background(), connector.CMServer{Type: "fail", Endpoint: "ep1"})

		// Trigger reconnect
		c.OnNetClose()
	})
}

func TestConnector_Lifecycle(t *testing.T) {
	c := connector.New(connector.DefaultConfig(), log.Discard)

	// Close should be idempotent
	err := c.Close()
	assert.NoError(t, err)
	err = c.Close()
	assert.NoError(t, err)

	// Send after close
	err = c.Send(context.Background(), []byte("fail"))
	assert.ErrorIs(t, err, connector.ErrClosed)
}

// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package connector

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lemon4ksan/g-man/pkg/log"
	"github.com/lemon4ksan/g-man/pkg/network"
)

type MockConnection struct {
	id       int64
	name     string
	messages chan network.Message
	errors   chan error
	closed   chan struct{}
	sent     chan []byte
	closeErr error
	sendErr  error
}

func (m *MockConnection) ID() int64                        { return m.id }
func (m *MockConnection) Name() string                     { return m.name }
func (m *MockConnection) Messages() <-chan network.Message { return m.messages }
func (m *MockConnection) Errors() <-chan error             { return m.errors }
func (m *MockConnection) Closed() <-chan struct{}          { return m.closed }

func (m *MockConnection) Close() error {
	close(m.closed)
	return m.closeErr
}

func (m *MockConnection) Send(ctx context.Context, data []byte) error {
	if m.sendErr != nil {
		return m.sendErr
	}

	select {
	case m.sent <- data:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

type MockEncryptableConn struct {
	MockConnection
	Cipher any
}

func (m *MockEncryptableConn) SetCipher(cipher network.Cipher) bool {
	m.Cipher = cipher
	return true
}

func TestConnectorError(t *testing.T) {
	t.Parallel()

	err := &connectorError{msg: "test_error", retriable: true}
	assert.Equal(t, "test_error", err.Error())
	assert.True(t, err.IsRetriable())
}

func TestConfig_Defaults(t *testing.T) {
	t.Parallel()

	cfg := DefaultConfig()
	assert.NotNil(t, cfg.Dialers)
	assert.NotNil(t, cfg.ReconnectPolicy)
	assert.Equal(t, 20*time.Second, cfg.ConnectTimeout)

	policy := DefaultReconnectPolicy()
	assert.Equal(t, 0, policy.MaxAttempts)
	assert.Equal(t, 1*time.Second, policy.InitialBackoff)
	assert.Equal(t, 30*time.Second, policy.MaxBackoff)
	assert.Equal(t, 2.0, policy.BackoffFactor)
	assert.NotNil(t, policy.ServerSelector)

	servers := []CMServer{{Endpoint: "a"}, {Endpoint: "b"}}
	selected := policy.ServerSelector(servers)
	assert.NotEmpty(t, selected.Endpoint)

	selectedEmpty := policy.ServerSelector(nil)
	assert.Empty(t, selectedEmpty.Endpoint)

	dialers := DefaultDialers()
	assert.NotNil(t, dialers["tcp"])
	assert.NotNil(t, dialers["netfilter"])
	assert.NotNil(t, dialers["websockets"])
}

func TestConnector_Lifecycle(t *testing.T) {
	t.Parallel()

	t.Run("new_lifecycle", func(t *testing.T) {
		t.Parallel()

		c := New(DefaultConfig(), log.Discard)
		assert.NotNil(t, c.Done())
		assert.NotNil(t, c.C())
		assert.False(t, c.IsConnected())

		c.UpdateLogger(log.Discard)

		err := c.Close()
		assert.NoError(t, err)
		assert.True(t, c.closed.Load())
	})
}

func TestConnector_Connect(t *testing.T) {
	t.Parallel()

	t.Run("unsupported_protocol", func(t *testing.T) {
		t.Parallel()

		c := New(DefaultConfig(), log.Discard)
		err := c.Connect(t.Context(), CMServer{Type: "udp", Endpoint: "1.1.1.1"})
		assert.ErrorIs(t, err, ErrUnsupportedType)
	})

	t.Run("already_connecting", func(t *testing.T) {
		t.Parallel()

		c := New(DefaultConfig(), log.Discard)
		c.isConnecting.Store(true)

		err := c.Connect(t.Context(), CMServer{Type: "tcp", Endpoint: "1.1.1.1"})
		assert.ErrorIs(t, err, ErrAlreadyConnecting)
	})

	t.Run("dialer_failure", func(t *testing.T) {
		t.Parallel()

		cfg := DefaultConfig()
		cfg.Dialers["tcp"] = func(ctx context.Context, logger log.Logger, endpoint, proxyURL string, headers http.Header) (network.Connection, error) {
			return nil, errors.New("dial fail")
		}

		c := New(cfg, log.Discard)
		err := c.Connect(t.Context(), CMServer{Type: "tcp", Endpoint: "1.1.1.1"})
		assert.ErrorContains(t, err, "dial fail")
	})

	t.Run("success", func(t *testing.T) {
		t.Parallel()

		cfg := DefaultConfig()
		mockConn := &MockEncryptableConn{
			MockConnection: MockConnection{
				id:       1,
				name:     "tcp",
				messages: make(chan network.Message, 1),
				errors:   make(chan error, 1),
				closed:   make(chan struct{}),
				sent:     make(chan []byte, 1),
			},
		}
		cfg.Dialers["tcp"] = func(ctx context.Context, logger log.Logger, endpoint, proxyURL string, headers http.Header) (network.Connection, error) {
			return mockConn, nil
		}

		c := New(cfg, log.Discard)
		err := c.Connect(t.Context(), CMServer{Type: "tcp", Endpoint: "1.1.1.1"})
		require.NoError(t, err)
		assert.True(t, c.IsConnected())

		err = c.Close()
		assert.NoError(t, err)
	})
}

func TestConnector_Send(t *testing.T) {
	t.Parallel()

	t.Run("send_on_closed", func(t *testing.T) {
		t.Parallel()

		c := New(DefaultConfig(), log.Discard)
		_ = c.Close()

		err := c.Send(t.Context(), []byte("data"))
		assert.ErrorIs(t, err, ErrClosed)
	})

	t.Run("send_on_disconnected", func(t *testing.T) {
		t.Parallel()

		c := New(DefaultConfig(), log.Discard)
		err := c.Send(t.Context(), []byte("data"))
		assert.ErrorIs(t, err, ErrDisconnected)
	})

	t.Run("send_success", func(t *testing.T) {
		t.Parallel()

		cfg := DefaultConfig()
		mockConn := &MockConnection{
			id:       1,
			name:     "tcp",
			messages: make(chan network.Message, 1),
			errors:   make(chan error, 1),
			closed:   make(chan struct{}),
			sent:     make(chan []byte, 1),
		}
		cfg.Dialers["tcp"] = func(ctx context.Context, logger log.Logger, endpoint, proxyURL string, headers http.Header) (network.Connection, error) {
			return mockConn, nil
		}

		c := New(cfg, log.Discard)
		err := c.Connect(t.Context(), CMServer{Type: "tcp", Endpoint: "1.1.1.1"})
		require.NoError(t, err)

		err = c.Send(t.Context(), []byte("hi"))
		assert.NoError(t, err)
		assert.Equal(t, []byte("hi"), <-mockConn.sent)
	})
}

func TestConnector_Encryption(t *testing.T) {
	t.Parallel()

	t.Run("non_encryptable", func(t *testing.T) {
		t.Parallel()

		cfg := DefaultConfig()
		mockConn := &MockConnection{}
		cfg.Dialers["tcp"] = func(ctx context.Context, logger log.Logger, endpoint, proxyURL string, headers http.Header) (network.Connection, error) {
			return mockConn, nil
		}

		c := New(cfg, log.Discard)
		err := c.Connect(t.Context(), CMServer{Type: "tcp", Endpoint: "1.1.1.1"})
		require.NoError(t, err)

		ok := c.SetEncryptionKey([]byte("secret"))
		assert.False(t, ok)
	})

	t.Run("encryptable", func(t *testing.T) {
		t.Parallel()

		cfg := DefaultConfig()
		mockConn := &MockEncryptableConn{
			MockConnection: MockConnection{
				id:       1,
				name:     "tcp",
				messages: make(chan network.Message, 1),
				errors:   make(chan error, 1),
				closed:   make(chan struct{}),
				sent:     make(chan []byte, 1),
			},
		}
		cfg.Dialers["tcp"] = func(ctx context.Context, logger log.Logger, endpoint, proxyURL string, headers http.Header) (network.Connection, error) {
			return mockConn, nil
		}

		c := New(cfg, log.Discard)
		err := c.Connect(t.Context(), CMServer{Type: "tcp", Endpoint: "1.1.1.1"})
		require.NoError(t, err)

		ok := c.SetEncryptionKey([]byte("secret_key_32_bytes_long_1234567"))
		assert.True(t, ok)
		assert.NotNil(t, mockConn.Cipher)
	})
}

func TestConnector_Disconnect(t *testing.T) {
	t.Parallel()

	t.Run("disconnect_unconnected", func(t *testing.T) {
		t.Parallel()

		c := New(DefaultConfig(), log.Discard)
		err := c.Disconnect()
		assert.NoError(t, err)
	})

	t.Run("disconnect_active", func(t *testing.T) {
		t.Parallel()

		cfg := DefaultConfig()
		mockConn := &MockConnection{
			id:       1,
			name:     "tcp",
			messages: make(chan network.Message, 1),
			errors:   make(chan error, 1),
			closed:   make(chan struct{}),
			sent:     make(chan []byte, 1),
		}
		cfg.Dialers["tcp"] = func(ctx context.Context, logger log.Logger, endpoint, proxyURL string, headers http.Header) (network.Connection, error) {
			return mockConn, nil
		}

		c := New(cfg, log.Discard)
		err := c.Connect(t.Context(), CMServer{Type: "tcp", Endpoint: "1.1.1.1"})
		require.NoError(t, err)

		err = c.Disconnect()
		assert.NoError(t, err)
		assert.False(t, c.IsConnected())
	})
}

func TestConnector_MonitorAndReconnect(t *testing.T) {
	t.Parallel()

	t.Run("pipe_inbound_messages", func(t *testing.T) {
		t.Parallel()

		cfg := DefaultConfig()
		mockConn := &MockConnection{
			id:       1,
			name:     "tcp",
			messages: make(chan network.Message, 1),
			errors:   make(chan error, 1),
			closed:   make(chan struct{}),
			sent:     make(chan []byte, 1),
		}
		cfg.Dialers["tcp"] = func(ctx context.Context, logger log.Logger, endpoint, proxyURL string, headers http.Header) (network.Connection, error) {
			return mockConn, nil
		}

		c := New(cfg, log.Discard)
		err := c.Connect(t.Context(), CMServer{Type: "tcp", Endpoint: "1.1.1.1"})
		require.NoError(t, err)

		mockConn.messages <- []byte("hello")

		select {
		case inbound := <-c.C():
			assert.Equal(t, []byte("hello"), inbound.Data)
			assert.Equal(t, "tcp", string(inbound.Transport))
		case <-time.After(1 * time.Second):
			t.Fatal("timeout waiting for piped message")
		}
	})

	t.Run("pipe_transport_errors", func(t *testing.T) {
		t.Parallel()

		cfg := DefaultConfig()
		mockConn := &MockConnection{
			id:       1,
			name:     "tcp",
			messages: make(chan network.Message, 1),
			errors:   make(chan error, 1),
			closed:   make(chan struct{}),
			sent:     make(chan []byte, 1),
		}
		cfg.Dialers["tcp"] = func(ctx context.Context, logger log.Logger, endpoint, proxyURL string, headers http.Header) (network.Connection, error) {
			return mockConn, nil
		}

		c := New(cfg, log.Discard)
		err := c.Connect(t.Context(), CMServer{Type: "tcp", Endpoint: "1.1.1.1"})
		require.NoError(t, err)

		mockConn.errors <- errors.New("socket read error")

		_ = c.Close()
	})

	t.Run("reconnect_loop_success", func(t *testing.T) {
		t.Parallel()

		reconnectAttempts := make(chan struct{}, 1)
		reconnectFinished := make(chan struct{})

		cfg := DefaultConfig()
		cfg.ReconnectPolicy = ReconnectPolicy{
			MaxAttempts:    3,
			InitialBackoff: 1 * time.Millisecond,
			MaxBackoff:     5 * time.Millisecond,
			BackoffFactor:  1.5,
			ServerSelector: func(servers []CMServer) CMServer {
				return CMServer{Type: "tcp", Endpoint: "1.1.1.1"}
			},
		}

		mockConn := &MockConnection{
			id:       1,
			name:     "tcp",
			messages: make(chan network.Message, 1),
			errors:   make(chan error, 1),
			closed:   make(chan struct{}),
			sent:     make(chan []byte, 1),
		}

		dialCalls := 0
		cfg.Dialers["tcp"] = func(ctx context.Context, logger log.Logger, endpoint, proxyURL string, headers http.Header) (network.Connection, error) {
			dialCalls++
			if dialCalls == 1 {
				return mockConn, nil
			}

			select {
			case reconnectAttempts <- struct{}{}:
			default:
			}

			reconnectedConn := &MockConnection{
				id:       2,
				name:     "tcp",
				messages: make(chan network.Message, 1),
				errors:   make(chan error, 1),
				closed:   make(chan struct{}),
				sent:     make(chan []byte, 1),
			}

			close(reconnectFinished)

			return reconnectedConn, nil
		}

		c := New(cfg, log.Discard)
		c.UpdateServers([]CMServer{{Type: "tcp", Endpoint: "1.1.1.1"}})

		err := c.Connect(t.Context(), CMServer{Type: "tcp", Endpoint: "1.1.1.1"})
		require.NoError(t, err)

		_ = mockConn.Close()

		select {
		case <-reconnectAttempts:
		case <-time.After(1 * time.Second):
			t.Fatal("timeout waiting for reconnect attempt")
		}

		select {
		case <-reconnectFinished:
		case <-time.After(1 * time.Second):
			t.Fatal("timeout waiting for reconnect finish")
		}

		assert.True(t, c.IsConnected())
	})

	t.Run("reconnect_loop_exhausted", func(t *testing.T) {
		t.Parallel()

		reconnectAttempts := make(chan struct{}, 2)

		cfg := DefaultConfig()
		cfg.ReconnectPolicy = ReconnectPolicy{
			MaxAttempts:    2,
			InitialBackoff: 1 * time.Millisecond,
			MaxBackoff:     5 * time.Millisecond,
			BackoffFactor:  1.5,
			ServerSelector: func(servers []CMServer) CMServer {
				return CMServer{Type: "tcp", Endpoint: "1.1.1.1"}
			},
		}

		mockConn := &MockConnection{
			id:       1,
			name:     "tcp",
			messages: make(chan network.Message, 1),
			errors:   make(chan error, 1),
			closed:   make(chan struct{}),
			sent:     make(chan []byte, 1),
		}

		dialCalls := 0
		cfg.Dialers["tcp"] = func(ctx context.Context, logger log.Logger, endpoint, proxyURL string, headers http.Header) (network.Connection, error) {
			dialCalls++
			if dialCalls == 1 {
				return mockConn, nil
			}

			select {
			case reconnectAttempts <- struct{}{}:
			default:
			}

			return nil, errors.New("reconnect dial failed")
		}

		c := New(cfg, log.Discard)

		err := c.Connect(t.Context(), CMServer{Type: "tcp", Endpoint: "1.1.1.1"})
		require.NoError(t, err)

		_ = mockConn.Close()

		for range 2 {
			select {
			case <-reconnectAttempts:
			case <-time.After(1 * time.Second):
				t.Fatal("timeout waiting for reconnect attempt to fail")
			}
		}

		time.Sleep(10 * time.Millisecond)
		assert.False(t, c.IsConnected())
	})

	t.Run("handle_disconnect_mismatched_connection", func(t *testing.T) {
		t.Parallel()

		c := New(DefaultConfig(), log.Discard)
		mockConn1 := &MockConnection{id: 1, closed: make(chan struct{})}
		mockConn2 := &MockConnection{id: 2, closed: make(chan struct{})}

		c.conn = mockConn1
		c.handleDisconnect(mockConn2)

		assert.Equal(t, mockConn1, c.conn)
	})

	t.Run("disconnect_no_reconnect_when_attempts_negative", func(t *testing.T) {
		t.Parallel()

		cfg := DefaultConfig()
		cfg.ReconnectPolicy.MaxAttempts = -1

		mockConn := &MockConnection{id: 1, closed: make(chan struct{})}
		cfg.Dialers["tcp"] = func(ctx context.Context, logger log.Logger, endpoint, proxyURL string, headers http.Header) (network.Connection, error) {
			return mockConn, nil
		}

		c := New(cfg, log.Discard)
		err := c.Connect(t.Context(), CMServer{Type: "tcp", Endpoint: "1.1.1.1"})
		require.NoError(t, err)

		_ = mockConn.Close()

		time.Sleep(10 * time.Millisecond)

		c.mu.RLock()
		assert.Nil(t, c.reconnectCancel)
		assert.Nil(t, c.conn)
		c.mu.RUnlock()
	})

	t.Run("disconnect_no_reconnect_when_connector_closed", func(t *testing.T) {
		t.Parallel()

		cfg := DefaultConfig()
		cfg.ReconnectPolicy.MaxAttempts = 3

		mockConn := &MockConnection{id: 1, closed: make(chan struct{})}
		cfg.Dialers["tcp"] = func(ctx context.Context, logger log.Logger, endpoint, proxyURL string, headers http.Header) (network.Connection, error) {
			return mockConn, nil
		}

		c := New(cfg, log.Discard)
		err := c.Connect(t.Context(), CMServer{Type: "tcp", Endpoint: "1.1.1.1"})
		require.NoError(t, err)

		_ = c.Close()

		c.handleDisconnect(mockConn)

		c.mu.RLock()
		assert.Nil(t, c.reconnectCancel)
		c.mu.RUnlock()
	})

	t.Run("cancel_active_reconnect_before_starting_new", func(t *testing.T) {
		t.Parallel()

		cfg := DefaultConfig()
		cfg.ReconnectPolicy.MaxAttempts = 3

		mockConn := &MockConnection{id: 1, closed: make(chan struct{})}
		cfg.Dialers["tcp"] = func(ctx context.Context, logger log.Logger, endpoint, proxyURL string, headers http.Header) (network.Connection, error) {
			return mockConn, nil
		}

		c := New(cfg, log.Discard)
		err := c.Connect(t.Context(), CMServer{Type: "tcp", Endpoint: "1.1.1.1"})
		require.NoError(t, err)

		var cancelCalled bool

		c.reconnectCancel = func() {
			cancelCalled = true
		}

		c.handleDisconnect(mockConn)
		assert.True(t, cancelCalled)
	})

	t.Run("reconnect_loop_terminated_by_context_cancel", func(t *testing.T) {
		t.Parallel()

		cfg := DefaultConfig()
		cfg.ReconnectPolicy = ReconnectPolicy{
			MaxAttempts:    5,
			InitialBackoff: 100 * time.Millisecond,
			MaxBackoff:     200 * time.Millisecond,
			BackoffFactor:  2.0,
			ServerSelector: func(servers []CMServer) CMServer {
				return CMServer{Type: "tcp", Endpoint: "1.1.1.1"}
			},
		}

		mockConn := &MockConnection{id: 1, closed: make(chan struct{})}
		cfg.Dialers["tcp"] = func(ctx context.Context, logger log.Logger, endpoint, proxyURL string, headers http.Header) (network.Connection, error) {
			return nil, errors.New("always fail")
		}

		c := New(cfg, log.Discard)
		c.conn = mockConn
		c.lastServer = CMServer{Type: "tcp", Endpoint: "1.1.1.1"}

		c.handleDisconnect(mockConn)

		c.mu.RLock()
		assert.NotNil(t, c.reconnectCancel)
		c.mu.RUnlock()

		c.cancelReconnect()

		time.Sleep(10 * time.Millisecond)
		assert.False(t, c.IsConnected())
	})

	t.Run("reconnect_fallback_to_last_server", func(t *testing.T) {
		t.Parallel()

		cfg := DefaultConfig()
		cfg.ReconnectPolicy = ReconnectPolicy{
			MaxAttempts:    1,
			InitialBackoff: 1 * time.Millisecond,
			ServerSelector: func(servers []CMServer) CMServer {
				return CMServer{}
			},
		}

		mockConn := &MockConnection{id: 1, closed: make(chan struct{})}
		dialedLast := false
		cfg.Dialers["tcp"] = func(ctx context.Context, logger log.Logger, endpoint, proxyURL string, headers http.Header) (network.Connection, error) {
			if endpoint == "last_endpoint" {
				dialedLast = true
			}

			return nil, errors.New("fail")
		}

		c := New(cfg, log.Discard)
		c.conn = mockConn
		c.lastServer = CMServer{Type: "tcp", Endpoint: "last_endpoint"}

		c.handleDisconnect(mockConn)

		time.Sleep(10 * time.Millisecond)
		assert.True(t, dialedLast)
	})

	t.Run("reconnect_loop_cancelled_during_backoff", func(t *testing.T) {
		t.Parallel()

		cfg := DefaultConfig()
		cfg.ReconnectPolicy = ReconnectPolicy{
			MaxAttempts:    5,
			InitialBackoff: 500 * time.Millisecond,
			MaxBackoff:     1000 * time.Millisecond,
			BackoffFactor:  2.0,
			ServerSelector: func(servers []CMServer) CMServer {
				return CMServer{Type: "tcp", Endpoint: "1.1.1.1"}
			},
		}

		mockConn := &MockConnection{id: 1, closed: make(chan struct{})}
		dialAttempts := make(chan struct{}, 1)
		cfg.Dialers["tcp"] = func(ctx context.Context, logger log.Logger, endpoint, proxyURL string, headers http.Header) (network.Connection, error) {
			select {
			case dialAttempts <- struct{}{}:
			default:
			}

			return nil, errors.New("fail")
		}

		c := New(cfg, log.Discard)
		c.conn = mockConn
		c.lastServer = CMServer{Type: "tcp", Endpoint: "1.1.1.1"}

		c.handleDisconnect(mockConn)

		select {
		case <-dialAttempts:
		case <-time.After(1 * time.Second):
			t.Fatal("timeout waiting for first dial")
		}

		c.cancelReconnect()

		time.Sleep(10 * time.Millisecond)
		assert.False(t, c.IsConnected())
	})
}

// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package socket_test

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	aoni_socket "github.com/lemon4ksan/aoni-contrib/socket"
	"github.com/lemon4ksan/aoni-contrib/socket/connector"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lemon4ksan/g-man/pkg/steam/socket"
)

func TestSocket_Heartbeat_ConsecutiveFailures_TriggersReconnect(t *testing.T) {
	t.Parallel()

	mConn := newMockConnection()
	mConn.mu.Lock()
	mConn.sendErr = errors.New("broken pipe")
	mConn.mu.Unlock()

	cfg := socket.DefaultConfig()
	cfg.MaxHeartbeatFailures = 2
	cfg.Connector.Dialers = map[string]socket.Dialer{
		"mock": func(ctx context.Context, endpoint socket.CMServer, framer aoni_socket.Framer, cipher aoni_socket.Cipher) (connector.Connection, error) {
			return mConn, nil
		},
	}

	var reconnectCalls atomic.Int32

	s := socket.New(cfg)
	defer s.Close()

	s.SetOnReconnect(func(ctx context.Context) {
		reconnectCalls.Add(1)
	})

	err := s.Connect(t.Context(), socket.CMServer{Type: "mock", Endpoint: "127.0.0.1:1234"})
	require.NoError(t, err)
	assert.True(t, s.IsConnected())

	// Start heartbeat with very small interval so it fires quickly
	err = s.StartHeartbeat(30 * time.Millisecond)
	require.NoError(t, err)

	// Wait for heartbeat failure threshold to be hit (2 failures * ~20ms interval)
	require.Eventually(t, func() bool {
		return mConn.closed.Load() || !s.IsConnected()
	}, 1*time.Second, 10*time.Millisecond, "socket transport should disconnect on consecutive heartbeat failures")
}

func TestSocket_Heartbeat_SuccessResetsFailureCounter(t *testing.T) {
	t.Parallel()

	mConn := newMockConnection()

	cfg := socket.DefaultConfig()
	cfg.MaxHeartbeatFailures = 2
	cfg.Connector.Dialers = map[string]socket.Dialer{
		"mock": func(ctx context.Context, endpoint socket.CMServer, framer aoni_socket.Framer, cipher aoni_socket.Cipher) (connector.Connection, error) {
			return mConn, nil
		},
	}

	s := socket.New(cfg)
	defer s.Close()

	err := s.Connect(t.Context(), socket.CMServer{Type: "mock", Endpoint: "127.0.0.1:1234"})
	require.NoError(t, err)
	assert.True(t, s.IsConnected())

	// Start heartbeat with 30ms interval
	err = s.StartHeartbeat(30 * time.Millisecond)
	require.NoError(t, err)

	// Inject 1 failure
	time.Sleep(10 * time.Millisecond)
	mConn.mu.Lock()
	mConn.sendErr = errors.New("transient send error")
	mConn.mu.Unlock()

	// Wait for 1 heartbeat cycle to fail
	time.Sleep(30 * time.Millisecond)

	// Clear error so next heartbeat succeeds, resetting failure count
	mConn.mu.Lock()
	mConn.sendErr = nil
	mConn.mu.Unlock()

	time.Sleep(50 * time.Millisecond)

	// Inject 1 more failure (should be count 1, not 2)
	mConn.mu.Lock()
	mConn.sendErr = errors.New("second transient send error")
	mConn.mu.Unlock()

	time.Sleep(20 * time.Millisecond)

	// Transport should still be connected because failures were not consecutive >= 2
	mConn.mu.Lock()
	mConn.sendErr = nil
	mConn.mu.Unlock()

	assert.False(t, mConn.closed.Load(), "transport should remain connected when failure count is reset by success")
	assert.True(t, s.IsConnected())
}

func TestSocket_CurrentServer_And_ConnectionState(t *testing.T) {
	t.Parallel()

	mConn := newMockConnection()
	cfg := socket.DefaultConfig()
	cfg.Connector.Dialers = map[string]socket.Dialer{
		"mock": func(ctx context.Context, endpoint socket.CMServer, framer aoni_socket.Framer, cipher aoni_socket.Cipher) (connector.Connection, error) {
			return mConn, nil
		},
	}

	s := socket.New(cfg)
	defer s.Close()

	srv := socket.CMServer{Type: "mock", Endpoint: "10.0.0.1:27015"}
	err := s.Connect(t.Context(), srv)
	require.NoError(t, err)

	cur, ok := s.CurrentServer()
	assert.True(t, ok)
	assert.Equal(t, srv, cur)
	assert.False(t, s.IsConnecting())

	err = s.WaitForConnection(t.Context())
	assert.NoError(t, err)
}

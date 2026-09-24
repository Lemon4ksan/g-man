// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package socket_test

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	aoni_socket "github.com/lemon4ksan/aoni-contrib/socket"
	"github.com/lemon4ksan/aoni-contrib/socket/connector"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lemon4ksan/g-man/pkg/steam/socket"
)

// hangingMockConnection simulates a socket that hangs on Send until context expires.
type hangingMockConnection struct {
	closed atomic.Bool
	sends  atomic.Int32
}

func (h *hangingMockConnection) Send(ctx context.Context, _ []byte) error {
	h.sends.Add(1)
	<-ctx.Done()

	return ctx.Err()
}

func (h *hangingMockConnection) Receive(ctx context.Context) (*aoni_socket.FrameBuffer, error) {
	<-ctx.Done()
	return nil, ctx.Err()
}

func (h *hangingMockConnection) Close() error {
	h.closed.Store(true)
	return nil
}

// countedMockConnection intercepts Send calls and routes them through a deterministic hook.
type countedMockConnection struct {
	closed atomic.Bool
	calls  atomic.Int32
	hook   func(n int32) error
}

func (c *countedMockConnection) Send(_ context.Context, _ []byte) error {
	n := c.calls.Add(1)
	if c.hook != nil {
		return c.hook(n)
	}

	return nil
}

func (c *countedMockConnection) Receive(ctx context.Context) (*aoni_socket.FrameBuffer, error) {
	<-ctx.Done()
	return nil, ctx.Err()
}

func (c *countedMockConnection) Close() error {
	c.closed.Store(true)
	return nil
}

// TestSocket_Heartbeat_ExceedingMaxFailures_3_TearsDownAndTriggersReconnect tests that
// exactly 3 consecutive dropped heartbeats (MaxHeartbeatFailures = 3) tear down transport and trigger reconnect.
func TestSocket_Heartbeat_ExceedingMaxFailures_3_TearsDownAndTriggersReconnect(t *testing.T) {
	t.Parallel()

	mConn := newMockConnection()

	// Setup mock to fail Send
	mConn.mu.Lock()
	mConn.sendErr = errors.New("heartbeat transport dropped")
	mConn.mu.Unlock()

	cfg := socket.DefaultConfig()
	cfg.MaxHeartbeatFailures = 3 // Explicitly 3 failures as required
	cfg.Connector.ConnectTimeout = 100 * time.Millisecond
	reconPolicy := connector.DefaultReconnectPolicy[socket.CMServer]()
	reconPolicy.InitialBackoff = 10 * time.Millisecond
	reconPolicy.MaxBackoff = 50 * time.Millisecond
	reconPolicy.BackoffFactor = 1.5
	cfg.Connector.ReconnectPolicy = reconPolicy

	var (
		reconnectTriggered atomic.Bool
		dialCount          atomic.Int32
	)

	reconnectConn := newMockConnection()

	cfg.Connector.Dialers = map[string]socket.Dialer{
		"mock": func(ctx context.Context, endpoint socket.CMServer, framer aoni_socket.Framer, cipher aoni_socket.Cipher) (connector.Connection, error) {
			count := dialCount.Add(1)
			if count == 1 {
				return mConn, nil
			}

			reconnectTriggered.Store(true)

			return reconnectConn, nil
		},
	}

	s := socket.New(cfg)
	defer s.Close()

	srv := socket.CMServer{Type: "mock", Endpoint: "127.0.0.1:27015"}
	err := s.Connect(t.Context(), srv)
	require.NoError(t, err)
	assert.True(t, s.IsConnected())
	assert.Equal(t, int32(1), dialCount.Load())

	// Start heartbeat with 30ms interval (sendInterval = 20ms)
	err = s.StartHeartbeat(30 * time.Millisecond)
	require.NoError(t, err)

	// Invariant: After 3 consecutive heartbeat failures, original connection MUST be closed,
	// and reconnection dialer must be invoked.
	require.Eventually(t, func() bool {
		return mConn.closed.Load() && reconnectTriggered.Load()
	}, 2*time.Second, 10*time.Millisecond, "socket transport should be torn down and reconnect loop triggered after 3 heartbeat failures")
}

// TestSocket_Heartbeat_SuccessResetsCounter_MultiCycleStress stress-tests that
// every successful heartbeat resets the failure counter back to 0, preventing
// spurious transport teardown even under 10 cycles of 2 consecutive failures (20 failures total).
func TestSocket_Heartbeat_SuccessResetsCounter_MultiCycleStress(t *testing.T) {
	t.Parallel()

	// Hook pattern: 2 failures followed by 1 success, repeated across 5 cycles (15 calls total).
	// Calls 1..15 contain 10 failures and 5 successes.
	// Since each success resets consecutiveFailures to 0, consecutiveFailures never reaches 3.
	// Calls 16, 17, 18 all fail -> consecutiveFailures reaches 3 -> transport teardown.
	countedConn := &countedMockConnection{
		hook: func(n int32) error {
			if n <= 15 {
				if n%3 == 0 {
					return nil // Success resets failure counter
				}

				return errors.New("transient heartbeat failure")
			}

			return errors.New("terminal heartbeat failure")
		},
	}

	cfg := socket.DefaultConfig()
	cfg.MaxHeartbeatFailures = 3
	cfg.Connector.Dialers = map[string]socket.Dialer{
		"mock": func(ctx context.Context, endpoint socket.CMServer, framer aoni_socket.Framer, cipher aoni_socket.Cipher) (connector.Connection, error) {
			return countedConn, nil
		},
	}

	s := socket.New(cfg)
	defer s.Close()

	err := s.Connect(t.Context(), socket.CMServer{Type: "mock", Endpoint: "127.0.0.1:27015"})
	require.NoError(t, err)
	assert.True(t, s.IsConnected())

	// Heartbeat interval 15ms => sendInterval = 10ms
	err = s.StartHeartbeat(15 * time.Millisecond)
	require.NoError(t, err)

	// Wait until all 18 calls have completed and transport is torn down.
	require.Eventually(t, func() bool {
		return countedConn.closed.Load() || !s.IsConnected()
	}, 5*time.Second, 10*time.Millisecond, "transport must tear down after 3 consecutive failures following 5 reset cycles")

	// Invariant: The call count must have reached at least 18 before teardown occurred.
	// If the counter had NOT reset upon each success, teardown would have occurred prematurely at call 4.
	assert.GreaterOrEqual(
		t,
		countedConn.calls.Load(),
		int32(18),
		"must survive 5 cycles of 2 failures each without premature teardown",
	)
}

// TestSocket_Heartbeat_StalledSendTimeout_TearsDownTransport tests that context
// deadline expiration on hanging Send calls triggers heartbeat failure handling and teardown.
func TestSocket_Heartbeat_StalledSendTimeout_TearsDownTransport(t *testing.T) {
	t.Parallel()

	hangingConn := &hangingMockConnection{}

	cfg := socket.DefaultConfig()
	cfg.MaxHeartbeatFailures = 3
	cfg.Connector.Dialers = map[string]socket.Dialer{
		"mock": func(ctx context.Context, endpoint socket.CMServer, framer aoni_socket.Framer, cipher aoni_socket.Cipher) (connector.Connection, error) {
			return hangingConn, nil
		},
	}

	s := socket.New(cfg)
	defer s.Close()

	err := s.Connect(t.Context(), socket.CMServer{Type: "mock", Endpoint: "127.0.0.1:27015"})
	require.NoError(t, err)

	// Heartbeat interval 30ms => sendInterval = 20ms, sendTimeout = 20ms
	err = s.StartHeartbeat(30 * time.Millisecond)
	require.NoError(t, err)

	// Each hanging Send will timeout after 20ms.
	// After 3 timeouts (~60-100ms), consecutiveFailures reaches 3 and disconnects.
	require.Eventually(t, func() bool {
		return hangingConn.closed.Load() || !s.IsConnected()
	}, 2*time.Second, 20*time.Millisecond, "hanging heartbeat sends must timeout and trigger transport teardown")

	assert.GreaterOrEqual(t, hangingConn.sends.Load(), int32(3))
}

// TestSocket_Heartbeat_ConcurrentRestart_RaceDetector tests rapid concurrent StartHeartbeat calls
// under the Go race detector to verify safe cancellation and no data races.
func TestSocket_Heartbeat_ConcurrentRestart_RaceDetector(t *testing.T) {
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

	err := s.Connect(t.Context(), socket.CMServer{Type: "mock", Endpoint: "127.0.0.1:27015"})
	require.NoError(t, err)

	var wg sync.WaitGroup

	for i := 0; i < 20; i++ {
		wg.Add(1)

		fn := func(idx int) {
			defer wg.Done()

			interval := time.Duration(10+idx) * time.Millisecond
			_ = s.StartHeartbeat(interval)
		}
		go fn(i)
	}

	wg.Wait()
	assert.True(t, s.IsConnected())
}

// TestSocket_Heartbeat_MultiCycle_TeardownAndCounterReset_LifecycleStress tests that
// stopping and restarting heartbeat loops across multiple cycles resets failure counters,
// terminates old heartbeat goroutines cleanly, and prevents premature transport disconnection.
func TestSocket_Heartbeat_MultiCycle_TeardownAndCounterReset_LifecycleStress(t *testing.T) {
	t.Parallel()

	var (
		callCount atomic.Int32
		mu        sync.Mutex
		sendHook  func() error
	)

	mockConn := newMockConnection()
	mockConn.mu.Lock()
	mockConn.sendHook = func() error {
		callCount.Add(1)

		mu.Lock()
		h := sendHook
		mu.Unlock()

		if h != nil {
			return h()
		}

		return nil
	}
	mockConn.mu.Unlock()

	cfg := socket.DefaultConfig()
	cfg.MaxHeartbeatFailures = 3
	cfg.Connector.Dialers = map[string]socket.Dialer{
		"mock": func(ctx context.Context, endpoint socket.CMServer, framer aoni_socket.Framer, cipher aoni_socket.Cipher) (connector.Connection, error) {
			return mockConn, nil
		},
	}

	s := socket.New(cfg)
	defer s.Close()

	err := s.Connect(t.Context(), socket.CMServer{Type: "mock", Endpoint: "127.0.0.1:27015"})
	require.NoError(t, err)

	// Execute 10 consecutive teardown & reset lifecycles
	for cycle := 0; cycle < 10; cycle++ {
		// Hook: fail 2 sends, then succeed (resets failure counter back to 0)
		var cycleAttempts atomic.Int32

		mu.Lock()
		sendHook = func() error {
			att := cycleAttempts.Add(1)
			if att <= 2 {
				return errors.New("transient heartbeat drop")
			}

			return nil
		}
		mu.Unlock()

		// Start heartbeat with short interval (15ms -> sendInterval 10ms)
		// This also cancels and tears down any existing heartbeat loop
		err = s.StartHeartbeat(15 * time.Millisecond)
		require.NoError(t, err)

		// Wait until at least 3 attempts have fired (2 failures, 1 success)
		require.Eventually(t, func() bool {
			return cycleAttempts.Load() >= 3
		}, 2*time.Second, 10*time.Millisecond, fmt.Sprintf("cycle %d should reach 3 attempts", cycle))

		// Invariant: Transport MUST remain connected because success reset the counter
		assert.True(t, s.IsConnected(), fmt.Sprintf("transport should remain connected in cycle %d", cycle))
		assert.False(t, mockConn.closed.Load(), fmt.Sprintf("connection should not be closed in cycle %d", cycle))
	}

	// Final verification: 3 consecutive failures MUST now tear down transport
	mu.Lock()
	sendHook = func() error {
		return errors.New("terminal failure")
	}
	mu.Unlock()

	require.Eventually(t, func() bool {
		return mockConn.closed.Load() || !s.IsConnected()
	}, 2*time.Second, 10*time.Millisecond, "3 consecutive terminal failures must trigger transport teardown")
}

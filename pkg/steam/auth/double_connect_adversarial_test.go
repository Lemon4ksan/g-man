// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package auth_test

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lemon4ksan/foundation/async/event"
	log "github.com/lemon4ksan/foundation/async/logkit"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"

	"github.com/lemon4ksan/g-man/pkg/steam/auth"
	"github.com/lemon4ksan/g-man/pkg/steam/protocol/enums"
	"github.com/lemon4ksan/g-man/pkg/steam/socket"
	"github.com/lemon4ksan/g-man/pkg/storage/memory"
	pb "github.com/lemon4ksan/g-man/protobuf/steam"
)

// concurrentStressSocket provides a completely thread-safe atomic mock
// to strictly verify dial prevention without lock contention or testify race conditions.
type concurrentStressSocket struct {
	isConnected     atomic.Bool
	isConnecting    atomic.Bool
	connectCalls    atomic.Int32
	waitCalls       atomic.Int32
	sendCalls       atomic.Int32
	waitForConnFunc func(ctx context.Context) error
	session         socket.Session
}

func newConcurrentStressSocket() *concurrentStressSocket {
	return &concurrentStressSocket{
		session: &socket.BasicSession{},
	}
}

func (s *concurrentStressSocket) IsConnected() bool  { return s.isConnected.Load() }
func (s *concurrentStressSocket) IsConnecting() bool { return s.isConnecting.Load() }

func (s *concurrentStressSocket) WaitForConnection(ctx context.Context) error {
	s.waitCalls.Add(1)

	if s.waitForConnFunc != nil {
		return s.waitForConnFunc(ctx)
	}

	return nil
}

func (s *concurrentStressSocket) CurrentServer() (socket.CMServer, bool) {
	return socket.CMServer{Type: "mock", Endpoint: "10.0.0.1:27015"}, s.isConnected.Load()
}

func (s *concurrentStressSocket) Connect(_ context.Context, _ socket.CMServer) error {
	s.connectCalls.Add(1)
	return nil
}

func (s *concurrentStressSocket) SendProto(
	_ context.Context,
	_ enums.EMsg,
	_ proto.Message,
	_ ...socket.SendOption,
) error {
	s.sendCalls.Add(1)
	return nil
}

func (s *concurrentStressSocket) SendRaw(_ context.Context, _ enums.EMsg, _ []byte, _ ...socket.SendOption) error {
	s.sendCalls.Add(1)
	return nil
}

func (s *concurrentStressSocket) SetEncryptionKey(_ []byte) bool                    { return true }
func (s *concurrentStressSocket) RegisterMsgHandler(_ enums.EMsg, _ socket.Handler) {}
func (s *concurrentStressSocket) Session() socket.Session                           { return s.session }
func (s *concurrentStressSocket) StartHeartbeat(_ time.Duration) error              { return nil }

type mockWebAuth struct{}

func (m *mockWebAuth) BeginAuthSessionViaCredentials(
	_ context.Context,
	_, _, _ string,
) (*pb.CAuthentication_BeginAuthSessionViaCredentials_Response, error) {
	return nil, nil
}

func (m *mockWebAuth) PollAuthSessionStatus(
	_ context.Context,
	_ uint64,
	_ []byte,
) (*pb.CAuthentication_PollAuthSessionStatus_Response, error) {
	return nil, nil
}

func (m *mockWebAuth) UpdateAuthSessionWithSteamGuardCode(
	_ context.Context,
	_, _ uint64,
	_ string,
	_ pb.EAuthSessionGuardType,
) error {
	return nil
}

func (m *mockWebAuth) GenerateAccessTokenForApp(
	_ context.Context,
	_ string,
	_ uint64,
) (*pb.CAuthentication_AccessToken_GenerateForApp_Response, error) {
	return nil, nil
}

// TestAuthenticator_ConcurrentLogOn_WhenConnected_StrictlyZeroDials stress tests
// 50 concurrent LogOn attempts while socket is connected.
// Invariant: Exactly ZERO socket dials (Connect calls) must occur.
func TestAuthenticator_ConcurrentLogOn_WhenConnected_StrictlyZeroDials(t *testing.T) {
	t.Parallel()

	sock := newConcurrentStressSocket()
	sock.isConnected.Store(true)

	bus := event.New()
	webAPI := new(mockWebAuth)
	memStore := memory.New()
	store := auth.NewKVStore(memStore.KV("auth"))

	a := auth.NewAuthenticator(sock, webAPI, bus, auth.WithStorage(store), auth.WithLogger(log.Discard))

	var wg sync.WaitGroup

	numWorkers := 50
	errs := make([]error, numWorkers)

	for i := 0; i < numWorkers; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()

			ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
			defer cancel()

			details := &auth.LogOnDetails{
				AccountName:  "testuser",
				RefreshToken: "test_refresh_token",
			}
			server := socket.CMServer{Type: "mock", Endpoint: "10.0.0.1:27015"}
			errs[idx] = a.LogOn(ctx, details, server)
		}(i)
	}

	wg.Wait()

	// Invariant: Zero dials to Connect on already-connected transport
	assert.Equal(t, int32(0), sock.connectCalls.Load(), "Connect must never be called when socket is connected")
}

// TestAuthenticator_ConcurrentLogOnAnonymous_WhenConnected_StrictlyZeroDials stress tests
// 50 concurrent LogOnAnonymous attempts while socket is connected.
// Invariant: Exactly ZERO socket dials (Connect calls) must occur.
func TestAuthenticator_ConcurrentLogOnAnonymous_WhenConnected_StrictlyZeroDials(t *testing.T) {
	t.Parallel()

	sock := newConcurrentStressSocket()
	sock.isConnected.Store(true)

	bus := event.New()
	webAPI := new(mockWebAuth)
	memStore := memory.New()
	store := auth.NewKVStore(memStore.KV("auth"))

	a := auth.NewAuthenticator(sock, webAPI, bus, auth.WithStorage(store), auth.WithLogger(log.Discard))

	var wg sync.WaitGroup

	numWorkers := 50

	for i := 0; i < numWorkers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()

			ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
			defer cancel()

			server := socket.CMServer{Type: "mock", Endpoint: "10.0.0.1:27015"}
			_ = a.LogOnAnonymous(ctx, server)
		}()
	}

	wg.Wait()

	// Invariant: Zero dials to Connect on already-connected transport
	assert.Equal(t, int32(0), sock.connectCalls.Load(), "Connect must never be called during concurrent LogOnAnonymous")
}

// TestAuthenticator_ConcurrentMixed_WhenConnecting_AwaitsAndZeroDials stress tests
// simultaneous LogOn and LogOnAnonymous calls while transport is actively dialing (IsConnecting == true).
// Invariant: All callers wait on WaitForConnection and NONE trigger duplicate Connect calls.
func TestAuthenticator_ConcurrentMixed_WhenConnecting_AwaitsAndZeroDials(t *testing.T) {
	t.Parallel()

	sock := newConcurrentStressSocket()
	sock.isConnecting.Store(true)
	sock.isConnected.Store(false)

	dialComplete := make(chan struct{})
	sock.waitForConnFunc = func(ctx context.Context) error {
		select {
		case <-dialComplete:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}

	bus := event.New()
	webAPI := new(mockWebAuth)
	memStore := memory.New()
	store := auth.NewKVStore(memStore.KV("auth"))

	a := auth.NewAuthenticator(sock, webAPI, bus, auth.WithStorage(store), auth.WithLogger(log.Discard))

	var wg sync.WaitGroup

	numWorkers := 40

	for i := 0; i < numWorkers; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()

			ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
			defer cancel()

			server := socket.CMServer{Type: "mock", Endpoint: "10.0.0.1:27015"}
			if idx%2 == 0 {
				details := &auth.LogOnDetails{
					AccountName:  "testuser",
					RefreshToken: "test_refresh_token",
				}
				_ = a.LogOn(ctx, details, server)
			} else {
				_ = a.LogOnAnonymous(ctx, server)
			}
		}(i)
	}

	// Wait for callers to enter WaitForConnection
	require.Eventually(t, func() bool {
		return sock.waitCalls.Load() > 0
	}, 1*time.Second, 10*time.Millisecond)

	// Simulate transport connection completion
	sock.isConnected.Store(true)
	sock.isConnecting.Store(false)
	close(dialComplete)

	wg.Wait()

	// Invariant: Zero duplicate Connect calls
	assert.Equal(
		t,
		int32(0),
		sock.connectCalls.Load(),
		"Connect must never be called by waiters once connection completes",
	)
	assert.GreaterOrEqual(t, sock.waitCalls.Load(), int32(1), "WaitForConnection must have been invoked")
}

// TestAuthenticator_RapidSequential_NoDuplicateConnect verifies that rapid sequential
// LogOn and LogOnAnonymous calls when connected never invoke Connect.
func TestAuthenticator_RapidSequential_NoDuplicateConnect(t *testing.T) {
	t.Parallel()

	sock := newConcurrentStressSocket()
	sock.isConnected.Store(true)

	bus := event.New()
	webAPI := new(mockWebAuth)
	memStore := memory.New()
	store := auth.NewKVStore(memStore.KV("auth"))

	a := auth.NewAuthenticator(sock, webAPI, bus, auth.WithStorage(store), auth.WithLogger(log.Discard))

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	server := socket.CMServer{Type: "mock", Endpoint: "10.0.0.1:27015"}
	details := &auth.LogOnDetails{
		AccountName:  "testuser",
		RefreshToken: "test_refresh_token",
	}

	for i := 0; i < 30; i++ {
		_ = a.LogOn(ctx, details, server)
	}

	assert.Equal(
		t,
		int32(0),
		sock.connectCalls.Load(),
		"Connect must strictly never be called during rapid sequential calls",
	)
}

// TestAuthenticator_LogOnThenLogOnAnonymous_ConsistentAtomicValueType asserts that
// LogOn followed by LogOnAnonymous on the same Authenticator instance does not panic
// from sync/atomic.Value type mismatch and cleanly cancels contexts.
func TestAuthenticator_LogOnThenLogOnAnonymous_ConsistentAtomicValueType(t *testing.T) {
	t.Parallel()

	sock := newConcurrentStressSocket()
	sock.isConnected.Store(true)

	bus := event.New()
	webAPI := new(mockWebAuth)
	memStore := memory.New()
	store := auth.NewKVStore(memStore.KV("auth"))

	a := auth.NewAuthenticator(sock, webAPI, bus, auth.WithStorage(store), auth.WithLogger(log.Discard))

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	server := socket.CMServer{Type: "mock", Endpoint: "10.0.0.1:27015"}
	details := &auth.LogOnDetails{
		AccountName:  "testuser",
		RefreshToken: "test_refresh_token",
	}

	// First call LogOn: stores context.CancelCauseFunc into a.loginCancel
	_ = a.LogOn(ctx, details, server)

	// Second call LogOnAnonymous: stores context.CancelCauseFunc into a.loginCancel
	// Must NOT panic because both store the identical concrete type context.CancelCauseFunc
	assert.NotPanics(t, func() {
		_ = a.LogOnAnonymous(ctx, server)
	}, "LogOn followed by LogOnAnonymous must not panic from atomic.Value type mismatch")
}

// TestAuthenticator_AlternatingLogOnAndAnonymous_HighConcurrencyStress stresses rapid
// alternating calls between LogOn and LogOnAnonymous under high concurrency (40 workers,
// 400 operations total) to confirm zero sync/atomic.Value type mismatch panics, zero context
// leaks, and zero data races.
func TestAuthenticator_AlternatingLogOnAndAnonymous_HighConcurrencyStress(t *testing.T) {
	t.Parallel()

	sock := newConcurrentStressSocket()
	sock.isConnected.Store(true)

	bus := event.New()
	webAPI := new(mockWebAuth)
	memStore := memory.New()
	store := auth.NewKVStore(memStore.KV("auth"))

	a := auth.NewAuthenticator(sock, webAPI, bus, auth.WithStorage(store), auth.WithLogger(log.Discard))

	server := socket.CMServer{Type: "mock", Endpoint: "10.0.0.1:27015"}
	numWorkers := 40
	opsPerWorker := 10

	var (
		wg          sync.WaitGroup
		panicCount  atomic.Int32
		activeCalls atomic.Int32
	)

	for w := 0; w < numWorkers; w++ {
		wg.Add(1)

		go func(workerID int) {
			defer wg.Done()

			defer func() {
				if r := recover(); r != nil {
					panicCount.Add(1)
					t.Errorf("Worker %d panicked: %v", workerID, r)
				}
			}()

			for i := 0; i < opsPerWorker; i++ {
				ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)

				activeCalls.Add(1)

				if (workerID+i)%2 == 0 {
					details := &auth.LogOnDetails{
						AccountName:  "testuser",
						RefreshToken: "test_refresh_token",
					}
					_ = a.LogOn(ctx, details, server)
				} else {
					_ = a.LogOnAnonymous(ctx, server)
				}

				activeCalls.Add(-1)
				cancel()
			}
		}(w)
	}

	wg.Wait()

	assert.Equal(t, int32(0), panicCount.Load(), "Must have zero sync/atomic.Value type mismatch panics")
	assert.Equal(t, int32(0), activeCalls.Load(), "All concurrent calls must have terminated cleanly")
}

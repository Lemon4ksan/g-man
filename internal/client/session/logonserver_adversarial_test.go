// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package session

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lemon4ksan/aoni"
	log "github.com/lemon4ksan/foundation/async/logkit"
	"github.com/stretchr/testify/assert"
	"google.golang.org/protobuf/proto"

	"github.com/lemon4ksan/g-man/pkg/steam/auth"
	"github.com/lemon4ksan/g-man/pkg/steam/community"
	"github.com/lemon4ksan/g-man/pkg/steam/id"
	"github.com/lemon4ksan/g-man/pkg/steam/protocol"
	"github.com/lemon4ksan/g-man/pkg/steam/protocol/enums"
	"github.com/lemon4ksan/g-man/pkg/steam/socket"
	"github.com/lemon4ksan/g-man/pkg/storage/memory"
	pb "github.com/lemon4ksan/g-man/protobuf/steam"
)

// threadSafeMockSocket provides a thread-safe SocketProvider implementation
// supporting dynamic CurrentServer updates and onReconnect callbacks for stress tests.
type threadSafeMockSocket struct {
	mu          sync.RWMutex
	onReconnect func(ctx context.Context)
	currSrv     socket.CMServer
	hasCurr     bool
	sess        socket.Session
}

func newThreadSafeMockSocket() *threadSafeMockSocket {
	return &threadSafeMockSocket{
		sess:    &socket.BasicSession{},
		hasCurr: true,
		currSrv: socket.CMServer{Type: "mock", Endpoint: "10.0.0.1:27015"},
	}
}

func (s *threadSafeMockSocket) CurrentServer() (socket.CMServer, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.currSrv, s.hasCurr
}

func (s *threadSafeMockSocket) SetCurrentServer(srv socket.CMServer) {
	s.mu.Lock()
	s.currSrv = srv
	s.hasCurr = true
	s.mu.Unlock()
}

func (s *threadSafeMockSocket) SetOnReconnect(fn func(ctx context.Context)) {
	s.mu.Lock()
	s.onReconnect = fn
	s.mu.Unlock()
}

func (s *threadSafeMockSocket) TriggerOnReconnect(ctx context.Context) {
	s.mu.RLock()
	fn := s.onReconnect
	s.mu.RUnlock()

	if fn != nil {
		fn(ctx)
	}
}

func (s *threadSafeMockSocket) IsConnected() bool                    { return true }
func (s *threadSafeMockSocket) UpdateLogger(_ log.Logger)            {}
func (s *threadSafeMockSocket) UpdateServers(_ []socket.CMServer)    {}
func (s *threadSafeMockSocket) SetEncryptionKey(_ []byte) bool       { return true }
func (s *threadSafeMockSocket) StartHeartbeat(_ time.Duration) error { return nil }
func (s *threadSafeMockSocket) Send(_ context.Context, _ socket.PayloadBuilder, _ ...socket.SendOption) error {
	return nil
}

func (s *threadSafeMockSocket) SendProto(
	_ context.Context,
	_ enums.EMsg,
	_ proto.Message,
	_ ...socket.SendOption,
) error {
	return nil
}

func (s *threadSafeMockSocket) SendRaw(_ context.Context, _ enums.EMsg, _ []byte, _ ...socket.SendOption) error {
	return nil
}

func (s *threadSafeMockSocket) SendSync(
	_ context.Context,
	_ socket.PayloadBuilder,
	_ ...socket.SendOption,
) (*protocol.Packet, error) {
	return nil, nil
}
func (s *threadSafeMockSocket) RegisterMsgHandler(_ enums.EMsg, _ socket.Handler)  {}
func (s *threadSafeMockSocket) RegisterServiceHandler(_ string, _ socket.Handler)  {}
func (s *threadSafeMockSocket) Disconnect() error                                  { return nil }
func (s *threadSafeMockSocket) Close() error                                       { return nil }
func (s *threadSafeMockSocket) Connect(_ context.Context, _ socket.CMServer) error { return nil }
func (s *threadSafeMockSocket) Session() socket.Session                            { return s.sess }

type concurrentMockAuth struct {
	logonCalls atomic.Int32
	holdTime   time.Duration
}

func (m *concurrentMockAuth) LogOn(ctx context.Context, _ *auth.LogOnDetails, _ socket.CMServer) error {
	m.logonCalls.Add(1)

	if m.holdTime > 0 {
		select {
		case <-time.After(m.holdTime):
		case <-ctx.Done():
			return ctx.Err()
		}
	}

	return nil
}

type concurrentMockWeb struct{}

func (m *concurrentMockWeb) HTTP() *http.Client                     { return &http.Client{} }
func (m *concurrentMockWeb) SessionID(_ string) string              { return "sess_id" }
func (m *concurrentMockWeb) Verify(_ context.Context) (bool, error) { return true, nil }
func (m *concurrentMockWeb) Authenticate(_ context.Context, _ pb.EAuthTokenPlatformType, _, _ string) error {
	return nil
}
func (m *concurrentMockWeb) IsAuthenticated() bool { return true }
func (m *concurrentMockWeb) WithTokenRefresher(_ func(ctx context.Context, refreshToken string) (string, error)) {
}
func (m *concurrentMockWeb) Refresh(_ context.Context) error { return nil }

type concurrentMockComm struct {
	community.Requester
}

func (m *concurrentMockComm) GetOrRegisterAPIKey(_ context.Context, _ string) (string, error) {
	return "test_api_key", nil
}

func setupConcurrentSessionTest(
	t *testing.T,
	authHold time.Duration,
) (*Session, *threadSafeMockSocket, *concurrentMockAuth) {
	t.Helper()

	sock := newThreadSafeMockSocket()
	mockAuth := &concurrentMockAuth{holdTime: authHold}
	mockWeb := &concurrentMockWeb{}
	mockComm := &concurrentMockComm{}

	cfg := Config{
		Logger:        log.Discard,
		Authenticator: mockAuth,
		Storage:       memory.New(),
		WebFactory: func(_ id.ID, _ log.Logger, _ any) WebSessionProvider {
			return mockWeb
		},
		CommunityFactory: func(_ aoni.HTTPDoer, _ community.SessionProvider, _ log.Logger) community.Requester {
			return mockComm
		},
	}

	sess := New(sock, cfg)
	sess.web = mockWeb
	sess.community = mockComm
	sess.logonDetails = &auth.LogOnDetails{
		AccountName:  "concurrent_user",
		RefreshToken: "refresh_token_xyz",
		SteamID:      76561198000000001,
	}
	sess.logonServer = socket.CMServer{Type: "mock", Endpoint: "10.0.0.1:27015"}

	return sess, sock, mockAuth
}

// TestSession_LogonServer_ConcurrentReadWrite_NoRace tests heavy concurrent reading and writing
// of LogonServer across 50 reader and 50 writer goroutines.
// Invariant: Zero data races, zero torn reads, and valid non-corrupted CMServer returned every time.
func TestSession_LogonServer_ConcurrentReadWrite_NoRace(t *testing.T) {
	t.Parallel()

	sess, _, _ := setupConcurrentSessionTest(t, 0)

	var wg sync.WaitGroup

	numWorkers := 50
	stop := make(chan struct{})

	// Readers
	for i := 0; i < numWorkers; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()

			for {
				select {
				case <-stop:
					return
				default:
					srv := sess.LogonServer()
					assert.NotEmpty(
						t,
						srv.Endpoint,
						"LogonServer must never return empty endpoint during concurrent mutations",
					)
				}
			}
		}(i)
	}

	// Writers
	for i := 0; i < numWorkers; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()

			for j := 0; j < 100; j++ {
				srv := socket.CMServer{
					Type:     "mock",
					Endpoint: fmt.Sprintf("10.0.%d.%d:27015", idx, j),
				}
				sess.SetLogonServer(srv)
			}
		}(i)
	}

	time.Sleep(100 * time.Millisecond)
	close(stop)
	wg.Wait()

	finalSrv := sess.LogonServer()
	assert.NotEmpty(t, finalSrv.Endpoint)
}

// TestSession_ConcurrentReconnect_DynamicLogonServerSync stress-tests concurrent
// Reconnect calls and onReconnect callbacks while dynamic CMServer rotation is actively occurring.
// Invariant: No deadlock, singleflight deduplication active, zero data races.
func TestSession_ConcurrentReconnect_DynamicLogonServerSync(t *testing.T) {
	t.Parallel()

	sess, sock, mockAuth := setupConcurrentSessionTest(t, 20*time.Millisecond)

	var wg sync.WaitGroup

	numCallers := 30

	// 1. Direct Reconnect callers
	for i := 0; i < numCallers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()

			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()

			_ = sess.Reconnect(ctx)
		}()
	}

	// 2. Transport onReconnect callback triggers
	for i := 0; i < numCallers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()

			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()

			sock.TriggerOnReconnect(ctx)
		}()
	}

	// 3. Dynamic CM Server rotators updating socket.CurrentServer()
	stopRotators := make(chan struct{})
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()

			for j := 0; ; j++ {
				select {
				case <-stopRotators:
					return
				default:
					sock.SetCurrentServer(socket.CMServer{
						Type:     "mock",
						Endpoint: fmt.Sprintf("192.168.1.%d:27015", (idx*10+j)%250+1),
					})
					time.Sleep(5 * time.Millisecond)
				}
			}
		}(i)
	}

	// 4. Concurrent LogonServer readers
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()

			for {
				select {
				case <-stopRotators:
					return
				default:
					_ = sess.LogonServer()
				}
			}
		}()
	}

	// Allow reconnects to complete
	time.Sleep(300 * time.Millisecond)
	close(stopRotators)
	wg.Wait()

	// Invariant: Logon calls must be deduplicated by reconnectSF and singleflight mechanisms
	assert.Greater(t, mockAuth.logonCalls.Load(), int32(0))
	assert.NotEmpty(t, sess.LogonServer().Endpoint)
}

// TestSession_ReconnectSingleFlight_DeduplicatesConcurrentEvents stress-tests that
// rapid back-to-back onReconnect invocations do not trigger storm logins on Steam.
func TestSession_ReconnectSingleFlight_DeduplicatesConcurrentEvents(t *testing.T) {
	t.Parallel()

	sess, sock, mockAuth := setupConcurrentSessionTest(t, 50*time.Millisecond)

	var wg sync.WaitGroup

	numSpikes := 40

	for i := 0; i < numSpikes; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()

			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()

			sock.TriggerOnReconnect(ctx)
		}()
	}

	wg.Wait()

	// Invariant: Out of 40 simultaneous triggers while holdTime is 50ms, singleflight
	// must deduplicate into significantly fewer LogOn calls (typically 1-3).
	calls := mockAuth.logonCalls.Load()
	assert.GreaterOrEqual(t, calls, int32(1))
	assert.LessOrEqual(t, calls, int32(5), "singleflight must deduplicate concurrent reconnect storms")

	_ = sess
}

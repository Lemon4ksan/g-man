// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package socket

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"
	"time"

	"google.golang.org/protobuf/proto"

	"github.com/lemon4ksan/g-man/pkg/bus"
	"github.com/lemon4ksan/g-man/pkg/jobs"
	"github.com/lemon4ksan/g-man/pkg/log"
	pb "github.com/lemon4ksan/g-man/pkg/protobuf/steam"
	"github.com/lemon4ksan/g-man/pkg/steam/protocol"
	"github.com/lemon4ksan/g-man/pkg/steam/protocol/enums"
	"github.com/lemon4ksan/g-man/pkg/steam/socket/internal/network"
	"github.com/lemon4ksan/g-man/pkg/steam/socket/internal/session"
)

var (
	// ErrClosed is returned when an operation is attempted on a Socket that
	// has been permanently shut down via Close().
	ErrClosed = errors.New("socket: instance is permanently closed")

	// ErrDisconnected is returned when sending a message requires an active
	// session, but the socket is currently disconnected.
	ErrDisconnected = errors.New("socket: not connected to any CM server")

	// ErrAlreadyConnecting is returned if Connect() is called while a
	// connection attempt is already in progress.
	ErrAlreadyConnecting = errors.New("socket: connection attempt already in progress")

	// ErrAlreadyConnected is returned if Connect() is called on a socket
	// that already has an active session.
	ErrAlreadyConnected = errors.New("socket: already connected")

	// ErrUnsupportedType is returned when the provided [CMServer.Type] does
	// not have a registered dialer in the configuration.
	ErrUnsupportedType = errors.New("socket: unsupported transport protocol")

	// ErrDecompressionLimit is returned when a Multi-message payload
	// exceeds the safety threshold (default 100MB) to prevent OOM attacks.
	ErrDecompressionLimit = errors.New("socket: decompression limit exceeded")

	// ErrDestJobFailed is returned when socket receives EMsg_DestJobFailed message.
	ErrDestJobFailed = errors.New("socket: destination job failed on Steam side")
)

// SessionView provides read-only access to the Steam session state.
type SessionView interface {
	// SteamID returns the 64-bit Steam ID assigned to the session.
	SteamID() uint64
	// SessionID returns the 32-bit session ID assigned by the CM.
	SessionID() int32
	// AccessToken returns the current OAuth2 access token.
	AccessToken() string
	// RefreshToken returns the current OAuth2 refresh token.
	RefreshToken() string

	// IsAuthenticated returns true if the session has been assigned both
	// a SessionID by the CM and a valid SteamID.
	IsAuthenticated() bool
}

// SessionWriter provides message transmission capabilities.
type SessionWriter interface {
	// Send writes the provided payload to the underlying network transport.
	Send(ctx context.Context, data []byte) error
}

// SessionController provides write access to modify the session's internal state and lifecycle.
type SessionController interface {
	// SetSteamID updates the session's Steam ID.
	SetSteamID(uint64)
	// SetSessionID updates the session's ID assigned by the CM.
	SetSessionID(int32)
	// SetRefreshToken updates the OAuth2 refresh token.
	SetRefreshToken(string)
	// SetAccessToken updates the OAuth2 access token.
	SetAccessToken(string)

	// SetEncryptionKey upgrades the underlying connection to use Steam's
	// symmetric encryption if the underlying connection supports it.
	SetEncryptionKey(key []byte) bool

	// Close terminates the underlying network connection.
	Close() error
}

// Session represents the complete lifecycle and state of a connection
// to a Steam Connection Manager (CM).
type Session interface {
	SessionView
	SessionWriter
	SessionController
}

// Handler defines a callback function for processing an incoming, fully-parsed Steam packet.
type Handler func(p *protocol.Packet)

// CMServer represents a Steam Connection Manager server endpoint.
type CMServer struct {
	Endpoint string  // Host:port address.
	Type     string  // Connection protocol: "tcp", "websockets", or "netfilter".
	Load     float64 // Server load metric (lower is better).
	Realm    string  // Steam realm, e.g., "steamglobal".
}

// ConnectionDialer defines a function signature for establishing network connections.
type ConnectionDialer func(ctx context.Context, nh network.Handler, logger log.Logger, endpoint string) (network.Connection, error)

// DefaultDialers returns a fresh map of standard transport dialers.
// Returning a map from a function prevents accidental global state mutation.
func DefaultDialers() map[string]ConnectionDialer {
	return map[string]ConnectionDialer{
		"tcp": func(ctx context.Context, nh network.Handler, l log.Logger, s string) (network.Connection, error) {
			return network.NewTCP(ctx, nh, l, s)
		},
		"websockets": func(ctx context.Context, nh network.Handler, l log.Logger, s string) (network.Connection, error) {
			return network.NewWS(ctx, nh, l, s, nil)
		},
		"netfilter": func(ctx context.Context, nh network.Handler, l log.Logger, s string) (network.Connection, error) {
			return network.NewTCP(ctx, nh, l, s) // netfilter is effectively TCP
		},
	}
}

// Register provides a type-safe, generic wrapper for registering message handlers.
// It automatically handles Protobuf unmarshaling and error logging.
//
// Example:
//
//	Register(s, eMsg, func() *pb.Response { return new(pb.Response) }, func(r *pb.Response) { ... })
func Register[T proto.Message](s *Socket, emsg enums.EMsg, factory func() T, handler func(T)) {
	s.RegisterMsgHandler(emsg, func(p *protocol.Packet) {
		msg := factory()
		if err := proto.Unmarshal(p.Payload, msg); err == nil {
			handler(msg)
		}
	})
}

// ReconnectPolicy defines how to respond to network issues.
type ReconnectPolicy struct {
	MaxAttempts    int
	InitialBackoff time.Duration
	MaxBackoff     time.Duration
	BackoffFactor  float64
	ServerSelector func([]CMServer) CMServer // Round-robin / lowest load
}

// DefaultReconnectPolicy returns the recommended default configuration.
func DefaultReconnectPolicy() ReconnectPolicy {
	return ReconnectPolicy{
		MaxAttempts:    10,
		InitialBackoff: 1 * time.Second,
		MaxBackoff:     30 * time.Second,
		BackoffFactor:  2.0,
		ServerSelector: func(servers []CMServer) CMServer {
			if len(servers) == 0 {
				return CMServer{}
			}

			return servers[0]
		},
	}
}

// Config holds configuration parameters for the Socket.
type Config struct {
	EventChanSize   int                         // Buffer size for the internal message-passing channel.
	WorkerCount     int                         // Number of parallel workers processing incoming packets.
	MaxJobs         int                         // Maximum concurrent jobs.
	Dialers         map[string]ConnectionDialer // Map of protocol names to dialer functions.
	ReconnectPolicy ReconnectPolicy
	ConnectTimeout  time.Duration
}

// DefaultConfig returns the recommended default configuration.
func DefaultConfig() Config {
	return Config{
		EventChanSize:   1000, // Increased to handle bursts of multi-messages
		MaxJobs:         1000,
		WorkerCount:     runtime.NumCPU(),
		Dialers:         DefaultDialers(),
		ReconnectPolicy: DefaultReconnectPolicy(),
		ConnectTimeout:  30 * time.Second,
	}
}

// State represents the current lifecycle state of the socket.
type State int32

const (
	// StateDisconnected indicates the socket is in it's default not connected state.
	StateDisconnected State = iota
	// StateConnecting indicates the socket is in the process of connecting.
	StateConnecting
	// StateConnected indicates the socket has an active connection.
	StateConnected
	// StateDisconnecting indicates the socket is shutting down the connection.
	StateDisconnecting
)

func (s State) String() string {
	switch s {
	case StateDisconnected:
		return "disconnected"
	case StateConnecting:
		return "connecting"
	case StateConnected:
		return "connected"
	case StateDisconnecting:
		return "disconnecting"
	default:
		return "unknown"
	}
}

// Option defines a functional option for configuring the Socket.
type Option func(*Socket)

// WithBus sets a custom event bus for the socket.
func WithBus(b *bus.Bus) Option {
	return func(s *Socket) { s.bus = b }
}

// WithLogger sets a custom logger.
func WithLogger(l log.Logger) Option {
	return func(s *Socket) { s.logger = l.With(log.Module("sock")) }
}

// WithSession injects a custom pre-configured session.
func WithSession(session Session) Option {
	return func(s *Socket) { s.setSession(session) }
}

// Socket is the core network engine. It orchestrates the connection lifecycle,
// message routing, job tracking, and session management. It is designed to be thread-safe.
type Socket struct {
	config Config
	state  atomic.Int32

	// Global dependencies
	logger     log.Logger
	bus        *bus.Bus
	jobManager *jobs.Manager[*protocol.Packet]
	session    atomic.Pointer[sessionContainer]

	ctx             atomic.Value // Context tied to the active connection
	cancel          atomic.Value // Cancels the active connection
	heartbeatActive atomic.Bool

	// Message routing
	handlersMu        sync.RWMutex
	handlers          map[enums.EMsg]Handler
	serviceHandlersMu sync.RWMutex
	serviceHandlers   map[string]Handler

	// Concurrency and pooling
	msgCh      chan *protocol.Packet
	bufferPool sync.Pool
	workerWg   sync.WaitGroup

	// Socket lifecycle
	closeOnce sync.Once
	done      chan struct{}

	serversMu  sync.RWMutex
	servers    []CMServer
	lastServer CMServer
}

// NewSocket initializes a new Socket instance with the given config and options.
func NewSocket(cfg Config, opts ...Option) *Socket {
	if cfg.Dialers == nil {
		cfg.Dialers = DefaultDialers()
	}

	if cfg.WorkerCount <= 0 {
		cfg.WorkerCount = 1
	}

	s := &Socket{
		config:          cfg,
		logger:          log.Discard,
		jobManager:      jobs.NewManager[*protocol.Packet](cfg.MaxJobs),
		done:            make(chan struct{}),
		handlers:        make(map[enums.EMsg]Handler),
		serviceHandlers: make(map[string]Handler),
		msgCh:           make(chan *protocol.Packet, cfg.EventChanSize),
		bufferPool: sync.Pool{
			New: func() any { return new(bytes.Buffer) },
		},
	}

	for _, opt := range opts {
		opt(s)
	}

	if s.bus == nil {
		s.bus = bus.New()
	}

	s.setState(StateDisconnected)

	s.RegisterMsgHandler(enums.EMsg_Multi, s.handleMulti)
	s.RegisterMsgHandler(enums.EMsg_ServiceMethod, s.handleService)

	return s
}

// UpdateServers updates the list of available CM servers.
func (s *Socket) UpdateServers(servers []CMServer) {
	s.serversMu.Lock()
	defer s.serversMu.Unlock()

	s.servers = servers
}

// IsConnected checks if current state is [StateConnected].
func (s *Socket) IsConnected() bool {
	return s.State() == StateConnected
}

// Bus returns the underlying event dispatcher.
func (s *Socket) Bus() *bus.Bus {
	return s.bus
}

// Session returns the current active session, if any.
func (s *Socket) Session() Session {
	container := s.session.Load()
	if container == nil {
		return nil
	}

	return container.sess
}

// State returns the current connection state.
func (s *Socket) State() State {
	return State(s.state.Load())
}

// Done returns a channel that is closed when the socket is permanently closed.
func (s *Socket) Done() <-chan struct{} { return s.done }

// RegisterMsgHandler registers a callback for a specific EMsg.
// Passing nil will remove the existing handler.
func (s *Socket) RegisterMsgHandler(eMsg enums.EMsg, handler Handler) {
	s.handlersMu.Lock()
	defer s.handlersMu.Unlock()

	if handler == nil {
		delete(s.handlers, eMsg)
	} else {
		s.handlers[eMsg] = handler
	}
}

// RegisterServiceHandler registers a callback for a specific Unified Service Method.
// Example method: "Player.GetGameBadgeLevels#1". Passing nil will remove the existing handler.
func (s *Socket) RegisterServiceHandler(method string, handler Handler) {
	s.serviceHandlersMu.Lock()
	defer s.serviceHandlersMu.Unlock()

	if handler == nil {
		delete(s.serviceHandlers, method)
	} else {
		s.serviceHandlers[method] = handler
	}
}

// Connect attempts to establish a connection to the provided CM server.
// This is a non-blocking call that starts background processes.
// The connection state can be monitored via the event bus or State().
// Any existing connection will be closed before a new one is established.
func (s *Socket) Connect(server CMServer) error {
	if s.State() == StateConnected {
		return ErrAlreadyConnected
	}

	if !s.state.CompareAndSwap(int32(StateDisconnected), int32(StateConnecting)) {
		return ErrAlreadyConnecting
	}

	s.lastServer = server

	dialer, ok := s.config.Dialers[server.Type]
	if !ok {
		s.setState(StateDisconnected)
		return fmt.Errorf("%w: %s", ErrUnsupportedType, server.Type)
	}

	start := time.Now()

	connCtx, connCancel := context.WithCancel(context.Background())
	s.ctx.Store(connCtx)
	s.cancel.Store(connCancel)

	dialCtx, dialCancel := context.WithTimeout(connCtx, s.config.ConnectTimeout)
	defer dialCancel()

	conn, err := dialer(dialCtx, inboundHandler{sock: s}, s.logger, server.Endpoint)
	if err != nil {
		s.setState(StateDisconnected)
		return fmt.Errorf("socket: transport dial failed: %w", err)
	}

	sLog := s.logger.With(log.String("endpoint", server.Endpoint), log.Int64("conn_id", conn.ID()))
	ls := newLoggedSession(session.New(conn), sLog)
	s.setSession(ls)

	s.setState(StateConnected)
	s.bus.Publish(&ConnectedEvent{Server: server.Endpoint})
	s.logger.Info("Successfully connected", log.Duration("latency", time.Since(start)))

	s.startWorkers()

	return nil
}

// StartHeartbeat begins sending periodic heartbeat messages to keep the connection alive.
// It runs in a background goroutine and stops automatically when the connection is closed
// or the context is cancelled. Only one heartbeat loop can be active at a time. Calling
// this while a heartbeat is already running will result in no-op.
func (s *Socket) StartHeartbeat(interval time.Duration) {
	if !s.heartbeatActive.CompareAndSwap(false, true) {
		s.logger.Warn("Heartbeat already active, ignoring duplicate start")
		return
	}

	ctx, ok := s.ctx.Load().(context.Context)
	if !ok || ctx == nil {
		s.heartbeatActive.Store(false)
		s.logger.Warn("StartHeartbeat called without an active connection")
		return
	}

	s.workerWg.Go(func() {
		defer s.heartbeatActive.Store(false)

		s.logger.Debug("Heartbeat started", log.Duration("interval", interval))

		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				if s.State() != StateConnected {
					return
				}

				if err := s.SendProto(ctx, enums.EMsg_ClientHeartBeat, &pb.CMsgClientHeartBeat{}); err != nil {
					s.logger.Warn("Heartbeat failed", log.Err(err))
				}

			case <-ctx.Done():
				s.logger.Debug("Heartbeat stopped due to connection closure")
				return
			case <-s.done:
				return
			}
		}
	})
}

// ClearHandlers resets the handlers map.
func (s *Socket) ClearHandlers() {
	s.handlersMu.Lock()
	clear(s.handlers)
	s.handlersMu.Unlock()

	s.serviceHandlersMu.Lock()
	clear(s.serviceHandlers)
	s.serviceHandlersMu.Unlock()
}

// Disconnect gracefully closes the current active connection,
// waits for workers to finish, and resets the session.
func (s *Socket) Disconnect() {
	if s.State() == StateDisconnected {
		return
	}

	s.setState(StateDisconnecting)

	if cancelFunc, ok := s.cancel.Load().(context.CancelFunc); ok {
		cancelFunc()
	}

	if sess := s.Session(); sess != nil {
		_ = sess.Close()
	}

	s.drainMsgChannel()
	s.setSession(nil)
	s.setState(StateDisconnected)

	s.bus.Publish(&DisconnectedEvent{})
	s.logger.Info("Client disconnected")
}

// Close permanently shuts down the Socket, its workers, and all associated resources.
// After Close is called, the Socket instance should not be reused.
func (s *Socket) Close() error {
	var err error
	s.closeOnce.Do(func() {
		s.ClearHandlers()
		s.Disconnect()
		close(s.done)
		s.workerWg.Wait()
		err = s.jobManager.Close()
	})

	return err
}

func (s *Socket) getContext() context.Context {
	ptr := s.ctx.Load()
	if ptr == nil {
		return context.Background()
	}

	return ptr.(context.Context)
}

func (s *Socket) setSession(sess Session) {
	if sess == nil {
		s.session.Store(nil)
		return
	}

	s.session.Store(&sessionContainer{sess: sess})
}

func (s *Socket) setState(new State) State {
	old := State(s.state.Swap(int32(new)))
	if old != new {
		s.bus.Publish(&StateEvent{Old: old, New: new})
	}

	return old
}

func (s *Socket) drainMsgChannel() {
	s.logger.Debug("Draining message channel...")

	for {
		select {
		case <-s.msgCh:
		default:
			return
		}
	}
}

func (s *Socket) recoverPanic(emsg enums.EMsg) {
	if r := recover(); r != nil {
		s.logger.Error("Socket recovered from panic", log.EMsg(emsg), log.Any("panic", r))
	}
}

func (s *Socket) startWorkers() {
	for range s.config.WorkerCount {
		s.workerWg.Go(s.worker)
	}
}

func (s *Socket) worker() {
	for {
		select {
		case pkt, ok := <-s.msgCh:
			if !ok {
				return
			}

			s.routePacket(pkt)

		case <-s.done:
			return
		}
	}
}

func (s *Socket) routePacket(packet *protocol.Packet) {
	l := s.logger.With(
		log.EMsg(packet.EMsg),
		log.JobID(packet.GetTargetJobID()),
	)

	if ah, ok := packet.Header.(protocol.AuthorizedHeader); ok {
		if sess := s.Session(); sess != nil {
			if steamID := ah.GetSteamID(); steamID != 0 {
				sess.SetSteamID(steamID)
			}

			if sessionID := ah.GetSessionID(); sessionID != 0 {
				sess.SetSessionID(sessionID)
			}
		}
	}

	if s.handleJobResponse(packet) {
		l.Debug("Packet routed to job callback")
		return
	}

	s.handlersMu.RLock()
	handler, ok := s.handlers[packet.EMsg]
	s.handlersMu.RUnlock()

	if ok {
		l.Debug("Packet routed to handler")
		func() {
			defer s.recoverPanic(packet.EMsg)

			handler(packet)
		}()
	} else {
		l.Debug("Unhandled message", log.EMsg(packet.EMsg))
	}
}

func (s *Socket) handleRemoteClose() {
	old := s.setState(StateDisconnected)
	if old == StateDisconnecting || old == StateDisconnected {
		return
	}

	if cancelFunc, ok := s.cancel.Load().(context.CancelFunc); ok {
		cancelFunc()
	}

	s.bus.Publish(&DisconnectedEvent{
		Error: fmt.Errorf("%w: remote host closed connection", ErrDisconnected),
	})

	if s.config.ReconnectPolicy.MaxAttempts > 0 {
		s.workerWg.Go(s.reconnectLoop)
	}
}

func (s *Socket) reconnectLoop() {
	policy := s.config.ReconnectPolicy
	backoff := policy.InitialBackoff

	s.logger.Info("Starting reconnection loop")

	for attempt := 1; attempt <= policy.MaxAttempts; attempt++ {
		select {
		case <-s.done:
			return
		default:
		}

		s.serversMu.RLock()
		targetServer := policy.ServerSelector(s.servers)
		s.serversMu.RUnlock()

		if targetServer.Endpoint == "" {
			targetServer = s.lastServer
		}

		s.bus.Publish(&ReconnectAttemptEvent{
			Attempt: attempt,
			Delay:   backoff,
		})

		s.logger.Debug("Reconnection attempt",
			log.Int("attempt", attempt),
			log.Duration("delay", backoff),
			log.String("endpoint", targetServer.Endpoint),
		)

		if err := s.Connect(targetServer); err == nil {
			s.logger.Info("Successfully reconnected", log.Int("attempts", attempt))
			return
		}

		timer := time.NewTimer(backoff)
		select {
		case <-timer.C:
			backoff = min(time.Duration(float64(backoff)*policy.BackoffFactor), policy.MaxBackoff)
		case <-s.done:
			timer.Stop()
			s.logger.Debug("Reconnection loop aborted: socket closed")
			return
		}
	}

	s.logger.Error("Reconnection failed after maximum attempts", log.Int("max_attempts", policy.MaxAttempts))
	s.setState(StateDisconnected)
}

type sessionContainer struct {
	sess Session
}

type inboundHandler struct {
	sock *Socket
}

func (h inboundHandler) OnNetMessage(msg network.NetMessage) {
	h.sock.processSingle(bytes.NewReader(msg))
}

func (h inboundHandler) OnNetError(err error) {
	h.sock.logger.Error("Network error", log.Err(err))
	h.sock.bus.Publish(&NetworkErrorEvent{Error: err})
}

func (h inboundHandler) OnNetClose() {
	h.sock.handleRemoteClose()
}

type logged struct {
	Session
	logger log.Logger
}

func newLoggedSession(s Session, l log.Logger) *logged {
	return &logged{
		Session: s,
		logger:  l,
	}
}

func (l *logged) Send(ctx context.Context, data []byte) error {
	l.logger.Debug("Writing to socket",
		log.Int("size_bytes", len(data)),
		log.Uint64("steam_id", l.SteamID()),
	)

	err := l.Session.Send(ctx, data)
	if err != nil {
		l.logger.Error("Failed to write to socket",
			log.Err(err),
			log.Int("size_bytes", len(data)),
		)
	}

	return err
}

func (l *logged) SetEncryptionKey(key []byte) bool {
	l.logger.Debug("Applying channel encryption key", log.Int("key_len", len(key)))

	if l.Session.SetEncryptionKey(key) {
		l.logger.Info("Channel encryption established successfully")
		return true
	}

	l.logger.Warn("Channel encryption skipped: connection not encryptable")

	return false
}

func (l *logged) Close() error {
	l.logger.Debug("Closing session connection",
		log.Uint64("steam_id", l.SteamID()),
		log.Int32("session_id", l.SessionID()),
	)

	return l.Session.Close()
}

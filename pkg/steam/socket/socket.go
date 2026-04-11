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

	"github.com/lemon4ksan/g-man/pkg/bus"
	"github.com/lemon4ksan/g-man/pkg/jobs"
	"github.com/lemon4ksan/g-man/pkg/log"
	pb "github.com/lemon4ksan/g-man/pkg/protobuf/steam"
	"github.com/lemon4ksan/g-man/pkg/steam/protocol"
	"github.com/lemon4ksan/g-man/pkg/steam/protocol/enums"
	"github.com/lemon4ksan/g-man/pkg/steam/socket/internal/network"
	"github.com/lemon4ksan/g-man/pkg/steam/socket/internal/session"

	"google.golang.org/protobuf/proto"
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

// Session represents the complete lifecycle and state of a connection
// to a Steam Connection Manager (CM).
type Session = session.Session

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
type ConnectionDialer func(nh network.Handler, logger log.Logger, endpoint string) (network.Connection, error)

// DefaultDialers returns a fresh map of standard transport dialers.
// Returning a map from a function prevents accidental global state mutation.
func DefaultDialers() map[string]ConnectionDialer {
	return map[string]ConnectionDialer{
		"tcp": func(nh network.Handler, l log.Logger, s string) (network.Connection, error) {
			return network.NewTCP(nh, l, s)
		},
		"websockets": func(nh network.Handler, l log.Logger, s string) (network.Connection, error) {
			return network.NewWS(nh, l, s, nil)
		},
		"netfilter": func(nh network.Handler, l log.Logger, s string) (network.Connection, error) {
			return network.NewTCP(nh, l, s) // netfilter is effectively TCP
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

// Config holds configuration parameters for the Socket.
type Config struct {
	EventChanSize int                         // Buffer size for the internal message-passing channel.
	WorkerCount   int                         // Number of parallel workers processing incoming packets.
	Dialers       map[string]ConnectionDialer // Map of protocol names to dialer functions.
}

// DefaultConfig returns the recommended default configuration.
func DefaultConfig() Config {
	return Config{
		EventChanSize: 1000, // Increased to handle bursts of multi-messages
		WorkerCount:   runtime.NumCPU(),
		Dialers:       DefaultDialers(),
	}
}

// State represents the current lifecycle state of the socket.
type State int32

const (
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

	connCtx    atomic.Value // Context tied to the active connection
	connCancel atomic.Value // Cancels the active connection

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
		jobManager:      jobs.NewManager[*protocol.Packet](1000),
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
		s.bus = bus.NewBus()
	}

	s.setState(StateDisconnected)

	s.RegisterMsgHandler(enums.EMsg_Multi, s.handleMulti)
	s.RegisterMsgHandler(enums.EMsg_ServiceMethod, s.handleService)

	return s
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
// This is a non-blocking call that starts background processes. The connection
// state can be monitored via the event bus or State().
func (s *Socket) Connect(ctx context.Context, server CMServer) error {
	if !s.canConnect() {
		return ErrAlreadyConnected
	}

	dialer, ok := s.config.Dialers[server.Type]
	if !ok {
		s.setState(StateDisconnected)
		return fmt.Errorf("%w: %s", ErrUnsupportedType, server.Type)
	}

	start := time.Now()

	conn, err := dialer(inboundHandler{sock: s}, s.logger, server.Endpoint)
	if err != nil {
		s.setState(StateDisconnected)
		return fmt.Errorf("socket: transport dial failed: %w", err)
	}

	sLog := s.logger.With(log.String("endpoint", server.Endpoint), log.Int64("conn_id", conn.ID()))
	ls := session.NewLogged(session.New(conn), sLog)
	s.setSession(ls)

	connCtx, connCancel := context.WithCancel(context.Background())
	s.connCtx.Store(connCtx)
	s.connCancel.Store(connCancel)

	s.setState(StateConnected)
	s.bus.Publish(&ConnectedEvent{Server: server.Endpoint})
	s.logger.Info("Successfully connected", log.Duration("latency", time.Since(start)))

	s.startWorkers(connCtx)
	return nil
}

// StartHeartbeat begins sending periodic heartbeat messages to keep the connection alive.
// It runs in a background goroutine and stops automatically when the connection is closed.
func (s *Socket) StartHeartbeat(interval time.Duration) {
	ctx := s.connCtx.Load().(context.Context)

	if ctx == nil {
		s.logger.Warn("StartHeartbeat called without an active connection")
		return
	}

	s.workerWg.Go(func() {
		s.logger.Debug("Heartbeat started", log.Duration("interval", interval))
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				if s.State() != StateConnected {
					return
				}
				s.SendProto(ctx, enums.EMsg_ClientHeartBeat, &pb.CMsgClientHeartBeat{})
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

// Disconnect gracefully closes the current active connection, waits for workers to finish,
// and resets the session.
func (s *Socket) Disconnect() {
	if s.State() == StateDisconnected {
		return
	}

	s.setState(StateDisconnecting)

	if cancelFunc, ok := s.connCancel.Load().(context.CancelFunc); ok {
		cancelFunc()
	}

	s.workerWg.Wait()
	s.drainMsgChannel()

	s.setSession(nil)
	s.bus.Publish(&DisconnectedEvent{})
	s.setState(StateDisconnected)
	s.logger.Info("Client disconnected")
}

// Close permanently shuts down the Socket, its workers, and all associated resources.
// After Close is called, the Socket instance should not be reused.
func (s *Socket) Close() error {
	s.closeOnce.Do(func() {
		s.ClearHandlers()
		s.Disconnect()
		close(s.done)     // Signal workers to stop
		s.workerWg.Wait() // Wait for graceful shutdown
		s.jobManager.Close()
	})
	return nil
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

func (s *Socket) setState(new State) State {
	old := State(s.state.Swap(int32(new)))
	if old != new {
		s.bus.Publish(&StateEvent{Old: old, New: new})
	}
	return old
}

func (s *Socket) recoverPanic(emsg enums.EMsg) {
	if r := recover(); r != nil {
		s.logger.Error("Socket recovered from panic", log.EMsg(emsg), log.Any("panic", r))
	}
}

func (s *Socket) startWorkers(ctx context.Context) {
	for range s.config.WorkerCount {
		s.workerWg.Add(1)
		go s.worker(ctx)
	}
}

// worker is the core of the concurrent processing model. It runs in a loop,
// consuming packets from the message channel and passing them to the router.
func (s *Socket) worker(ctx context.Context) {
	defer s.workerWg.Done()
	for {
		select {
		case pkt, ok := <-s.msgCh:
			if !ok {
				return
			} // msgCh closed during socket Close()
			s.routePacket(pkt)
		case <-ctx.Done():
			s.logger.Debug("Worker stopped due to disconnect")
			return
		case <-s.done:
			s.logger.Debug("Worker stopped due to socket close")
			return
		}
	}
}

func (s *Socket) canConnect() bool {
	return s.state.CompareAndSwap(int32(StateDisconnected), int32(StateConnecting))
}

// routePacket is the central dispatcher for all incoming messages.
// It prioritizes routing in the following order:
// 1. Job Response: If the packet has a target Job ID, it's resolved.
// 2. Registered Handler: If a handler for the packet's EMsg exists, it's invoked.
// 3. Unhandled: Otherwise, the packet is logged as unhandled.
func (s *Socket) routePacket(packet *protocol.Packet) {
	l := s.logger.With(
		log.EMsg(packet.EMsg),
		log.JobID(packet.GetTargetJobID()),
	)

	// Update session state if authorized headers are present.
	if ah, ok := packet.Header.(protocol.AuthorizedHeader); ok {
		sess := s.Session()
		if sess != nil {
			if steamID := ah.GetSteamID(); steamID != 0 {
				sess.SetSteamID(steamID)
			}
			if sessionID := ah.GetSessionID(); sessionID != 0 {
				sess.SetSessionID(sessionID)
			}
		}
	}

	// Check if this is a response to an ongoing job.
	if s.handleJobResponse(packet) {
		l.Debug("Packet routed to job callback")
		return // Packet consumed by job callback
	}

	// Route to registered generic handlers (including Multi and ServiceMethods).
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
	if cancelFunc, ok := s.connCancel.Load().(context.CancelFunc); ok {
		cancelFunc()
	}

	old := s.setState(StateDisconnected)
	if old == StateDisconnecting {
		s.bus.Publish(&DisconnectedEvent{})
	} else {
		s.bus.Publish(&DisconnectedEvent{Error: fmt.Errorf("%w: connection closed unexpectedly", ErrDisconnected)})
	}
}

func (s *Socket) getContext() context.Context {
	ptr := s.connCtx.Load()
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

type sessionContainer struct {
	sess Session
}

// inboundHandler acts as the adapter (or "bridge") between the low-level `network`
// package and the higher-level `socket` logic. It implements [network.Handler].
type inboundHandler struct {
	sock *Socket
}

// OnNetMessage translates a raw network message into a logical packet and queues it for processing.
func (h inboundHandler) OnNetMessage(msg network.NetMessage) {
	h.sock.processSingle(bytes.NewReader(msg))
}

// OnNetError pushes a network-layer error onto the event bus.
func (h inboundHandler) OnNetError(err error) {
	h.sock.logger.Error("Network error", log.Err(err))
	h.sock.bus.Publish(&NetworkErrorEvent{Error: err})
}

// OnNetClose triggers the socket's disconnect logic when the underlying connection is lost.
func (h inboundHandler) OnNetClose() {
	h.sock.handleRemoteClose()
}

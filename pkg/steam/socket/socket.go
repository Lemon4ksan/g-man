// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package socket provides a flexible, event-driven network socket layer
// for communicating with Steam Connection Manager (CM) servers.
package socket

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"reflect"
	"runtime"
	"sync"
	"sync/atomic"
	"time"

	"github.com/lemon4ksan/g-man/pkg/jobs"
	"github.com/lemon4ksan/g-man/pkg/log"
	"github.com/lemon4ksan/g-man/pkg/steam/bus"
	"github.com/lemon4ksan/g-man/pkg/steam/network"
	pb "github.com/lemon4ksan/g-man/pkg/steam/protobuf"
	"github.com/lemon4ksan/g-man/pkg/steam/protocol"

	"google.golang.org/protobuf/proto"
)

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
			return network.NewTCPConnection(nh, l, s)
		},
		"websockets": func(nh network.Handler, l log.Logger, s string) (network.Connection, error) {
			return network.NewWSConnection(nh, l, s)
		},
		"netfilter": func(nh network.Handler, l log.Logger, s string) (network.Connection, error) {
			return network.NewTCPConnection(nh, l, s) // netfilter is effectively TCP
		},
	}
}

// TypedHandler defines a generic callback for a specific, unmarshaled Protobuf message type.
type TypedHandler[T proto.Message] func(body T)

// RegisterMsgHandlerTyped provides a type-safe, generic wrapper for registering message handlers.
// It automatically handles Protobuf unmarshaling and error logging.
//
// Example:
//
//	RegisterMsgHandlerTyped(s, protocol.EMsg_ClientLogOnResponse, func(resp *pb.CMsgClientLogonResponse) {
//	   fmt.Println("Logon result:", resp.GetEresult())
//	})
func RegisterMsgHandlerTyped[T proto.Message](s *Socket, eMsg protocol.EMsg, handler TypedHandler[T]) {
	var zero T
	typ := reflect.TypeOf(zero).Elem()

	wrapper := func(p *protocol.Packet) {
		body := reflect.New(typ).Interface().(T)

		if err := proto.Unmarshal(p.Payload, body); err != nil {
			s.logger.Error("Failed to unmarshal", log.Err(err))
			return
		}
		handler(body)
	}
	s.RegisterMsgHandler(eMsg, wrapper)
}

// Config holds configuration parameters for the Socket.
type Config struct {
	ConnectTimeout time.Duration               // Max time to wait for a network connection to establish.
	EventChanSize  int                         // Buffer size for the internal message-passing channel.
	WorkerCount    int                         // Number of parallel workers processing incoming packets.
	DebugEvents    bool                        // If true, enables verbose event logging.
	Dialers        map[string]ConnectionDialer // Map of protocol names to dialer functions.
}

// DefaultConfig returns the recommended default configuration.
func DefaultConfig() Config {
	return Config{
		ConnectTimeout: 10 * time.Second,
		EventChanSize:  1000, // Increased to handle bursts of multi-messages
		WorkerCount:    runtime.NumCPU(),
		DebugEvents:    true,
		Dialers:        DefaultDialers(),
	}
}

// State represents the current lifecycle state of the socket.
type State int32

const (
	// StateDisconnected indicates the socket is not connected.
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
	return func(s *Socket) { s.logger = l.WithModule("sock") }
}

// WithSession injects a custom pre-configured session.
func WithSession(session Session) Option {
	return func(s *Socket) { s.session.Store(&session) }
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
	session    atomic.Pointer[Session]

	mu         sync.RWMutex
	connCtx    context.Context    // Context tied to the active connection
	connCancel context.CancelFunc // Cancels the active connection

	// Message routing
	handlersMu        sync.RWMutex
	handlers          map[protocol.EMsg]Handler
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
		handlers:        make(map[protocol.EMsg]Handler),
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

	s.RegisterMsgHandler(protocol.EMsg_Multi, s.handleMulti)
	s.RegisterMsgHandler(protocol.EMsg_ServiceMethod, s.handleServiceMethod)

	return s
}

// Bus returns the underlying event dispatcher.
func (s *Socket) Bus() *bus.Bus {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.bus
}

// Session returns the current active session, if any.
func (s *Socket) Session() Session {
	sess := s.session.Load()
	if sess == nil {
		return nil
	}
	return *sess
}

// State returns the current connection state.
func (s *Socket) State() State {
	return State(s.state.Load())
}

// Done returns a channel that is closed when the socket is permanently closed.
func (s *Socket) Done() <-chan struct{} { return s.done }

// RegisterMsgHandler registers a callback for a specific EMsg.
// Passing nil will remove the existing handler.
func (s *Socket) RegisterMsgHandler(eMsg protocol.EMsg, handler Handler) {
	s.handlersMu.Lock()
	defer s.handlersMu.Unlock()

	if handler == nil {
		delete(s.handlers, eMsg)
	} else {
		s.handlers[eMsg] = handler
	}
}

// RegisterServiceHandler registers a callback for a specific Unified Service Method.
// Example method: "Player.GetGameBadgeLevels#1"
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
	if !s.state.CompareAndSwap(int32(StateDisconnected), int32(StateConnecting)) {
		return errors.New("socket: already connecting or connected")
	}

	dialer, ok := s.config.Dialers[server.Type]
	if !ok {
		s.setState(StateDisconnected)
		return fmt.Errorf("unsupported connection type: %s", server.Type)
	}

	start := time.Now()

	conn, err := dialer(inboundHandler{sock: s}, s.logger, server.Endpoint)
	if err != nil {
		s.setState(StateDisconnected)
		return fmt.Errorf("transport dial failed: %w", err)
	}

	baseSession := NewBaseSession(conn)
	sessionLogger := s.logger.With(
		log.String("endpoint", server.Endpoint),
		log.Int64("conn_id", conn.ID()),
	)

	var ls Session = NewLoggedSession(baseSession, sessionLogger)
	s.session.Store(&ls)

	connCtx, connCancel := context.WithCancel(ctx)

	s.mu.Lock()
	s.connCtx, s.connCancel = connCtx, connCancel
	s.mu.Unlock()

	s.startWorkers(connCtx, s.config.WorkerCount)

	s.setState(StateConnected)
	s.bus.Publish(&ConnectedEvent{Server: server.Endpoint})
	s.logger.Info("Successfully connected", log.Duration("latency", time.Since(start)))

	return nil
}

// StartHeartbeat begins sending periodic heartbeat messages to keep the connection alive.
// It runs in a background goroutine and stops automatically when the connection is closed.
func (s *Socket) StartHeartbeat(interval time.Duration) {
	s.mu.RLock()
	ctx := s.connCtx
	s.mu.RUnlock()

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
				s.SendProto(ctx, protocol.EMsg_ClientHeartBeat, &pb.CMsgClientHeartBeat{})
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

	s.mu.Lock()
	if s.connCancel != nil {
		s.connCancel()
	}
	s.mu.Unlock()

	s.workerWg.Wait()
	s.drainMsgChannel()

	s.session.Store(nil)
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

func (s *Socket) recoverPanic(emsg protocol.EMsg) {
	if r := recover(); r != nil {
		s.logger.Error("Socket recovered from panic",
			log.String("emsg", emsg.String()),
			log.Any("panic", r),
		)
	}
}

func (s *Socket) startWorkers(ctx context.Context, count int) {
	for range count {
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

// routePacket is the central dispatcher for all incoming messages.
// It prioritizes routing in the following order:
// 1. Job Response: If the packet has a target Job ID, it's resolved.
// 2. Registered Handler: If a handler for the packet's EMsg exists, it's invoked.
// 3. Unhandled: Otherwise, the packet is logged as unhandled.
func (s *Socket) routePacket(packet *protocol.Packet) {
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
		return // Packet consumed by job callback
	}

	// Route to registered generic handlers (including Multi and ServiceMethods).
	s.handlersMu.RLock()
	handler, ok := s.handlers[packet.EMsg]
	s.handlersMu.RUnlock()

	if ok {
		func() {
			defer s.recoverPanic(packet.EMsg)
			handler(packet)
		}()
	} else {
		s.logger.Debug("Unhandled message", log.String("emsg", packet.EMsg.String()))
	}
}

// handleMulti is the built-in handler for EMsg_Multi, which contains multiple
// nested or compressed packets.
func (s *Socket) handleMulti(packet *protocol.Packet) {
	msg := &pb.CMsgMulti{}
	if err := proto.Unmarshal(packet.Payload, msg); err != nil {
		s.logger.Error("Failed to unmarshal CMsgMulti", log.Err(err))
		return
	}

	payload := msg.GetMessageBody()
	if msg.GetSizeUnzipped() > 0 {
		var err error
		payload, err = s.decompressPayload(payload, int64(msg.GetSizeUnzipped()))
		if err != nil {
			s.logger.Error("Failed to decompress multi payload", log.Err(err))
			return
		}
	}

	if err := s.processMultiPayload(payload); err != nil {
		s.logger.Error("Error processing multi payload", log.Err(err))
	}
}

// handleServiceMethod is the default handler for EMsg_ServiceMethod packets.
func (s *Socket) handleServiceMethod(packet *protocol.Packet) {
	header, ok := packet.Header.(*protocol.MsgHdrProtoBuf)
	if !ok {
		s.logger.Warn("Received ServiceMethod with non-proto header")
		return
	}

	methodName := header.Proto.GetTargetJobName()

	s.serviceHandlersMu.RLock()
	handler, ok := s.serviceHandlers[methodName]
	s.serviceHandlersMu.RUnlock()

	if ok {
		handler(packet)
	} else {
		s.logger.Debug("Unhandled ServiceMethod", log.String("method", methodName))
	}
}

func (s *Socket) handleJobResponse(packet *protocol.Packet) bool {
	targetID := packet.GetTargetJobID()
	if targetID == protocol.NoJob {
		return false
	}

	var err error
	if packet.EMsg == protocol.EMsg_DestJobFailed {
		err = fmt.Errorf("destination job failed on Steam side")
	}

	return s.jobManager.Resolve(targetID, packet, err)
}

func (s *Socket) processSingle(msg io.Reader) {
	packet, err := protocol.ParsePacket(msg)
	if err != nil {
		s.logger.Error("Failed to parse packet", log.Err(err))
		return
	}

	select {
	case s.msgCh <- packet:
	case <-s.done:
	}
}

func (s *Socket) decompressPayload(data network.NetMessage, unzippedSize int64) ([]byte, error) {
	if unzippedSize > 100*1024*1024 { // 100MB limit to prevent OOM attacks
		return nil, errors.New("unzipped size too large")
	}

	gr, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	defer gr.Close()

	out := make([]byte, unzippedSize)
	if _, err := io.ReadFull(gr, out); err != nil {
		return nil, fmt.Errorf("read full decompressed payload: %w", err)
	}

	return out, nil
}

func (s *Socket) processMultiPayload(payload []byte) error {
	reader := bytes.NewReader(payload)
	for reader.Len() > 0 {
		var subSize uint32
		if err := binary.Read(reader, binary.LittleEndian, &subSize); err != nil {
			return fmt.Errorf("read size: %w", err)
		}
		if subSize == 0 {
			continue
		}

		packet, err := protocol.ParsePacket(io.LimitReader(reader, int64(subSize)))
		if err != nil {
			s.logger.Error("Failed to parse nested multi packet", log.Err(err))
			continue
		}

		select {
		case s.msgCh <- packet:
		case <-s.done:
			return nil
		}
	}
	return nil
}

func (s *Socket) handleRemoteClose() {
	s.mu.Lock()
	if s.connCancel != nil {
		s.connCancel()
	}
	s.mu.Unlock()

	old := s.setState(StateDisconnected)
	if old == StateDisconnecting {
		s.bus.Publish(&DisconnectedEvent{})
	} else {
		s.bus.Publish(&DisconnectedEvent{Error: errors.New("connection closed unexpectedly")})
	}
}

// inboundHandler acts as the adapter (or "bridge") between the low-level `network`
// package and the higher-level `socket` logic. It implements network.Handler.
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

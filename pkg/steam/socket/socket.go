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
	"runtime"
	"sync"
	"sync/atomic"
	"time"

	"github.com/lemon4ksan/g-man/pkg/jobs"
	"github.com/lemon4ksan/g-man/pkg/log"
	"github.com/lemon4ksan/g-man/pkg/steam/bus"
	"github.com/lemon4ksan/g-man/pkg/steam/network"
	"github.com/lemon4ksan/g-man/pkg/steam/protocol"
	pb "github.com/lemon4ksan/g-man/pkg/steam/protobuf"

	"google.golang.org/protobuf/proto"
)

// Handler defines a callback function for processing incoming Steam packets.
type Handler func(p *protocol.Packet)

// CMServer represents a Steam Connection Manager server endpoint.
type CMServer struct {
	Endpoint string  // Host:port address for connection.
	Type     string  // Connection protocol: "netfilter" (TCP) or "websockets".
	Load     float64 // Current server load (lower is better).
	Realm    string  // Steam realm, e.g., "steamglobal", "steamchina".
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

// Config holds configuration parameters for the Socket.
type Config struct {
	ConnectTimeout time.Duration
	EventChanSize  int
	WorkerCount    int
	DebugEvents    bool
	BlockingEvents bool
	Dialers        map[string]ConnectionDialer
}

// DefaultConfig returns the recommended default configuration.
func DefaultConfig() Config {
	return Config{
		ConnectTimeout: 10 * time.Second,
		EventChanSize:  1000, // Increased to handle bursts of multi-messages
		WorkerCount:    runtime.NumCPU(),
		DebugEvents:    true,
		BlockingEvents: false,
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
	return func(s *Socket) { s.logger = l }
}

// WithSession injects a custom pre-configured session.
func WithSession(session Session) Option {
	return func(s *Socket) { s.session = session }
}

// WithEncoder sets a custom message encoder.
func WithEncoder(encoder Encoder) Option {
	return func(s *Socket) { s.encoder = encoder }
}

// Socket represents the core client network manager. It handles connections,
// message routing, job tracking, and payload encoding/decoding.
type Socket struct {
	config Config
	state  atomic.Int32

	// Global dependencies
	logger     log.Logger
	encoder    Encoder
	bus        *bus.Bus
	jobManager *jobs.Manager[*protocol.Packet]

	// Connection specific state (protected by mu)
	mu         sync.RWMutex
	session    Session
	ctx        context.Context    // Context tied to the entire socket
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
func NewSocket(ctx context.Context, cfg Config, opts ...Option) *Socket {
	if cfg.Dialers == nil {
		cfg.Dialers = DefaultDialers()
	}
	if cfg.WorkerCount <= 0 {
		cfg.WorkerCount = 1
	}

	s := &Socket{
		ctx:             ctx,
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
	if s.encoder == nil {
		s.encoder = &BaseEncoder{}
	}

	s.setState(StateDisconnected)

	s.RegisterMsgHandler(protocol.EMsg_Multi, s.handleMulti)
	s.RegisterMsgHandler(protocol.EMsg_ServiceMethod, s.handleServiceMethod)

	// Start workers ONCE for the lifetime of the Socket.
	s.startWorkers(s.config.WorkerCount)

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
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.session
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

// StartHeartbeat begins sending heartbeat messages at the specified interval.
// It runs asynchronously and stops automatically when the connection drops.
func (s *Socket) StartHeartbeat(interval time.Duration) {
	s.mu.RLock()
	ctx := s.connCtx
	s.mu.RUnlock()

	if ctx == nil {
		s.logger.Warn("StartHeartbeat called without an active connection")
		return
	}

	go func() {
		s.logger.Debug("Heartbeat started", log.Duration("interval", interval))
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				if s.State() != StateConnected {
					return
				}
				s.CallProto(ctx, protocol.EMsg_ClientHeartBeat, &pb.CMsgClientHeartBeat{}, nil)
			case <-ctx.Done():
				s.logger.Debug("Heartbeat stopped due to connection closure")
				return
			case <-s.done:
				return
			}
		}
	}()
}

// Connect attempts to establish a connection to the provided CM server.
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
	handler := &inboundHandler{sock: s}

	// Establish transport connection
	conn, err := dialer(handler, s.logger, server.Endpoint)
	if err != nil {
		s.setState(StateDisconnected)
		return fmt.Errorf("transport dial failed: %w", err)
	}

	baseSession := NewBaseSession(conn)
	sessionLogger := s.logger.With(
		log.String("endpoint", server.Endpoint),
		log.Int64("conn_id", conn.ID()),
	)

	// Setup active connection context
	connCtx, connCancel := context.WithCancel(s.ctx)

	s.mu.Lock()
	s.session = NewLoggedSession(baseSession, sessionLogger)
	s.connCtx = connCtx
	s.connCancel = connCancel
	s.mu.Unlock()

	s.logger.Info("Successfully connected", log.Duration("latency", time.Since(start)))

	s.setState(StateConnected)
	s.bus.Publish(&ConnectedEvent{Server: server.Endpoint})

	return nil
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

// Disconnect gracefully closes the current active connection.
func (s *Socket) Disconnect() {
	s.setState(StateDisconnecting)

	s.mu.Lock()
	if s.connCancel != nil {
		s.connCancel()
	}
	if s.session != nil {
		s.session.Close()
		s.session = nil
	}
	s.mu.Unlock()

	s.logger.Info("Client disconnected")
	s.bus.Publish(&DisconnectedEvent{})
	s.setState(StateDisconnected)
}

// Close permanently shuts down the Socket and all its background workers.
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

// CallUnified executes a unified service method and waits for the response if cb provided.
func (s *Socket) CallUnified(ctx context.Context, method string, req proto.Message, cb jobs.Callback[*protocol.Packet]) error {
	return s.sendRequest(ctx, cb, func(buf *bytes.Buffer, sourceJob uint64) error {
		sess := s.Session()
		if sess == nil {
			return errors.New("no active session")
		}
		return s.encoder.EncodeUnified(buf, sess.SteamID(), sess.SessionID(), method, sourceJob, req)
	})
}

// CallProto sends a Protocol Buffer message and optionally tracks the response via a callback.
func (s *Socket) CallProto(ctx context.Context, eMsg protocol.EMsg, req proto.Message, cb jobs.Callback[*protocol.Packet]) error {
	return s.sendRequest(ctx, cb, func(buf *bytes.Buffer, sourceJob uint64) error {
		sess := s.Session()
		if sess == nil {
			return errors.New("no active session")
		}
		return s.encoder.EncodeProto(buf, eMsg, sess.SteamID(), sess.SessionID(), sourceJob, protocol.NoJob, req)
	})
}

// CallRaw is a general-purpose method for sending ready-made bytes while waiting for a response.
func (s *Socket) CallRaw(ctx context.Context, eMsg protocol.EMsg, payload []byte, cb jobs.Callback[*protocol.Packet]) error {
	return s.sendRequest(ctx, cb, func(buf *bytes.Buffer, sourceJob uint64) error {
		return s.encoder.EncodeRaw(buf, eMsg, protocol.NoJob, sourceJob, payload)
	})
}

// CallRaw is a general-purpose method for sending ready-made bytes while waiting for a response with unified capabilities.
func (s *Socket) CallUnifiedRaw(ctx context.Context, eMsg protocol.EMsg, targetName string, payload []byte, cb jobs.Callback[*protocol.Packet]) error {
	return s.sendRequest(ctx, cb, func(buf *bytes.Buffer, sourceJob uint64) error {
		if targetName != "" {
			sess := s.Session()
			if sess == nil {
				return errors.New("socket: session required for unified raw call")
			}
			return s.encoder.EncodeUnifiedRaw(buf, sess.SteamID(), sess.SessionID(), targetName, sourceJob, payload)
		}
		return s.encoder.EncodeRaw(buf, eMsg, protocol.NoJob, sourceJob, payload)
	})
}

// SendUnified executes a unified service method.
func (s *Socket) SendUnified(ctx context.Context, method string, req proto.Message) error {
	return s.CallUnified(ctx, method, req, nil)
}

// CallProto sends a Protocol Buffer message and optionally tracks the response via a callback.
func (s *Socket) SendProto(ctx context.Context, eMsg protocol.EMsg, req proto.Message) error {
	return s.CallProto(ctx, eMsg, req, nil)
}

// CallRaw is a general-purpose method for sending ready-made bytes.
func (s *Socket) SendRaw(ctx context.Context, eMsg protocol.EMsg, payload []byte) error {
	return s.CallRaw(ctx, eMsg, payload, nil)
}

// CallRaw is a general-purpose method for sending ready-made bytes with unified capabilities.
func (s *Socket) SendUnifiedRaw(ctx context.Context, eMsg protocol.EMsg, targetName string, payload []byte) error {
	return s.CallUnifiedRaw(ctx, eMsg, targetName, payload, nil)
}

type payloadBuilder func(buf *bytes.Buffer, sourceJobID uint64) error

func (s *Socket) sendRequest(ctx context.Context, cb jobs.Callback[*protocol.Packet], build payloadBuilder) error {
	sess := s.Session()
	if sess == nil {
		return errors.New("socket is disconnected")
	}

	var sourceJobID uint64 = protocol.NoJob
	if cb != nil {
		sourceJobID = s.jobManager.NextID()
		err := s.jobManager.Add(sourceJobID, func(response *protocol.Packet, err error) {
			var eMsg protocol.EMsg
			if response != nil {
				eMsg = response.EMsg
			}
			defer s.recoverPanic(eMsg)
			cb(response, err)
		}, jobs.WithContext[*protocol.Packet](ctx))

		if err != nil {
			return fmt.Errorf("track job: %w", err)
		}
	}

	buf := s.bufferPool.Get().(*bytes.Buffer)
	buf.Reset()
	defer s.bufferPool.Put(buf)

	if err := build(buf, sourceJobID); err != nil {
		if cb != nil {
			s.jobManager.Resolve(sourceJobID, nil, err)
		}
		return err
	}

	if err := sess.Send(ctx, buf.Bytes()); err != nil {
		if cb != nil {
			s.jobManager.Resolve(sourceJobID, nil, err)
		}
		return err
	}

	return nil
}

func (s *Socket) startWorkers(count int) {
	for range count {
		s.workerWg.Add(1)
		go s.worker()
	}
}

func (s *Socket) worker() {
	defer s.workerWg.Done()
	for {
		select {
		case pkt, ok := <-s.msgCh:
			if !ok {
				return
			} // msgCh closed during socket Close()
			s.routePacket(pkt)
		case <-s.done:
			return // Socket closed
		}
	}
}

// routePacket determines where the packet should go.
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

// handleMulti is the default handler for EMsg_Multi packets.
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

	return io.ReadAll(io.LimitReader(gr, unzippedSize))
}

func (s *Socket) processMultiPayload(payload network.NetMessage) error {
	reader := bytes.NewReader(payload)
	for reader.Len() > 0 {
		var subSize uint32
		if err := binary.Read(reader, binary.LittleEndian, &subSize); err != nil {
			return fmt.Errorf("read size: %w", err)
		}
		if subSize == 0 {
			continue
		}
		s.processSingle(io.LimitReader(reader, int64(subSize)))
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

func (s *Socket) recoverPanic(emsg protocol.EMsg) {
	if r := recover(); r != nil {
		s.logger.Error("Socket recovered from panic",
			log.String("emsg", emsg.String()),
			log.Any("panic", r),
		)
	}
}

func (s *Socket) setState(new State) State {
	old := State(s.state.Swap(int32(new)))
	if old != new {
		s.bus.Publish(&StateEvent{Old: old, New: new})
	}
	return old
}

// inboundHandler acts as the bridge between the raw network connection and the Socket.
type inboundHandler struct {
	sock *Socket
}

func (h *inboundHandler) OnNetMessage(msg network.NetMessage) {
	h.sock.processSingle(bytes.NewReader(msg))
}
func (h *inboundHandler) OnNetError(err error) {
	h.sock.logger.Error("Network error", log.Err(err))
	h.sock.bus.Publish(&NetworkErrorEvent{Error: err})
}
func (h *inboundHandler) OnNetClose() { h.sock.handleRemoteClose() }

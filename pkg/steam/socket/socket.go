// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package socket provides the central facade for persistent TCP/WebSocket connections, job dispatching, and worker pool decoding to Steam CM servers.
package socket

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/lemon4ksan/aoni/fast"
	"github.com/lemon4ksan/aoni/realtime/socket"
	"github.com/lemon4ksan/aoni/realtime/socket/connector"
	"github.com/lemon4ksan/aoni/realtime/socket/dispatcher"
	"github.com/lemon4ksan/aoni/realtime/socket/processor"
	"github.com/lemon4ksan/foundation/async/log"
	"github.com/lemon4ksan/foundation/generic"
	"google.golang.org/protobuf/proto"

	"github.com/lemon4ksan/g-man/internal/framer"
	pb "github.com/lemon4ksan/g-man/protobuf/steam"
	"github.com/lemon4ksan/g-man/pkg/steam/protocol"
	"github.com/lemon4ksan/g-man/pkg/steam/protocol/enums"
)

// ErrClosed indicates socket operation was attempted after Close.
var ErrClosed = errors.New("socket: instance is permanently closed")

// Session defines credential and session state methods.
type Session interface {
	SteamID() uint64
	SessionID() int32
	RefreshToken() string
	AccessToken() string
	IsAuthenticated() bool
	SetSteamID(sid uint64)
	SetSessionID(sid int32)
	SetRefreshToken(token string)
	SetAccessToken(token string)
}

// BasicSession provides an atomic container for session metadata.
type BasicSession struct {
	steamID      atomic.Uint64
	sessionID    atomic.Int32
	refreshToken atomic.Value
	accessToken  atomic.Value
}

// SteamID returns the 64-bit Steam ID.
func (s *BasicSession) SteamID() uint64 { return s.steamID.Load() }

// SessionID returns the 32-bit session ID.
func (s *BasicSession) SessionID() int32 { return s.sessionID.Load() }

// RefreshToken returns the refresh token string.
func (s *BasicSession) RefreshToken() string {
	val, _ := s.refreshToken.Load().(string)
	return val
}

// AccessToken returns the access token string.
func (s *BasicSession) AccessToken() string {
	val, _ := s.accessToken.Load().(string)
	return val
}

// IsAuthenticated reports whether a session is authenticated.
func (s *BasicSession) IsAuthenticated() bool {
	return s.SessionID() != 0 && s.SteamID() != 0
}

// SetSteamID sets the 64-bit Steam ID.
func (s *BasicSession) SetSteamID(sid uint64) { s.steamID.Store(sid) }

// SetSessionID sets the session ID.
func (s *BasicSession) SetSessionID(sid int32) { s.sessionID.Store(sid) }

// SetRefreshToken sets the refresh token string.
func (s *BasicSession) SetRefreshToken(token string) { s.refreshToken.Store(token) }

// SetAccessToken sets the access token string.
func (s *BasicSession) SetAccessToken(token string) { s.accessToken.Store(token) }

// Config configures the Steam socket subsystem.
type Config struct {
	FastClient *fast.Client
	Connector  ConnectorConfig
	Processor  ProcessorConfig
	MaxJobs    int
}

// DefaultConfig builds recommended socket subsystem settings.
func DefaultConfig() Config {
	return Config{
		Connector: DefaultConnectorConfig(),
		Processor: DefaultProcessorConfig(),
		MaxJobs:   1000,
	}
}

// Socket manages connection dialing, worker decoding pools, and packet job dispatching.
type Socket struct {
	cfg    Config
	mu     sync.RWMutex
	logger log.Logger

	conn     *connector.Connector[CMServer]
	proc     *processor.Processor[*protocol.Packet]
	dispatch *dispatcher.Dispatcher[enums.EMsg, uint64, *protocol.Packet]
	session  Session

	closeOnce       sync.Once
	closed          atomic.Bool
	heartbeatCancel context.CancelFunc
}

// New initializes a Socket facade instance.
func New(cfg Config) *Socket {
	if cfg.FastClient != nil && cfg.Connector.FastClient == nil {
		cfg.Connector.FastClient = cfg.FastClient
	}

	dialerMap := cfg.Connector.Dialers
	if len(dialerMap) == 0 {
		dialerMap = NewDialers(cfg.Connector.ProxyURL)
	}

	dialer := func(ctx context.Context, endpoint CMServer, f socket.Framer, c socket.Cipher) (connector.Connection, error) {
		t := strings.ToLower(strings.TrimSpace(endpoint.Type))
		switch t {
		case "", "tcp", "netfilter":
			t = "tcp"
		case "websocket", "websockets", "ws", "wss":
			t = "websocket"
		}

		d, ok := dialerMap[t]
		if !ok {
			return nil, fmt.Errorf("connector: unsupported transport protocol: %s", t)
		}

		return d(ctx, endpoint, f, c)
	}

	connCfg := connector.Config[CMServer]{
		Dialer:          dialer,
		ReconnectPolicy: cfg.Connector.ReconnectPolicy,
		ConnectTimeout:  cfg.Connector.ConnectTimeout,
		Logger:          log.Discard,
	}

	s := &Socket{
		cfg:     cfg,
		logger:  log.Discard,
		session: &BasicSession{},
	}

	s.conn = connector.New[CMServer](connCfg)

	extractor := dispatcher.Extractor[enums.EMsg, uint64, *protocol.Packet]{
		GetOpCode: func(p *protocol.Packet) enums.EMsg {
			if p == nil {
				return 0
			}

			return p.EMsg
		},
		GetMethod: func(p *protocol.Packet) (string, bool) {
			if p != nil && p.HeaderKind == protocol.HeaderKindProto && p.HdrProto.Proto != nil {
				name := p.HdrProto.Proto.GetTargetJobName()
				return name, name != ""
			}

			return "", false
		},
		GetJobID: func(p *protocol.Packet) (uint64, bool) {
			if p != nil {
				if p.HeaderKind == protocol.HeaderKindProto && p.HdrProto.Proto != nil {
					jid := p.HdrProto.Proto.GetJobidTarget()
					if jid != 0 && jid != protocol.NoJob {
						return jid, true
					}
				}

				if p.HeaderKind == protocol.HeaderKindExtended {
					if p.HdrExt.TargetJobID != 0 && p.HdrExt.TargetJobID != protocol.NoJob {
						return p.HdrExt.TargetJobID, true
					}
				}
			}

			return 0, false
		},
	}

	dispCfg := dispatcher.Config{
		MaxJobs: cfg.MaxJobs,
		Logger:  s.logger,
	}

	s.dispatch = dispatcher.New[enums.EMsg, uint64, *protocol.Packet](dispCfg, s.conn, extractor)
	s.dispatch.RegisterHandler(enums.EMsg_Multi, s.handleMulti)

	decode := func(data []byte) (*protocol.Packet, error) {
		pkt, err := protocol.ParsePacket(bytes.NewReader(data))
		if err != nil {
			s.Logger().Error("Failed to parse packet", log.Err(err), log.Int("len", len(data)))
			return nil, err
		}

		s.Logger().
			Debug("Decoded packet", log.Uint32("emsg", uint32(pkt.EMsg)), log.Bool("isProto", pkt.IsProto), log.Int("payloadLen", len(pkt.Payload)))

		return pkt, nil
	}

	s.proc = processor.New[*protocol.Packet](cfg.Processor, s.conn.C(), s.dispatch, decode)

	return s
}

func (s *Socket) handleMulti(packet *protocol.Packet) {
	msg := &pb.CMsgMulti{}
	if err := protocol.UnmarshalProto(packet.Payload, msg); err != nil {
		s.Logger().Error("Failed to unmarshal CMsgMulti", log.Err(err))
		return
	}

	payload := msg.GetMessageBody()
	if msg.GetSizeUnzipped() > 0 {
		gr, err := gzip.NewReader(bytes.NewReader(payload))
		if err != nil {
			s.Logger().Error("Failed to decompress multi payload", log.Err(err))
			return
		}
		defer gr.Close()

		unzipped, err := io.ReadAll(gr)
		if err != nil {
			s.Logger().Error("Failed to read decompressed multi payload", log.Err(err))
			return
		}

		payload = unzipped
	}

	reader := bytes.NewReader(payload)
	for reader.Len() > 0 {
		var subSize uint32
		if err := binary.Read(reader, binary.LittleEndian, &subSize); err != nil {
			s.Logger().Error("Failed to read multi sub-packet size", log.Err(err))
			return
		}

		if subSize == 0 {
			continue
		}

		subData := make([]byte, subSize)
		if _, err := io.ReadFull(reader, subData); err != nil {
			s.Logger().Error("Failed to read multi sub-packet data", log.Err(err))
			return
		}

		subPkt, err := protocol.ParsePacket(bytes.NewReader(subData))
		if err != nil {
			s.Logger().Error("Failed to parse multi sub-packet", log.Err(err))
			continue
		}

		s.dispatch.Dispatch(subPkt)
	}
}

// Connector returns the underlying connector instance.
func (s *Socket) Connector() *connector.Connector[CMServer] { return s.conn }

// Dispatcher returns the underlying packet dispatcher instance.
func (s *Socket) Dispatcher() *dispatcher.Dispatcher[enums.EMsg, uint64, *protocol.Packet] {
	return s.dispatch
}

// Session returns the socket session container.
func (s *Socket) Session() Session { return s.session }

// Logger returns the socket logger.
func (s *Socket) Logger() log.Logger {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.logger
}

// UpdateLogger updates the active logger.
func (s *Socket) UpdateLogger(logger log.Logger) {
	s.mu.Lock()
	s.logger = logger.With(log.Module("sock"))
	s.mu.Unlock()
}

// IsConnected reports whether an active connection exists.
func (s *Socket) IsConnected() bool {
	return s.conn.IsConnected() && !s.closed.Load()
}

// UpdateServers updates known Connection Manager servers.
func (s *Socket) UpdateServers(servers []CMServer) {
	s.conn.UpdateEndpoints(servers)
}

// SetOnReconnect registers a callback function executed after reconnect.
func (s *Socket) SetOnReconnect(fn func(ctx context.Context)) {
	s.conn.SetOnReconnect(fn)
}

// Connect dials a target CM server.
func (s *Socket) Connect(ctx context.Context, server CMServer) error {
	if s.closed.Load() {
		return ErrClosed
	}

	s.proc.Start()

	return s.conn.Connect(ctx, server)
}

// Send serializes and transmits a packet.
func (s *Socket) Send(ctx context.Context, build PayloadBuilder, opts ...SendOption) error {
	if s.closed.Load() {
		return ErrClosed
	}

	if build == nil {
		return errors.New("socket: nil payload builder")
	}

	var optCfg SendConfig
	for _, opt := range opts {
		if opt != nil {
			opt(&optCfg)
		}
	}

	jobID := s.dispatch.NextJobID()

	var buf bytes.Buffer
	if err := build(s.session, &buf, jobID, optCfg.Token); err != nil {
		return fmt.Errorf("build payload: %w", err)
	}

	return s.conn.Send(ctx, buf.Bytes())
}

// SendRaw sends raw bytes under an EMsg.
func (s *Socket) SendRaw(ctx context.Context, eMsg enums.EMsg, payload []byte, opts ...SendOption) error {
	return s.Send(ctx, Raw(eMsg, payload), opts...)
}

// SendProto sends a protobuf message under an EMsg.
func (s *Socket) SendProto(ctx context.Context, eMsg enums.EMsg, req proto.Message, opts ...SendOption) error {
	return s.Send(ctx, Proto(eMsg, req), opts...)
}

// SendUnified sends a unified protobuf message.
func (s *Socket) SendUnified(ctx context.Context, method string, req proto.Message, opts ...SendOption) error {
	return s.Send(ctx, Unified(method, req), opts...)
}

// SendSync transmits a message and synchronously awaits response.
func (s *Socket) SendSync(ctx context.Context, build PayloadBuilder, opts ...SendOption) (*protocol.Packet, error) {
	if s.closed.Load() {
		return nil, ErrClosed
	}

	if build == nil {
		return nil, errors.New("socket: nil payload builder")
	}

	var optCfg SendConfig
	for _, opt := range opts {
		if opt != nil {
			opt(&optCfg)
		}
	}

	jobID := s.dispatch.NextJobID()

	var buf bytes.Buffer
	if err := build(s.session, &buf, jobID, optCfg.Token); err != nil {
		return nil, fmt.Errorf("build payload: %w", err)
	}

	return s.dispatch.SendSync(ctx, jobID, buf.Bytes())
}

// SendAsync returns a future for asynchronous response delivery.
func (s *Socket) SendAsync(
	ctx context.Context,
	build PayloadBuilder,
	opts ...SendOption,
) *generic.Future[*protocol.Packet] {
	return generic.NewFutureFunc(func() (*protocol.Packet, error) {
		return s.SendSync(ctx, build, opts...)
	})
}

// StartHeartbeat initiates background sending of periodic ClientHeartBeat messages.
func (s *Socket) StartHeartbeat(interval time.Duration) error {
	if s.closed.Load() {
		return ErrClosed
	}

	s.mu.Lock()
	if s.heartbeatCancel != nil {
		s.heartbeatCancel()
	}

	ctx, cancel := context.WithCancel(context.Background()) //nolint:gosec
	s.heartbeatCancel = cancel
	s.mu.Unlock()

	s.Logger().Debug("Starting heartbeat loop", log.Duration("interval", interval))

	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				if !s.IsConnected() {
					continue
				}

				err := s.SendProto(context.Background(), enums.EMsg_ClientHeartBeat, &pb.CMsgClientHeartBeat{})
				if err != nil {
					s.Logger().Warn("Failed to send heartbeat", log.Err(err))
				}

			case <-ctx.Done():
				s.Logger().Debug("Heartbeat loop stopped")
				return
			}
		}
	}()

	return nil
}

// Disconnect closes the active connection.
func (s *Socket) Disconnect() error {
	s.session.SetSessionID(0)
	return s.conn.Disconnect()
}

// Close permanently shuts down the socket.
func (s *Socket) Close() error {
	var errs []error

	s.closed.Store(true)
	s.closeOnce.Do(func() {
		s.mu.Lock()
		if s.heartbeatCancel != nil {
			s.heartbeatCancel()
		}

		s.mu.Unlock()
		errs = append(errs, s.conn.Close())

		s.proc.Stop()
		errs = append(errs, s.dispatch.Close())

		s.dispatch.ClearHandlers()
	})

	return errors.Join(errs...)
}

// RegisterMsgHandler registers a message handler for an EMsg opcode.
func (s *Socket) RegisterMsgHandler(eMsg enums.EMsg, h Handler) {
	s.dispatch.RegisterHandler(eMsg, dispatcher.Handler[*protocol.Packet](h))
}

// RegisterServiceHandler registers a handler for unified service methods.
func (s *Socket) RegisterServiceHandler(method string, h Handler) {
	s.dispatch.RegisterMethodHandler(method, dispatcher.Handler[*protocol.Packet](h))
}

// UnregisterMsgHandler removes an opcode handler.
func (s *Socket) UnregisterMsgHandler(eMsg enums.EMsg) {
	s.dispatch.RegisterHandler(eMsg, nil)
}

// UnregisterServiceHandler removes a service method handler.
func (s *Socket) UnregisterServiceHandler(method string) {
	s.dispatch.RegisterMethodHandler(method, nil)
}

// SetEncryptionKey configures symmetric session encryption.
func (s *Socket) SetEncryptionKey(key []byte) bool {
	if len(key) == 0 {
		s.conn.SetCipher(nil)
		return false
	}

	s.conn.SetCipher(framer.NewSteamCipher(key))

	return true
}

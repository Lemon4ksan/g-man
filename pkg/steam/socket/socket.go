// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package socket

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"time"

	"google.golang.org/protobuf/proto"

	"github.com/lemon4ksan/g-man/pkg/jobs"
	"github.com/lemon4ksan/g-man/pkg/log"
	pb "github.com/lemon4ksan/g-man/pkg/protobuf/steam"
	"github.com/lemon4ksan/g-man/pkg/steam/protocol"
	"github.com/lemon4ksan/g-man/pkg/steam/protocol/enums"
	"github.com/lemon4ksan/g-man/pkg/steam/socket/connector"
	"github.com/lemon4ksan/g-man/pkg/steam/socket/dispatcher"
	"github.com/lemon4ksan/g-man/pkg/steam/socket/processor"
	"github.com/lemon4ksan/g-man/pkg/steam/socket/session"
)

// ErrClosed is returned when an operation is attempted on a Socket that
// has been permanently shut down via Close().
var ErrClosed = errors.New("socket: instance is permanently closed")

type (
	// CMServer represents a Steam Connection Manager server endpoint.
	CMServer = connector.CMServer
	// Handler defines a callback function for processing a fully-parsed Steam packet.
	Handler = dispatcher.Handler
	// PayloadBuilder defines how to assemble a binary packet.
	PayloadBuilder = dispatcher.PayloadBuilder
	// SendOption defines a functional option for configuring a Send operation.
	SendOption = dispatcher.SendOption
)

var (
	// Raw builds a packet using Extended headers (non-protobuf).
	Raw = dispatcher.Raw
	// Proto builds a standard Protobuf-wrapped packet.
	Proto = dispatcher.Proto
	// Unified builds a Protobuf packet for Unified Service methods.
	Unified = dispatcher.Unified
	// DynamicRaw creates a PayloadBuilder that decides between Protobuf and Extended
	// headers based on whether a targetName (Unified Service method) is provided.
	// targetName == "" implies a standard (non-unified) message.
	DynamicRaw = dispatcher.DynamicRaw

	// WithCallback adds a callback to asynchronously wait for a response to the sent packet.
	WithCallback = dispatcher.WithCallback
	// WithToken sets an access token for service method calls via the socket.
	WithToken = dispatcher.WithToken
)

// Session defines the implementation of thread-safe Steam session.
type Session interface {
	// SteamID returns the 64-bit Steam ID assigned to the session.
	SteamID() uint64

	// SessionID returns the 32-bit session ID assigned by the CM.
	SessionID() int32

	// RefreshToken returns the current OAuth2 refresh token.
	RefreshToken() string

	// AccessToken returns the current OAuth2 access token.
	AccessToken() string

	// IsAuthenticated returns true if the session has been assigned both
	// a SessionID by the CM and a valid SteamID.
	IsAuthenticated() bool

	// SetSteamID updates the session's Steam ID.
	SetSteamID(sid uint64)

	// SetSessionID updates the session's ID assigned by the CM.
	SetSessionID(sid int32)

	// SetRefreshToken updates the OAuth2 refresh token.
	SetRefreshToken(token string)

	// SetAccessToken updates the OAuth2 access token.
	SetAccessToken(token string)
}

// Config aggregates configurations for all underlying socket subsystems.
type Config struct {
	Connector connector.Config
	Processor processor.Config
	MaxJobs   int
}

// DefaultConfig returns a recommended baseline for high-performance Steam bots.
func DefaultConfig() Config {
	return Config{
		Connector: connector.DefaultConfig(),
		Processor: processor.DefaultConfig(),
		MaxJobs:   1000,
	}
}

// Socket acts as the central facade for Steam network operations.
// It orchestrates the connection lifecycle, message processing, and routing.
type Socket struct {
	cfg    Config
	logger log.Logger

	// Subsystems
	conn     *connector.Connector
	proc     *processor.Processor
	dispatch *dispatcher.Dispatcher
	session  Session

	// Lifecycle
	closeOnce sync.Once
	closed    atomic.Bool
}

// NewSocket initializes a new Steam Socket facade.
func NewSocket(cfg Config, logger log.Logger) *Socket {
	s := &Socket{
		cfg:     cfg,
		logger:  logger,
		session: &session.Session{},
	}

	s.conn = connector.New(cfg.Connector, s.logger)

	s.dispatch = dispatcher.New(
		jobs.NewManager[*protocol.Packet](cfg.MaxJobs),
		s.conn,
		s.session,
		s.logger,
	)

	s.proc = processor.New(cfg.Processor, s.conn.C(), s.dispatch, s.logger)

	return s
}

// IsConnected returns true if the underlying transport is currently active.
func (s *Socket) IsConnected() bool {
	return s.conn.IsConnected() && !s.closed.Load()
}

// UpdateServers refreshes the list of available Steam CMs in the connector.
func (s *Socket) UpdateServers(servers []CMServer) {
	s.conn.UpdateServers(servers)
}

// Connector returns the internal network manager. Primarily used for advanced
// configuration or testing.
func (s *Socket) Connector() *connector.Connector {
	return s.conn
}

// Connect initiates a connection to a Steam CM server.
func (s *Socket) Connect(ctx context.Context, server CMServer) error {
	if s.closed.Load() {
		return ErrClosed
	}

	s.proc.Start() // Ensure workers are running before network starts

	return s.conn.Connect(ctx, server)
}

// Send is the primary method for transmitting data.
func (s *Socket) Send(ctx context.Context, build PayloadBuilder, opts ...SendOption) error {
	if s.closed.Load() {
		return ErrClosed
	}

	return s.dispatch.Send(ctx, build, opts...)
}

// SendRaw is a helper for raw messages.
func (s *Socket) SendRaw(ctx context.Context, eMsg enums.EMsg, payload []byte, opts ...SendOption) error {
	return s.Send(ctx, Raw(eMsg, payload), opts...)
}

// SendProto is a high-level helper for Protobuf messages.
func (s *Socket) SendProto(ctx context.Context, eMsg enums.EMsg, req proto.Message, opts ...SendOption) error {
	return s.Send(ctx, Proto(eMsg, req), opts...)
}

// SendUnified is a high-level helper for Unified Service calls.
func (s *Socket) SendUnified(ctx context.Context, method string, req proto.Message, opts ...SendOption) error {
	return s.Send(ctx, Unified(method, req), opts...)
}

// SendSync blocks until a response is received or the context is canceled.
func (s *Socket) SendSync(ctx context.Context, build PayloadBuilder, opts ...SendOption) (*protocol.Packet, error) {
	type result struct {
		pkt *protocol.Packet
		err error
	}

	resCh := make(chan result, 1)
	cb := func(pkt *protocol.Packet, err error) {
		resCh <- result{pkt, err}
	}

	if err := s.Send(ctx, build, append(opts, dispatcher.WithCallback(cb))...); err != nil {
		return nil, err
	}

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case res := <-resCh:
		return res.pkt, res.err
	}
}

// StartHeartbeat begins sending periodic ClientHeartBeat messages to Steam.
// The loop automatically stops if the socket is closed or the connection drops.
func (s *Socket) StartHeartbeat(interval time.Duration) error {
	if s.closed.Load() {
		return ErrClosed
	}

	s.logger.Debug("Starting heartbeat loop", log.Duration("interval", interval))

	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				if !s.IsConnected() {
					continue
				}

				// We use a background-like context for heartbeats,
				// but Send internally checks if the socket is closed.
				err := s.SendProto(context.Background(), enums.EMsg_ClientHeartBeat, &pb.CMsgClientHeartBeat{})
				if err != nil {
					s.logger.Warn("Failed to send heartbeat", log.Err(err))
				}

			case <-s.conn.Done():
				s.logger.Debug("Heartbeat loop stopped")
				return
			}
		}
	}()

	return nil
}

// Disconnect gracefully closes the transport connection.
func (s *Socket) Disconnect() error {
	s.session.SetSessionID(0) // SessionID is transient to the connection
	return s.conn.Disconnect()
}

// Close permanently shuts down the socket and all its subsystems.
func (s *Socket) Close() error {
	var errs []error

	s.closed.Store(true)
	s.closeOnce.Do(func() {
		errs = append(errs, s.conn.Close())
		s.proc.Stop()
		errs = append(errs, s.dispatch.Close())
		s.dispatch.ClearHandlers()
	})

	return errors.Join(errs...)
}

// RegisterMsgHandler adds a handler for a specific EMsg.
func (s *Socket) RegisterMsgHandler(eMsg enums.EMsg, h Handler) {
	s.dispatch.RegisterMsgHandler(eMsg, h)
}

// RegisterServiceHandler adds a handler for a Unified Service method.
func (s *Socket) RegisterServiceHandler(method string, h Handler) {
	s.dispatch.RegisterServiceHandler(method, h)
}

// UnregisterMsgHandler removes a handler for a specific Steam message.
func (s *Socket) UnregisterMsgHandler(eMsg enums.EMsg) {
	s.dispatch.RegisterMsgHandler(eMsg, nil)
}

// UnregisterServiceHandler removes a handler for a specific Unified Service method.
func (s *Socket) UnregisterServiceHandler(method string) {
	s.dispatch.RegisterServiceHandler(method, nil)
}

// Session returns the shared state container.
func (s *Socket) Session() Session { return s.session }

// SetEncryptionKey upgrades the connection to encrypted mode.
func (s *Socket) SetEncryptionKey(key []byte) bool { return s.conn.SetEncryptionKey(key) }

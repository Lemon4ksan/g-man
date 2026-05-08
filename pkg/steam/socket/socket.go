// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package socket

import (
	"bytes"
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"time"

	"github.com/lemon4ksan/g-man/pkg/bus"
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

// Aliases for an easy access.
type (
	Handler  = dispatcher.Handler
	CMServer = connector.CMServer
)

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

func WithLogger(l log.Logger) bus.Option[*Socket] {
	return func(s *Socket) { s.logger = l }
}

// Socket acts as the central facade for Steam network operations.
// It orchestrates the connection lifecycle, message processing, and routing.
type Socket struct {
	cfg    Config
	logger log.Logger

	// Subsystems
	conn       *connector.Connector
	proc       *processor.Processor
	dispatch   *dispatcher.Dispatcher
	session    Session
	jobManager *jobs.Manager[*protocol.Packet]

	bufferPool sync.Pool

	// Lifecycle
	closeOnce sync.Once
	closed    atomic.Bool
}

// NewSocket initializes a new Steam Socket facade.
func NewSocket(cfg Config, opts ...bus.Option[*Socket]) *Socket {
	b := bus.New()
	l := log.Discard
	jm := jobs.NewManager[*protocol.Packet](cfg.MaxJobs)

	sess := &session.Session{}
	disp := dispatcher.New(jm, l)
	proc := processor.New(cfg.Processor, disp, l)

	conn := connector.New(context.Background(), cfg.Connector, b,
		connector.WithLogger(l),
		connector.WithDataHandler(proc), // Wire Connector -> Processor
	)

	s := &Socket{
		cfg:        cfg,
		logger:     l.With(log.Module("socket")),
		conn:       conn,
		proc:       proc,
		dispatch:   disp,
		session:    sess,
		jobManager: jm,
		bufferPool: sync.Pool{
			New: func() interface{} {
				return bytes.NewBuffer(make([]byte, 0, 1024))
			},
		},
	}

	for _, opt := range opts {
		opt(s)
	}

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
	s.proc.Start() // Ensure workers are running before network starts
	return s.conn.Connect(ctx, server)
}

// StartHeartbeat begins sending periodic ClientHeartBeat messages to Steam.
// The loop automatically stops if the socket is closed or the connection drops.
func (s *Socket) StartHeartbeat(interval time.Duration) {
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
}

// Disconnect gracefully closes the transport connection.
func (s *Socket) Disconnect() error {
	s.session.SetSessionID(0) // SessionID is transient to the connection
	return s.conn.Disconnect()
}

// Close permanently shuts down the socket and all its subsystems.
func (s *Socket) Close() error {
	var err error

	s.closed.Store(true)
	s.closeOnce.Do(func() {
		err = s.conn.Close()
		s.proc.Stop()
		_ = s.jobManager.Close()
		s.dispatch.ClearHandlers()
	})

	return err
}

// RegisterMsgHandler adds a handler for a specific EMsg.
func (s *Socket) RegisterMsgHandler(eMsg enums.EMsg, h Handler) {
	s.dispatch.RegisterMsgHandler(eMsg, dispatcher.Handler(h))
}

// RegisterServiceHandler adds a handler for a Unified Service method.
func (s *Socket) RegisterServiceHandler(method string, h Handler) {
	s.dispatch.RegisterServiceHandler(method, dispatcher.Handler(h))
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

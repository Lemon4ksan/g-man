// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package socket provides the central facade for persistent TCP/WebSocket connections, job dispatching, and worker pool decoding to Steam CM servers.
package socket

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"time"

	"github.com/lemon4ksan/aoni/fast"
	"github.com/lemon4ksan/miyako/generic"
	"github.com/lemon4ksan/miyako/jobs"
	"github.com/lemon4ksan/miyako/log"
	"google.golang.org/protobuf/proto"

	"github.com/lemon4ksan/g-man/internal/socket/connector"
	"github.com/lemon4ksan/g-man/internal/socket/dispatcher"
	"github.com/lemon4ksan/g-man/internal/socket/processor"
	"github.com/lemon4ksan/g-man/internal/socket/session"
	pb "github.com/lemon4ksan/g-man/pkg/protobuf/steam"
	"github.com/lemon4ksan/g-man/pkg/steam/protocol"
	"github.com/lemon4ksan/g-man/pkg/steam/protocol/enums"
)

// ErrClosed indicates socket operation was attempted after Close.
var ErrClosed = errors.New("socket: instance is permanently closed")

type (
	Dialer          = connector.Dialer
	ConnectorConfig = connector.Config
	ReconnectPolicy = connector.ReconnectPolicy
	ProcessorConfig = processor.Config
)

var (
	DefaultConnectorConfig = connector.DefaultConfig
	DefaultDialers         = connector.DefaultDialers
	DefaultReconnectPolicy = connector.DefaultReconnectPolicy
	DefaultProcessorConfig = processor.DefaultConfig
)

type (
	CMServer       = connector.CMServer
	Handler        = dispatcher.Handler
	PayloadBuilder = dispatcher.PayloadBuilder
	SendOption     = dispatcher.SendOption
)

var (
	Raw             = dispatcher.Raw
	Proto           = dispatcher.Proto
	Unified         = dispatcher.Unified
	DynamicRaw      = dispatcher.DynamicRaw
	DynamicRawProto = dispatcher.DynamicRawProto

	WithCallback = dispatcher.WithCallback
	WithToken    = dispatcher.WithToken
)

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
//
// Thread Safety:
//   - Fully safe for concurrent use across all public methods.
type Socket struct {
	cfg    Config
	mu     sync.RWMutex
	logger log.Logger

	conn     *connector.Connector
	proc     *processor.Processor
	dispatch *dispatcher.Dispatcher
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

	s := &Socket{
		cfg:     cfg,
		logger:  log.Discard,
		session: &session.Session{},
	}

	s.conn = connector.New(cfg.Connector, s.logger)
	s.dispatch = dispatcher.New(
		jobs.NewManager[uint64, *protocol.Packet](cfg.MaxJobs),
		s.conn,
		s.session,
		s.logger,
	)
	s.proc = processor.New(cfg.Processor, s.conn.C(), s.dispatch, s.logger)

	return s
}

func (s *Socket) Dispatcher() *dispatcher.Dispatcher { return s.dispatch }

func (s *Socket) Logger() log.Logger {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.logger
}

func (s *Socket) UpdateLogger(logger log.Logger) {
	s.mu.Lock()
	s.logger = logger.With(log.Module("sock"))
	s.mu.Unlock()

	s.conn.UpdateLogger(s.Logger())
	s.dispatch.UpdateLogger(s.Logger())
	s.proc.UpdateLogger(s.Logger())
}

func (s *Socket) IsConnected() bool {
	return s.conn.IsConnected() && !s.closed.Load()
}

func (s *Socket) UpdateServers(servers []CMServer) {
	s.conn.UpdateServers(servers)
}

// SetOnReconnect registers a callback function executed after a successful socket reconnect cycle.
func (s *Socket) SetOnReconnect(fn func(ctx context.Context)) {
	s.conn.SetOnReconnect(fn)
}

func (s *Socket) Connector() *connector.Connector { return s.conn }

func (s *Socket) Connect(ctx context.Context, server CMServer) error {
	if s.closed.Load() {
		return ErrClosed
	}

	s.proc.Start()

	return s.conn.Connect(ctx, server)
}

func (s *Socket) Send(ctx context.Context, build PayloadBuilder, opts ...SendOption) error {
	if s.closed.Load() {
		return ErrClosed
	}

	return s.dispatch.Send(ctx, build, opts...)
}

func (s *Socket) SendRaw(ctx context.Context, eMsg enums.EMsg, payload []byte, opts ...SendOption) error {
	return s.Send(ctx, Raw(eMsg, payload), opts...)
}

func (s *Socket) SendProto(ctx context.Context, eMsg enums.EMsg, req proto.Message, opts ...SendOption) error {
	return s.Send(ctx, Proto(eMsg, req), opts...)
}

func (s *Socket) SendUnified(ctx context.Context, method string, req proto.Message, opts ...SendOption) error {
	return s.Send(ctx, Unified(method, req), opts...)
}

func (s *Socket) SendSync(ctx context.Context, build PayloadBuilder, opts ...SendOption) (*protocol.Packet, error) {
	type result struct {
		pkt *protocol.Packet
		err error
	}

	resCh := make(chan result, 1)
	cb := func(ctx context.Context, pkt *protocol.Packet, err error) {
		resCh <- result{pkt, err}
	}

	if err := s.Send(ctx, build, append(opts, dispatcher.WithCallback(cb))...); err != nil {
		return nil, err
	}

	select {
	case <-ctx.Done():
		return nil, ctx.Err()

	case res := <-resCh:
		if errors.Is(res.err, jobs.ErrJobCancelled) {
			return nil, ctx.Err()
		}

		return res.pkt, res.err
	}
}

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

func (s *Socket) Disconnect() error {
	s.session.SetSessionID(0)

	return s.conn.Disconnect()
}

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

func (s *Socket) RegisterMsgHandler(eMsg enums.EMsg, h Handler) {
	s.dispatch.RegisterMsgHandler(eMsg, h)
}

func (s *Socket) RegisterServiceHandler(method string, h Handler) {
	s.dispatch.RegisterServiceHandler(method, h)
}

func (s *Socket) UnregisterMsgHandler(eMsg enums.EMsg) {
	s.dispatch.RegisterMsgHandler(eMsg, nil)
}

func (s *Socket) UnregisterServiceHandler(method string) {
	s.dispatch.RegisterServiceHandler(method, nil)
}

func (s *Socket) Session() Session { return s.session }

func (s *Socket) SetEncryptionKey(key []byte) bool { return s.conn.SetEncryptionKey(key) }

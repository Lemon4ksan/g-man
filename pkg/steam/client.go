// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package steam

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/lemon4ksan/g-man/pkg/bus"
	"github.com/lemon4ksan/g-man/pkg/log"
	"github.com/lemon4ksan/g-man/pkg/rest"
	"github.com/lemon4ksan/g-man/pkg/steam/api"
	"github.com/lemon4ksan/g-man/pkg/steam/auth"
	"github.com/lemon4ksan/g-man/pkg/steam/community"
	"github.com/lemon4ksan/g-man/pkg/steam/id"
	"github.com/lemon4ksan/g-man/pkg/steam/module"
	"github.com/lemon4ksan/g-man/pkg/steam/protocol"
	"github.com/lemon4ksan/g-man/pkg/steam/protocol/enums"
	"github.com/lemon4ksan/g-man/pkg/steam/service"
	"github.com/lemon4ksan/g-man/pkg/steam/socket"
	"github.com/lemon4ksan/g-man/pkg/steam/sys/directory"
	tr "github.com/lemon4ksan/g-man/pkg/steam/transport"
	"github.com/lemon4ksan/g-man/pkg/storage"
	"github.com/lemon4ksan/g-man/pkg/storage/memory"
)

// NewReadyClient creates a client, configures a default logger (if none provided),
// connects to the optimal CM server and performs the logon flow in one step.
func NewReadyClient(ctx context.Context, cfg Config, details *auth.LogOnDetails, opts ...Option) (*Client, error) {
	logger := log.New(log.DefaultConfig(log.LevelInfo))
	opts = append([]Option{WithLogger(logger)}, opts...)

	// Create the base client.
	c, err := NewClient(cfg, opts...)
	if err != nil {
		return nil, err
	}

	// Resolve the best CM server.
	srv, err := directory.New(c.Service()).GetOptimalCMServer(ctx)
	if err != nil {
		return nil, err
	}

	// Perform the login sequence.
	if err = c.ConnectAndLogin(ctx, srv, details); err != nil {
		return nil, err
	}

	return c, nil
}

// GetModule returns the first registered module that matches type T.
// This is a type-safe helper that eliminates the need for manual type assertions.
func GetModule[T any](c *Client) T {
	for _, m := range c.modules.All() {
		if typed, ok := m.(T); ok {
			return typed
		}
	}

	var zero T

	return zero
}

// ErrNotRunning is returned when the client is not running.
var ErrNotRunning = errors.New("client must be running (call Run() first)")

// SocketProvider defines the interface for socket operations required by the client.
type SocketProvider interface {
	auth.SocketProvider
	IsConnected() bool
	SendSync(ctx context.Context, build socket.PayloadBuilder, opts ...socket.SendOption) (*protocol.Packet, error)
	RegisterServiceHandler(method string, handler socket.Handler)
	Disconnect() error
	Close() error
}

// Config aggregates configurations for all core subsystems and standard modules.
type Config struct {
	Socket   socket.Config
	Storage  storage.Provider
	HTTP     rest.HTTPDoer // Optional custom HTTP client
	REST     *rest.Client  // Optional custom REST client (overrides HTTP if provided)
	Device   *auth.DeviceConfig
	Registry *api.UnmarshalRegistry
	Bus      *bus.Bus
	ProxyURL string // Global proxy URL (affects both HTTP and Socket)
}

// DefaultConfig returns the baseline configuration for core systems.
func DefaultConfig() Config {
	return Config{
		Socket: socket.DefaultConfig(),
	}
}

// State represents the lifecycle state of the high-level client.
type State int32

const (
	// StateNew indicates the client is initialized but not yet running.
	StateNew State = iota
	// StateRunning indicates the client's background loops are active.
	StateRunning
	// StateAuthorized indicates the client is fully authorized and ready.
	StateAuthorized
	// StateClosed indicates the client has been permanently shut down.
	StateClosed
)

// String returns a human-readable representation of the client state.
func (s State) String() string {
	switch s {
	case StateNew:
		return "new"
	case StateRunning:
		return "running"
	case StateClosed:
		return "closed"
	default:
		return "unknown"
	}
}

// Option defines a functional configuration option for custom overrides.
type Option func(*Client)

// WithLogger sets a custom logger for the Steam client.
func WithLogger(l log.Logger) Option {
	return func(c *Client) { c.logger = l }
}

// WithModule adds a module to the client and initializes it immediately.
func WithModule(m module.Module) Option {
	return func(c *Client) {
		c.modules.Add(m)
	}
}

// Client acts as the central hub connecting the cmSocket, Auth, WebSession, and Modules.
type Client struct {
	cfg    Config
	logger log.Logger
	bus    *bus.Bus

	socket  SocketProvider
	session *SessionManager
	router  *ServiceRouter
	modules *ModuleManager
	rest    *rest.Client

	ctx       context.Context
	cancel    context.CancelFunc
	closed    chan struct{}
	wg        sync.WaitGroup
	state     atomic.Int32
	closeOnce sync.Once
}

// NewClient initializes a Steam Client.
func NewClient(cfg Config, opts ...Option) (*Client, error) {
	ctx, cancel := context.WithCancel(context.Background()) // #nosec G118

	if cfg.HTTP == nil {
		cfg.HTTP = &http.Client{Timeout: 30 * time.Second}
	}

	if cfg.Storage == nil {
		cfg.Storage = memory.New()
	}

	if cfg.Registry == nil {
		cfg.Registry = api.NewUnmarshalRegistry()
	}

	if cfg.Bus == nil {
		cfg.Bus = bus.New()
	}

	c := &Client{
		cfg:    cfg,
		ctx:    ctx,
		cancel: cancel,
		bus:    cfg.Bus,
		logger: log.Discard,
		closed: make(chan struct{}),
	}

	c.modules = &ModuleManager{
		modules: make(map[string]module.Module),
		state:   &c.state,
		initCtx: c,
		authCtx: c,
	}

	for _, opt := range opts {
		opt(c)
	}

	if cfg.Socket.Connector.ProxyURL == "" {
		cfg.Socket.Connector.ProxyURL = cfg.ProxyURL
	}

	c.socket = socket.NewSocket(cfg.Socket, c.logger)
	c.session = NewSessionManager(cfg, c.bus, c.logger, c.socket)
	c.router = NewServiceRouter(c.session, c.socket)

	if cfg.REST != nil {
		c.rest = cfg.REST
	} else {
		c.rest = rest.NewClient(cfg.HTTP)
	}

	if err := c.run(); err != nil {
		return nil, err
	}

	return c, nil
}

// State returns the current client lifecycle state.
func (c *Client) State() State { return State(c.state.Load()) }

// RegisterModule adds a module to the client and initializes it immediately.
func (c *Client) RegisterModule(m module.Module) {
	if err := c.modules.Register(c.ctx, m); err != nil {
		c.logger.Error("Failed to register module",
			log.String("name", m.Name()),
			log.Err(err))
	}
}

// Module returns the registered Module with the given name.
func (c *Client) Module(name string) module.Module {
	return c.modules.Get(name)
}

// RegisterPacketHandler is a shortcut to register a socket packet handler.
func (c *Client) RegisterPacketHandler(eMsg enums.EMsg, handler socket.Handler) {
	c.socket.RegisterMsgHandler(eMsg, handler)
}

// RegisterServiceHandler is a shortcut to register a unified service handler.
func (c *Client) RegisterServiceHandler(method string, handler socket.Handler) {
	c.socket.RegisterServiceHandler(method, handler)
}

// UnregisterPacketHandler removes a packet handler.
func (c *Client) UnregisterPacketHandler(eMsg enums.EMsg) {
	c.socket.RegisterMsgHandler(eMsg, nil)
}

// UnregisterServiceHandler removes a service handler.
func (c *Client) UnregisterServiceHandler(method string) {
	c.socket.RegisterServiceHandler(method, nil)
}

// Storage returns the client's storage provider.
func (c *Client) Storage() storage.Provider { return c.session.Storage() }

// Bus returns the internal event bus.
func (c *Client) Bus() *bus.Bus { return c.bus }

// Socket returns the underlying socket provider.
func (c *Client) Socket() SocketProvider { return c.socket }

// Logger returns the client's logger.
func (c *Client) Logger() log.Logger { return c.logger }

// Rest returns the low-level REST requester.
func (c *Client) Rest() rest.Requester { return c.rest }

// Service returns the Doer interface for making API requests.
func (c *Client) Service() service.Doer {
	return c.router
}

// Community returns the Steam Community requester. Returns nil if not authenticated.
func (c *Client) Community() community.Requester {
	return c.session.community
}

// SteamID returns the logged-in SteamID.
func (c *Client) SteamID() id.ID {
	if sess := c.socket.Session(); sess != nil {
		return id.ID(sess.SteamID())
	}

	return 0
}

// Do implements the [service.Doer] interface.
// It automatically selects between SocketProvider and HTTP transport and handles silent token refresh.
func (c *Client) Do(ctx context.Context, req *tr.Request) (*tr.Response, error) {
	if c.State() != StateRunning {
		return nil, ErrNotRunning
	}

	return c.router.Do(ctx, req)
}

// ConnectAndLogin connects to the CM and performs the login sequence.
func (c *Client) ConnectAndLogin(ctx context.Context, server socket.CMServer, details *auth.LogOnDetails) error {
	if c.State() == StateClosed {
		return module.ErrClosed
	}

	if err := c.session.LogOn(ctx, server, details); err != nil {
		return err
	}

	c.state.Store(int32(StateAuthorized))

	if err := c.modules.StartAuthedAll(c.ctx, c); err != nil {
		c.logger.Error("Some modules failed to start authorized", log.Err(err))
		return err
	}

	return nil
}

// Disconnect closes the CM connection but keeps the client running.
func (c *Client) Disconnect() error {
	return c.session.Disconnect()
}

// Close shuts down the client, stops all modules, and releases resources.
func (c *Client) Close() error {
	var err error
	c.closeOnce.Do(func() {
		c.state.Store(int32(StateClosed))
		c.cancel()
		c.wg.Wait()
		err = c.session.Close()
		close(c.closed)
	})

	return err
}

// Wait blocks until the client is fully stopped.
func (c *Client) Wait() {
	<-c.closed
}

func (c *Client) run() (err error) {
	if err := c.modules.InitAll(c); err != nil {
		err = fmt.Errorf("init modules: %w", err)
		return err
	}

	if err := c.modules.StartAll(c.ctx); err != nil {
		err = fmt.Errorf("start modules: %w", err)
		return err
	}

	c.wg.Go(func() {
		_ = c.session.StartRefreshLoop(c.ctx)
	})

	c.state.Store(int32(StateRunning))

	return err
}

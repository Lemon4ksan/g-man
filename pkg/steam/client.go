// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package steam provides the main entry point and orchestrator for the Steam library.
package steam

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"

	"github.com/lemon4ksan/g-man/pkg/log"
	"github.com/lemon4ksan/g-man/pkg/modules/auth"
	"github.com/lemon4ksan/g-man/pkg/rest"
	"github.com/lemon4ksan/g-man/pkg/steam/api"
	"github.com/lemon4ksan/g-man/pkg/steam/bus"
	"github.com/lemon4ksan/g-man/pkg/steam/protocol"
	"github.com/lemon4ksan/g-man/pkg/steam/socket"
	tr "github.com/lemon4ksan/g-man/pkg/steam/transport"
)

// InitContext provides the module with access to the necessary client resources
// during the initialization phase, without exposing lifecycle management methods (Close, Connect).
type InitContext interface {
	// Bus provides access to the event bus for subscribing/publishing internal messages.
	Bus() *bus.Bus

	// Logger returns the configured logger.
	Logger() log.Logger

	// Proto returns the UnifiedClient for making requests over the CM Socket.
	Proto() *api.UnifiedClient

	// RegisterPacketHandler registers a handler for low-level EMsg (TCP/UDP).
	RegisterPacketHandler(eMsg protocol.EMsg, handler socket.Handler)

	// RegisterServiceHandler registers a handler for Protobuf services (Unified Services).
	RegisterServiceHandler(method string, handler socket.Handler)

	// GetModule allows you to find another module if there are dependencies between them.
	GetModule(name string) Module

	// UnregisterPacketHandler removes the handler from socket for freeing memory.
	UnregisterPacketHandler(eMsg protocol.EMsg)

	// UnregisterServiceHandler removes the service handler from socket for freeing memory.
	UnregisterServiceHandler(method string)
}

type AuthContext interface {
	// Community returns an authorized community client.
	Community() *api.CommunityClient

	// SteamID returns the steam id of the authorized user.
	SteamID() uint64
}

// Module defines the contract for pluggable extensions (e.g., Trade, Chat, GC).
type Module interface {
	Name() string

	// Init is called during client creation. Use this to register packet handlers
	// and subscribe to bus events.
	Init(init InitContext) error

	// Start is called when the client starts running. Use this to launch
	// background tasks (tickers, pollers). The context is cancelled when the client closes.
	Start(ctx context.Context) error
}

// ModuleAuth defines the contract for pluggable extensions that require authorized clients.
type ModuleAuth interface {
	Module

	// StartAuthed is called after a successful Steam login and WebSession creation.
	StartAuthed(ctx context.Context, auth AuthContext) error
}

// State represents the lifecycle state of the high-level client.
type State int32

const (
	StateNew State = iota
	StateRunning
	StateClosed
)

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

var (
	ErrClientClosed = errors.New("steam: client is closed")
)

// Config aggregates configurations for all core subsystems.
type Config struct {
	Socket socket.Config
	Auth   auth.Config

	// HTTPClient is optional. If nil, a default client is used.
	HTTPClient rest.HTTPDoer
}

func DefaultConfig() Config {
	return Config{
		Socket: socket.DefaultConfig(),
		Auth:   auth.DefaultConfig(),
	}
}

// Option defines a functional configuration option for the Client.
type Option func(*Client)

func WithLogger(l log.Logger) Option {
	return func(c *Client) { c.logger = l }
}

func WithModule(m Module) Option {
	return func(c *Client) { c.modules[m.Name()] = m }
}

// Client acts as the central hub connecting the Socket, Auth, WebSession, and Modules.
type Client struct {
	// Configuration & Dependencies
	cfg    Config
	logger log.Logger
	bus    *bus.Bus

	// Core Components
	socket     *socket.Socket
	auth       *auth.Authenticator
	webSession *auth.WebSession // Concrete type, can be nil before login
	community  *api.CommunityClient

	// API Clients
	webTransport    tr.Transport
	unifiedClient   *api.UnifiedClient // WebAPI (HTTP)
	socketAPIClient *api.UnifiedClient // CM (TCP/WS)

	// State & Lifecycle
	state   atomic.Int32
	mu      sync.RWMutex
	modules map[string]Module

	ctx    context.Context    // Global client context
	cancel context.CancelFunc // Cancels everything on Close()
	done   chan struct{}      // Closed when fully stopped
	wg     sync.WaitGroup
}

// NewClient initializes a Steam Client with the provided config and options.
func NewClient(cfg Config, opts ...Option) *Client {
	ctx, cancel := context.WithCancel(context.Background())

	c := &Client{
		cfg:     cfg,
		logger:  log.Discard,
		bus:     bus.NewBus(),
		modules: make(map[string]Module),
		ctx:     ctx,
		cancel:  cancel,
		done:    make(chan struct{}),
	}

	for _, opt := range opts {
		opt(c)
	}

	c.webTransport = tr.NewHTTPTransport(cfg.HTTPClient, api.WebAPIBase)
	c.unifiedClient = api.NewUnifiedClient(c.webTransport)

	// We pass the global client context to the socket so it dies when we die.
	c.socket = socket.NewSocket(
		ctx,
		cfg.Socket,
		socket.WithBus(c.bus),
		socket.WithLogger(c.logger), // Logger will be wrapped inside
	)

	// Auth needs a UnifiedClient (HTTP) to perform the initial handshake.
	authService := auth.NewAuthenticationService(c.unifiedClient, nil)
	c.auth = auth.NewAuthenticator(
		c.socket,
		authService,
		cfg.Auth,
		auth.WithLogger(c.logger),
	)

	// Initialize Socket API Client (Lazy-ready)
	// This client uses the socket transport. It works only when connected.
	socketTransport := tr.NewSocketTransport(c.socket)
	c.socketAPIClient = api.NewUnifiedClient(socketTransport)

	for name, mod := range c.modules {
		if err := mod.Init(c); err != nil {
			c.logger.Error("Failed to init module", log.String("name", name), log.Err(err))
		}
	}

	// Start lifecycle monitor
	c.wg.Add(1)
	go c.run()

	return c
}

// GetModule returns the registered Module with the given name.
func (c *Client) GetModule(name string) Module {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.modules[name]
}

// ConnectAndLogin connects to the CM and performs the login sequence.
// This is a helper that combines Socket.Connect and Auth.LogOn.
func (c *Client) ConnectAndLogin(ctx context.Context, server socket.CMServer, details *auth.LogOnDetails) error {
	if c.State() == StateClosed {
		return ErrClientClosed
	}

	if err := c.socket.Connect(ctx, server); err != nil {
		return fmt.Errorf("connect: %w", err)
	}

	if err := c.auth.LogOn(ctx, details, server); err != nil {
		return fmt.Errorf("login: %w", err)
	}

	c.mu.Lock()
	sess := c.socket.Session()
	if sess == nil {
		return errors.New("session lost after login")
	}
	steamID := sess.SteamID()
	c.webSession = auth.NewWebSession(steamID, c.logger)
	c.mu.Unlock()

	// We use the Socket API Client for fetching because we are already logged in via TCP
	socketAuthSvc := auth.NewAuthenticationService(c.socketAPIClient, nil)

	if err := c.webSession.Authenticate(ctx, socketAuthSvc, details.RefreshToken); err != nil {
		c.logger.Warn("Failed to establish web session", log.Err(err))
		// We don't return error here, because TCP login was successful.
		// Trade bot might work partially (chat works, offers might not).
	} else {
		c.logger.Info("Web session established")
	}

	c.wg.Add(1)
	go c.startAuthed()

	return nil
}

// Disconnect closes the connection but keeps the client running (modules stay active).
func (c *Client) Disconnect() {
	c.mu.Lock()
	c.community = nil
	c.mu.Unlock()
	c.socket.Disconnect()
}

// Close shuts down the client, stops all modules, and releases resources.
func (c *Client) Close() error {
	c.cancel()
	c.wg.Wait()
	return nil
}

// Wait blocks until the client is fully stopped.
func (c *Client) Wait() {
	<-c.done
}

func (c *Client) State() State              { return State(c.state.Load()) }
func (c *Client) Bus() *bus.Bus             { return c.bus }
func (c *Client) Socket() *socket.Socket    { return c.socket }
func (c *Client) Auth() *auth.Authenticator { return c.auth }
func (c *Client) Logger() log.Logger        { return c.logger }

// API returns the UnifiedClient for making HTTP WebAPI requests.
// It automatically injects the AccessToken if available.
func (c *Client) API() *api.UnifiedClient {
	c.mu.RLock()
	defer c.mu.RUnlock()

	// If we have a session, inject the token for every request
	if sess := c.socket.Session(); sess != nil {
		return c.unifiedClient.WithAccessToken(sess.AccessToken())
	}
	return c.unifiedClient
}

// Proto returns the UnifiedClient for making requests over the CM Socket.
// This is preferred for most operations as it avoids HTTP rate limits.
func (c *Client) Proto() *api.UnifiedClient {
	return c.socketAPIClient
}

// Community returns a client for interacting with the Steam Community website.
// Returns nil if the web session is not established.
func (c *Client) Community() *api.CommunityClient {
	c.mu.RLock()

	if c.community != nil {
		defer c.mu.RUnlock()
		return c.community
	}

	ws := c.webSession
	c.mu.RUnlock()

	if ws == nil || !ws.IsAuthenticated() {
		return nil
	}

	// Create a transport using the authenticated WebSession client (CookieJar)
	tr := tr.NewHTTPTransport(ws.Client(), api.CommunityBase)
	c.community = api.NewCommunityClient(tr, ws.SessionID, c.logger)
	return c.community
}

// SteamID returns the logged-in SteamID, or 0.
func (c *Client) SteamID() uint64 {
	if sess := c.socket.Session(); sess != nil {
		return sess.SteamID()
	}
	return 0
}

// RegisterPacketHandler is a shortcut to register a socket packet handler.
func (c *Client) RegisterPacketHandler(eMsg protocol.EMsg, handler socket.Handler) {
	c.socket.RegisterMsgHandler(eMsg, handler)
}

// RegisterServiceHandler is a shortcut to register a unified service handler.
func (c *Client) RegisterServiceHandler(method string, handler socket.Handler) {
	c.socket.RegisterServiceHandler(method, handler)
}

// UnregisterPacketHandler removes the handler from socket for freeing memory.
func (c *Client) UnregisterPacketHandler(eMsg protocol.EMsg) {
	c.socket.RegisterMsgHandler(eMsg, nil)
}

// UnregisterServiceHandler removes the service handler from socket for freeing memory.
func (c *Client) UnregisterServiceHandler(method string) {
	c.socket.RegisterServiceHandler(method, nil)
}

func (c *Client) run() {
	defer c.wg.Done()
	c.state.Store(int32(StateRunning))

	// Start all modules
	c.mu.RLock()
	for name, mod := range c.modules {
		if err := mod.Start(c.ctx); err != nil {
			c.logger.Error("Failed to start module", log.String("name", name), log.Err(err))
		}
	}
	c.mu.RUnlock()

	<-c.ctx.Done()

	// Close all modules
	c.mu.RLock()
	for name, mod := range c.modules {
		if closer, ok := mod.(interface{ Close() error }); ok {
			if err := closer.Close(); err != nil {
				c.logger.Error("Failed to close module", log.String("name", name), log.Err(err))
			}
		}
	}
	c.mu.RUnlock()

	c.socket.Close()
	c.bus.Close()
	close(c.done)
	c.state.Store(int32(StateClosed))
}

func (c *Client) startAuthed() {
	for name, mod := range c.modules {
		if authed, ok := mod.(ModuleAuth); ok {
			if err := authed.StartAuthed(c.ctx, c); err != nil {
				c.logger.Error("Failed to start authed module", log.String("name", name), log.Err(err))
			}
		}
	}
}

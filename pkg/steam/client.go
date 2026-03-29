// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package steam provides the main entry point and orchestrator for the Steam library.
package steam

import (
	"context"
	"fmt"
	"maps"
	"sync"
	"sync/atomic"

	"github.com/lemon4ksan/g-man/pkg/log"
	"github.com/lemon4ksan/g-man/pkg/modules"
	"github.com/lemon4ksan/g-man/pkg/modules/auth"
	"github.com/lemon4ksan/g-man/pkg/rest"
	"github.com/lemon4ksan/g-man/pkg/steam/bus"
	"github.com/lemon4ksan/g-man/pkg/steam/community"
	"github.com/lemon4ksan/g-man/pkg/steam/protocol"
	"github.com/lemon4ksan/g-man/pkg/steam/service"
	"github.com/lemon4ksan/g-man/pkg/steam/socket"
	tr "github.com/lemon4ksan/g-man/pkg/steam/transport"
	"github.com/lemon4ksan/g-man/pkg/storage"
	"github.com/lemon4ksan/g-man/pkg/storage/memory"
)

// Config aggregates configurations for all core subsystems and standard modules.
type Config struct {
	Socket  socket.Config
	Auth    auth.Config
	Storage storage.Provider
	HTTP    rest.HTTPDoer // Optional custom HTTP client
}

// DefaultConfig returns the baseline configuration for core systems.
func DefaultConfig() Config {
	return Config{
		Socket: socket.DefaultConfig(),
		Auth:   auth.DefaultConfig(),
	}
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

// Option defines a functional configuration option for custom overrides.
type Option func(*Client)

func WithLogger(l log.Logger) Option {
	return func(c *Client) { c.logger = l }
}

// Client acts as the central hub connecting the Socket, Auth, WebSession, and Modules.
type Client struct {
	// Configuration & Dependencies
	cfg     Config
	logger  log.Logger
	bus     *bus.Bus
	storage storage.Provider

	// Core Components
	socket     *socket.Socket
	auth       *auth.Authenticator
	webSession *auth.WebSession
	community  *community.Client

	// API Clients
	restClient      *rest.Client
	unifiedClient   *service.Client // WebAPI (HTTP)
	socketAPIClient *service.Client // CM (TCP/WS)

	// State & Lifecycle
	state   atomic.Int32
	mu      sync.RWMutex
	modules map[string]modules.Module

	ctx    context.Context    // Global client context
	cancel context.CancelFunc // Cancels everything on Close()
	done   chan struct{}      // Closed when fully stopped
	wg     sync.WaitGroup
}

// NewClient initializes a Steam Client.
func NewClient(cfg Config, opts ...Option) *Client {
	ctx, cancel := context.WithCancel(context.Background())

	// Fallback to in-memory storage if none provided
	if cfg.Storage == nil {
		cfg.Storage = memory.New()
	}

	c := &Client{
		cfg:     cfg,
		logger:  log.Discard,
		bus:     bus.NewBus(),
		storage: cfg.Storage,
		modules: make(map[string]modules.Module),
		ctx:     ctx,
		cancel:  cancel,
		done:    make(chan struct{}),
	}

	for _, opt := range opts {
		opt(c)
	}

	webTransport := tr.NewHTTPTransport(cfg.HTTP, service.WebAPIBase)
	c.unifiedClient = service.New(webTransport)
	c.restClient = rest.NewClient(cfg.HTTP)

	c.socket = socket.NewSocket(
		cfg.Socket,
		socket.WithBus(c.bus),
		socket.WithLogger(c.logger),
	)

	// Initialize Auth with Storage Support
	authService := auth.NewAuthenticationService(c.unifiedClient, nil)
	c.auth = auth.NewAuthenticator(
		c.socket,
		authService,
		cfg.Auth,
		auth.WithLogger(c.logger),
		auth.WithStorage(cfg.Storage.AuthStore()),
	)

	socketTransport := tr.NewSocketTransport(c.socket)
	c.socketAPIClient = service.New(socketTransport)

	for name, mod := range c.modules {
		if err := mod.Init(c); err != nil {
			c.logger.Error("Failed to init module", log.String("name", name), log.Err(err))
		}
	}

	c.wg.Add(1)
	go c.run()

	return c
}

// ConnectAndLogin connects to the CM and performs the login sequence.
func (c *Client) ConnectAndLogin(ctx context.Context, server socket.CMServer, details *auth.LogOnDetails) error {
	if c.State() == StateClosed {
		return modules.ErrClientClosed
	}

	if err := c.auth.LogOn(ctx, details, server); err != nil {
		return fmt.Errorf("login: %w", err)
	}

	c.mu.Lock()
	c.webSession = auth.NewWebSession(details.SteamID, c.logger)
	c.mu.Unlock()

	c.wg.Go(func() {
		defer c.startAuthed()

		socketAuthSvc := auth.NewAuthenticationService(c.socketAPIClient, nil)
		c.logger.Debug("Exchanging saved Refresh Token for Access Token...", log.Uint64("steam_id", details.SteamID))

		resp, err := socketAuthSvc.GenerateAccessTokenForApp(ctx, details.RefreshToken, details.SteamID)
		if err != nil {
			c.logger.Warn("Saved token expired or rejected", log.Err(err))
		} else {
			details.AccessToken = resp.GetAccessToken()
			c.logger.Debug("Successfully generated Access Token for CM")
		}

		err = c.webSession.Authenticate(c.ctx, socketAuthSvc.DeviceConf().PlatformType, details.RefreshToken, details.AccessToken)
		if err != nil {
			c.logger.Warn("Web session failed", log.Err(err))
			return
		}

		c.logger.Info("Web session ready")

		c.mu.Lock()
		comm := community.New(c.webSession.Client().HTTP(), c.webSession.SessionID, c.logger)
		c.community = comm
		c.mu.Unlock()

		apiKey, err := comm.GetOrRegisterAPIKey(c.ctx, "g-man-bot.dev")
		if err != nil {
			c.logger.Warn("Could not auto-fetch API Key", log.Err(err))
			return
		}

		c.logger.Info("WebAPI Key acquired automatically", log.String("key", apiKey[:4]+"***"))

		c.mu.Lock()
		c.unifiedClient.WithAPIKey(apiKey)
		c.unifiedClient.WithAccessToken(details.AccessToken)
		c.socketAPIClient.WithAPIKey(apiKey)
		c.socketAPIClient.WithAccessToken(details.AccessToken)
		c.mu.Unlock()
	})

	return nil
}

// Do implements the [service.Requester] interface.
// This makes the Client a "smart proxy" that selects the transport on the fly.
func (c *Client) Do(ctx context.Context, req *tr.Request) (*tr.Response, error) {
	c.mu.RLock()
	_, isSocketCompatible := req.Target().(tr.SocketTarget)
	isConnected := c.socket.State() == socket.StateConnected
	
	var selectedRequester service.Requester
	if isConnected && isSocketCompatible {
		selectedRequester = c.socketAPIClient
	} else {
		selectedRequester = c.unifiedClient
	}
	c.mu.RUnlock()

	return selectedRequester.Do(ctx, req)
}

// WithCustomModule allows adding non-standard (user-defined) modules.
func (c *Client) RegisterModule(m modules.Module) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.modules[m.Name()] = m
}

// Module returns the registered Module with the given name.
func (c *Client) Module(name string) modules.Module {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.modules[name]
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

func (c *Client) Storage() storage.Provider { return c.storage }
func (c *Client) State() State              { return State(c.state.Load()) }
func (c *Client) Bus() *bus.Bus             { return c.bus }
func (c *Client) Socket() *socket.Socket    { return c.socket }
func (c *Client) Logger() log.Logger        { return c.logger }
func (c *Client) Rest() rest.Requester      { return c.restClient }

// Service returns the client for making HTTP WebAPI, Unified and Legacy requests.
func (c *Client) Service() service.Requester {
	return c
}

// Community returns a client for interacting with the Steam Community website.
// Returns nil if the web session is not established.
func (c *Client) Community() community.Requester {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if c.community != nil && c.webSession != nil && c.webSession.IsAuthenticated() {
		return c.community
	}
	return nil
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
	c.mu.RLock()
	mods := make(map[string]modules.Module, len(c.modules))
	maps.Copy(mods, c.modules)
	c.mu.RUnlock()

	for name, mod := range mods {
		if authed, ok := mod.(modules.ModuleAuth); ok {
			if err := authed.StartAuthed(c.ctx, c); err != nil {
				c.logger.Error("Failed to start authed module", log.String("name", name), log.Err(err))
			}
		}
	}
}

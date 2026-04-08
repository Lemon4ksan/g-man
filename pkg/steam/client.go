// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package steam

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"sync"
	"sync/atomic"
	"time"

	"github.com/lemon4ksan/g-man/pkg/log"
	"github.com/lemon4ksan/g-man/pkg/modules"
	"github.com/lemon4ksan/g-man/pkg/modules/auth"
	"github.com/lemon4ksan/g-man/pkg/rest"
	"github.com/lemon4ksan/g-man/pkg/steam/api"
	"github.com/lemon4ksan/g-man/pkg/steam/bus"
	"github.com/lemon4ksan/g-man/pkg/steam/community"
	"github.com/lemon4ksan/g-man/pkg/steam/protocol"
	"github.com/lemon4ksan/g-man/pkg/steam/service"
	"github.com/lemon4ksan/g-man/pkg/steam/socket"
	"github.com/lemon4ksan/g-man/pkg/steam/steamid"
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

	reauthMu sync.Mutex
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

	c.wg.Go(c.run)

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

		if err := c.RefreshSession(ctx); err != nil {
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
		c.unifiedClient = c.unifiedClient.WithAPIKey(apiKey)
		c.socketAPIClient = c.socketAPIClient.WithAPIKey(apiKey)
		c.mu.Unlock()
	})

	return nil
}

// Do implements the [service.Doer] interface.
// This makes the Client a "smart proxy" that selects the transport on the fly.
func (c *Client) Do(ctx context.Context, req *tr.Request) (*tr.Response, error) {
	resp, err := c.performDo(ctx, req)

	if err != nil && errors.Is(err, api.ErrSessionExpired) {
		c.logger.Warn("Session expired detected during request, attempting silent refresh...")

		if refreshErr := c.RefreshSession(c.ctx); refreshErr != nil {
			return nil, fmt.Errorf("session refresh failed: %w", refreshErr)
		}

		return c.performDo(ctx, req)
	}

	return resp, err
}

// RefreshSession is the central method for refreshing all tokens.
func (c *Client) RefreshSession(ctx context.Context) error {
	c.reauthMu.Lock()
	defer c.reauthMu.Unlock()

	if isAlive, _ := c.webSession.Verify(ctx); isAlive {
		return nil
	}

	c.logger.Info("Refreshing Steam session tokens...")

	sess := c.socket.Session()
	if sess == nil {
		return errors.New("cannot refresh session: socket is not connected")
	}

	socketAuthSvc := auth.NewAuthenticationService(c.socketAPIClient, nil)
	c.logger.Debug("Exchanging saved Refresh Token for Access Token...", log.SteamID(sess.SteamID()))

	resp, err := socketAuthSvc.GenerateAccessTokenForApp(ctx, sess.RefreshToken(), sess.SteamID())
	if err != nil {
		return fmt.Errorf("failed to generate access token: %w", err)
	}

	newAccessToken := resp.GetAccessToken()
	sess.SetAccessToken(newAccessToken)

	c.mu.Lock()
	c.unifiedClient = c.unifiedClient.WithAccessToken(newAccessToken)
	c.socketAPIClient = c.socketAPIClient.WithAccessToken(newAccessToken)
	c.mu.Unlock()

	err = c.webSession.Authenticate(c.ctx, socketAuthSvc.DeviceConf().PlatformType, sess.RefreshToken(), sess.AccessToken())
	if err != nil {
		c.logger.Warn("Web session failed", log.Err(err))
		return fmt.Errorf("web auth failed during refresh: %w", err)
	}

	c.bus.Publish(&auth.WebSessionReadyEvent{})
	return nil
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
	c.state.Store(int32(StateClosed))
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
func (c *Client) Service() service.Doer {
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
func (c *Client) SteamID() steamid.ID {
	if sess := c.socket.Session(); sess != nil {
		return steamid.ID(sess.SteamID())
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

func (c *Client) performDo(ctx context.Context, req *tr.Request) (*tr.Response, error) {
	c.mu.RLock()
	uClient := c.unifiedClient
	sClient := c.socketAPIClient
	isConnected := c.socket.State() == socket.StateConnected
	c.mu.RUnlock()

	_, isSocketCompatible := req.Target().(tr.SocketTarget)

	var selected service.Doer
	if isConnected && isSocketCompatible {
		selected = sClient
	} else {
		selected = uClient
	}

	if selected == nil {
		return nil, errors.New("no transport available")
	}

	return selected.Do(ctx, req)
}

func (c *Client) run() {
	c.state.Store(int32(StateRunning))

	// Start all modules
	c.mu.RLock()
	for name, mod := range c.modules {
		if err := mod.Start(c.ctx); err != nil {
			c.logger.Error("Failed to start module", log.String("name", name), log.Err(err))
		}
	}
	c.mu.RUnlock()

	verifyTicker := time.NewTicker(5 * time.Minute)
	defer verifyTicker.Stop()

	for {
		select {
		case <-c.ctx.Done():
			goto shutdown
		case <-verifyTicker.C:
			if c.State() == StateRunning && c.webSession.IsAuthenticated() {
				go func() {
					isAlive, _ := c.webSession.Verify(c.ctx)
					if !isAlive && c.ctx.Err() == nil {
						c.RefreshSession(c.ctx)
					}
				}()
			}
		}
	}

shutdown:
	c.logger.Debug("Orchestrator shutting down...")
	c.socket.Disconnect()

	// Close all modules
	c.mu.RLock()
	allModules := make([]modules.Module, 0, len(c.modules))
	for _, m := range c.modules {
		allModules = append(allModules, m)
	}
	c.mu.RUnlock()

	for _, mod := range allModules {
		if closer, ok := mod.(interface{ Close() error }); ok {
			if err := closer.Close(); err != nil {
				c.logger.Error("Failed to close module", log.String("name", mod.Name()), log.Err(err))
			}
		}
	}

	c.socket.Close()
	c.bus.Close()
	close(c.done)
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

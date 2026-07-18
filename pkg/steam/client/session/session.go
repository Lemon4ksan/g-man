// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package session provides a session orchestrator for Steam connections.
package session

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/lemon4ksan/aoni"
	"github.com/lemon4ksan/miyako/bus"
	"github.com/lemon4ksan/miyako/generic"
	"github.com/lemon4ksan/miyako/log"

	pb "github.com/lemon4ksan/g-man/pkg/protobuf/steam"
	"github.com/lemon4ksan/g-man/pkg/steam/auth"
	"github.com/lemon4ksan/g-man/pkg/steam/auth/websession"
	"github.com/lemon4ksan/g-man/pkg/steam/community"
	"github.com/lemon4ksan/g-man/pkg/steam/id"
	"github.com/lemon4ksan/g-man/pkg/steam/module"
	"github.com/lemon4ksan/g-man/pkg/steam/protocol"
	"github.com/lemon4ksan/g-man/pkg/steam/service"
	"github.com/lemon4ksan/g-man/pkg/steam/socket"
	tr "github.com/lemon4ksan/g-man/pkg/steam/transport"
	"github.com/lemon4ksan/g-man/pkg/storage"
	"github.com/lemon4ksan/g-man/pkg/storage/memory"
)

var (
	// ErrMissingCredentials is returned when no cached credentials are available.
	ErrMissingCredentials = errors.New("session: missing required credentials")
	// ErrSocketNotConnected is returned by [Session.SetAccessToken] when the socket is not connected.
	ErrSocketNotConnected = errors.New("session: cannot refresh session: socket is not connected")
	// ErrNoCommunityClient is returned by [Session.GetOrRegisterAPIKey] when the community client is not available.
	ErrNoCommunityClient = errors.New("session: no community client available")
)

// AuthenticatorProvider defines the contract for logging on to the Steam server.
type AuthenticatorProvider interface {
	// LogOn performs a network logon sequence using the provided credentials.
	LogOn(ctx context.Context, details *auth.LogOnDetails, server socket.CMServer) error
}

// WebSessionProvider defines the contract for managing OIDC web sessions and cookie jars.
type WebSessionProvider interface {
	// HTTP returns the underlying HTTP client containing session cookies.
	HTTP() *http.Client
	// SessionID returns the unique session ID string for the given base URL.
	SessionID(baseURL string) string
	// Verify checks if the current web session is active and valid.
	Verify(ctx context.Context) (bool, error)
	// Authenticate performs OIDC authentication using the given OAuth tokens.
	Authenticate(ctx context.Context, platformType pb.EAuthTokenPlatformType, refreshToken, accessToken string) error
	// IsAuthenticated reports whether the session has valid active credentials.
	IsAuthenticated() bool
}

// SocketProvider defines the network socket operations required by [Session].
type SocketProvider interface {
	auth.SocketProvider
	// IsConnected reports whether the network socket is actively connected.
	IsConnected() bool
	// UpdateLogger sets a new logger instance for the socket.
	UpdateLogger(logger log.Logger)
	// UpdateServers updates the client's internal list of Connection Manager servers.
	UpdateServers(servers []socket.CMServer)
	// Send transmits a message asynchronously over the socket.
	Send(ctx context.Context, build socket.PayloadBuilder, opts ...socket.SendOption) error
	// SendSync transmits a message and blocks until a matching response is received.
	SendSync(ctx context.Context, build socket.PayloadBuilder, opts ...socket.SendOption) (*protocol.Packet, error)
	// RegisterServiceHandler registers a handler function for incoming unified service messages.
	RegisterServiceHandler(method string, handler socket.Handler)
	// Disconnect gracefully shuts down the current socket connection.
	Disconnect() error
	// Close permanently releases all socket resources.
	Close() error
}

// WebSessionFactory constructs a custom [WebSessionProvider] instance.
type WebSessionFactory func(steamID id.ID, logger log.Logger, baseDoer aoni.HTTPDoer) WebSessionProvider

// CommunityClientFactory constructs a custom [CommunityProvider] instance.
type CommunityClientFactory func(httpClient *http.Client, sess community.SessionProvider, logger log.Logger) community.Requester

// Config decouples configuration for [Session] from global client parameters.
// Use [Config.ResolveDefaults] to initialize default fallback values.
type Config struct {
	// RefreshJobInterval specifies the interval at which the refresh loop runs.
	// Default: 5 minutes.
	RefreshJobInterval time.Duration
	// Device specifies the hardware and platform information used during logon.
	// Default: [auth.DefaultDeviceConfig].
	Device *auth.DeviceConfig
	// Storage provides persistent key-value storage for cached sessions.
	// Default: [memory.New].
	Storage storage.Provider
	// HTTP defines the HTTP request executor for WebAPI calls.
	// Default client is configured with 30 second timeout and default HTTP transport.
	HTTP aoni.HTTPDoer
	// WebAPIBase sets the target base URL for WebAPI requests.
	// Default: [service.WebAPIBase].
	WebAPIBase string
	// Bus is the internal event bus used to dispatch session events.
	Bus *bus.Bus
	// Logger is the logger instance used by the session manager.
	Logger log.Logger
	// Authenticator is the provider used to authenticate with Steam CM servers.
	// Default: [auth.NewAuthenticator].
	Authenticator AuthenticatorProvider
	// WebFactory constructs the provider used for web authentication.
	// Default: [websession.New].
	WebFactory WebSessionFactory
	// CommunityFactory constructs the provider used for community interactions.
	// Default: [community.New].
	CommunityFactory CommunityClientFactory
}

// ResolveDefaults initializes default values for unconfigured [Config] fields.
func (cfg *Config) ResolveDefaults() {
	if cfg.RefreshJobInterval == 0 {
		cfg.RefreshJobInterval = 5 * time.Minute
	}

	if cfg.Logger == nil {
		cfg.Logger = log.Discard
	}

	if cfg.Bus == nil {
		cfg.Bus = bus.New()
	}

	if cfg.Storage == nil {
		cfg.Storage = memory.New()
	}

	if cfg.WebAPIBase == "" {
		cfg.WebAPIBase = service.WebAPIBase
	}

	if cfg.HTTP == nil {
		cfg.HTTP = &http.Client{Timeout: 30 * time.Second}
	}

	if cfg.Device == nil {
		d := auth.DefaultDeviceConfig()
		cfg.Device = &d
	}

	if cfg.WebFactory == nil {
		cfg.WebFactory = func(steamID id.ID, logger log.Logger, baseDoer aoni.HTTPDoer) WebSessionProvider {
			return websession.New(steamID, logger, baseDoer)
		}
	}

	if cfg.CommunityFactory == nil {
		cfg.CommunityFactory = func(httpClient *http.Client, sess community.SessionProvider, logger log.Logger) community.Requester {
			return community.NewClient(httpClient, sess).WithLogger(logger)
		}
	}
}

// Session orchestrates and maintains the lifetime of all Steam sessions (Socket & Web).
// It manages OAuth2 token lifetime, cookie synchronization, and background verification.
// Use [New] to construct a new session manager instance.
type Session struct {
	mu sync.RWMutex

	auth      AuthenticatorProvider
	web       WebSessionProvider
	community community.Requester
	socket    SocketProvider
	logger    log.Logger
	storage   storage.Provider
	device    *auth.DeviceConfig
	bus       *bus.Bus
	http      aoni.HTTPDoer

	webFactory       WebSessionFactory
	communityFactory CommunityClientFactory

	unified   *service.Client // WebAPI Client (HTTP)
	socketAPI *service.Client // CM Client (TCP/WS)

	refreshLoopOnce    sync.Once
	refreshJobInterval time.Duration
	refreshSF          *generic.SingleFlight[struct{}]

	closed atomic.Bool

	logonDetails *auth.LogOnDetails
	logonServer  socket.CMServer

	enrichedAccount string
	enrichedSteamID id.ID
}

// New creates a new standalone, cohesive [Session] instance.
// Falls back to standard defaults via [Config.ResolveDefaults] if [Config] is empty.
func New(socket SocketProvider, cfg Config) *Session {
	cfg.ResolveDefaults()

	unified := service.New(tr.NewHTTPTransport(cfg.HTTP, cfg.WebAPIBase))

	if cfg.Authenticator == nil {
		cfg.Authenticator = auth.NewAuthenticator(
			socket,
			auth.NewAuthenticationService(unified, cfg.Device),
			cfg.Bus,
			auth.WithLogger(cfg.Logger),
			auth.WithStorage(auth.NewKVStore(cfg.Storage.KV("auth"))),
		)
	}

	return &Session{
		auth:               cfg.Authenticator,
		socket:             socket,
		logger:             cfg.Logger.With(log.Module("session_manager")),
		storage:            cfg.Storage,
		device:             cfg.Device,
		bus:                cfg.Bus,
		http:               cfg.HTTP,
		webFactory:         cfg.WebFactory,
		communityFactory:   cfg.CommunityFactory,
		unified:            unified,
		socketAPI:          service.New(tr.NewSocketTransport(socket)),
		refreshSF:          generic.NewSingleFlight[struct{}](),
		refreshJobInterval: cfg.RefreshJobInterval,
	}
}

// Storage returns the configured persistent [storage.Provider] instance.
func (c *Session) Storage() storage.Provider { return c.storage }

// SteamID returns the logged-in user's [id.ID] if a socket session exists.
// Returns 0 if no active socket session is found.
func (c *Session) SteamID() id.ID {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if c.logonDetails != nil && c.logonDetails.SteamID != 0 {
		return c.logonDetails.SteamID
	}

	if sess := c.socket.Session(); sess != nil {
		return id.ID(sess.SteamID())
	}

	return 0
}

// AccessToken returns the current OAuth2 access token if a socket session is active.
// Returns an empty string if no session exists.
func (c *Session) AccessToken() string {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if c.logonDetails != nil && c.logonDetails.AccessToken != "" {
		return c.logonDetails.AccessToken
	}

	if sess := c.socket.Session(); sess != nil {
		return sess.AccessToken()
	}

	return ""
}

// RefreshToken returns the current OAuth2 refresh token if a socket session is active.
// Returns an empty string if no session exists.
func (c *Session) RefreshToken() string {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if c.logonDetails != nil && c.logonDetails.RefreshToken != "" {
		return c.logonDetails.RefreshToken
	}

	if sess := c.socket.Session(); sess != nil {
		return sess.RefreshToken()
	}

	return ""
}

// Community returns the active [community.Requester] instance.
// Lazily initializes the community client if it has not been constructed.
func (c *Session) Community() community.Requester {
	c.mu.RLock()
	comm := c.community
	c.mu.RUnlock()

	if comm == nil {
		web := c.Web()
		c.mu.Lock()
		if c.community == nil {
			c.community = c.communityFactory(web.HTTP(), web, c.logger)
		}

		comm = c.community
		c.mu.Unlock()
	}

	return comm
}

// Web returns the active [WebSessionProvider] instance.
// Lazily initializes the web session if it has not been constructed.
func (c *Session) Web() WebSessionProvider {
	c.mu.RLock()
	web := c.web
	c.mu.RUnlock()

	if web == nil {
		steamID := c.SteamID()
		c.mu.Lock()
		if c.web == nil {
			c.web = c.webFactory(steamID, c.logger, c.http)
		}

		web = c.web
		c.mu.Unlock()
	}

	return web
}

// Socket returns the low-level service client communicating over socket transport.
func (c *Session) Socket() *service.Client {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.socketAPI
}

// Unified returns the service client communicating over unified HTTP WebAPI transport.
func (c *Session) Unified() *service.Client {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.unified
}

// IsAuthenticated reports whether the current web session is validated and active.
func (c *Session) IsAuthenticated() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.web != nil && c.web.IsAuthenticated()
}

// IsSocketConnected reports whether the current socket connection is active.
func (c *Session) IsSocketConnected() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.socket != nil && c.socket.IsConnected()
}

// SetLogonServer updates the target [socket.CMServer] address used for connections.
func (c *Session) SetLogonServer(s socket.CMServer) {
	c.mu.Lock()
	c.logonServer = s
	c.mu.Unlock()
}

// SetAPIKey configures the web and socket API clients with the given Steam WebAPI key.
func (c *Session) SetAPIKey(key string) {
	c.mu.Lock()
	c.unified = c.unified.WithAPIKey(key)
	c.socketAPI = c.socketAPI.WithAPIKey(key)
	c.mu.Unlock()
}

// SetAccessToken updates the active OAuth2 access token across socket and API subsystems.
// Returns [ErrSocketNotConnected] if no active network socket session exists.
func (c *Session) SetAccessToken(token string) error {
	sess := c.socket.Session()
	if sess == nil {
		return ErrSocketNotConnected
	}

	sess.SetAccessToken(token)

	c.mu.Lock()
	c.unified = c.unified.WithAccessToken(token)
	c.socketAPI = c.socketAPI.WithAccessToken(token)
	c.mu.Unlock()

	return nil
}

// LogOn performs a full authentication sequence and initializes the web session.
// Automatically fetches and configures the required Steam WebAPI key.
// Returns an error if login, token refresh, or API key discovery fails, or if context ctx is canceled.
func (c *Session) LogOn(ctx context.Context, server socket.CMServer, details *auth.LogOnDetails) error {
	if details == nil {
		return errors.New("session: cannot login with nil credentials")
	}

	c.EnrichLogger(details.AccountName, details.SteamID)

	c.mu.Lock()
	c.logonDetails = details
	c.logonServer = server
	c.mu.Unlock()

	if err := c.auth.LogOn(ctx, details, server); err != nil {
		return fmt.Errorf("session: login failed: %w", err)
	}

	c.EnrichLogger(details.AccountName, details.SteamID)

	if err := c.Refresh(ctx); err != nil {
		return fmt.Errorf("session: initial token refresh failed: %w", err)
	}

	if key, err := c.GetOrRegisterAPIKey(ctx, "g-man-bot.dev"); err != nil {
		c.Logger().Warn("Could not auto-fetch WebAPI Key", log.Err(err))
	} else {
		c.Logger().Info("WebAPI Key acquired automatically", log.String("key", key[:4]+"***"))
		c.SetAPIKey(key)
	}

	return nil
}

// GetOrRegisterAPIKey retrieves the Steam WebAPI key, registering one for the domain if none exists.
// Returns an error if the context ctx is canceled or the login attempt fails.
// If the community client is not available, returns [ErrNoCommunityClient].
func (c *Session) GetOrRegisterAPIKey(ctx context.Context, name string) (string, error) {
	comm := c.Community()
	if comm == nil {
		return "", ErrNoCommunityClient
	}

	apiKey, err := comm.GetOrRegisterAPIKey(ctx, name)
	if err != nil {
		return "", err
	}

	return apiKey, nil
}

// Reconnect performs a login sequence using cached session details.
// Returns [ErrMissingCredentials] if no logon details were stored.
// Returns an error if the context ctx is canceled or the login attempt fails.
func (c *Session) Reconnect(ctx context.Context) error {
	c.mu.RLock()
	details := c.logonDetails
	server := c.logonServer
	c.mu.RUnlock()

	if details == nil {
		return ErrMissingCredentials
	}

	c.Logger().Info("Attempting automatic reconnection...")

	c.mu.Lock()
	c.web = nil
	c.community = nil
	c.mu.Unlock()

	return c.LogOn(ctx, server, details)
}

// Verify checks the validity of the active web session.
// Returns an error if context ctx is canceled or the verification request fails.
func (c *Session) Verify(ctx context.Context) (bool, error) {
	return c.Web().Verify(ctx)
}

// Refresh performs a safe, deduplicated OAuth2 token refresh.
// Returns [module.ErrClosed] if the session manager has been closed.
// Returns [ErrMissingRefreshTokenOrSteamID] if required token parameters are absent.
func (c *Session) Refresh(ctx context.Context) error {
	if c.closed.Load() {
		return module.ErrClosed
	}

	_, err := c.refreshSF.Do("refresh", func() (struct{}, error) {
		return struct{}{}, c.doRefresh(ctx)
	})

	return err
}

func (c *Session) doRefresh(ctx context.Context) error {
	if isAlive, _ := c.Verify(ctx); isAlive {
		return nil
	}

	c.Logger().Info("Refreshing Steam session tokens...")

	refreshToken := c.RefreshToken()
	steamID := c.SteamID().Uint64()

	if refreshToken == "" || steamID == 0 {
		return fmt.Errorf("%w: refresh token: %q, steamID: %d", ErrMissingCredentials, refreshToken, steamID)
	}

	socketAuthSvc := auth.NewAuthenticationService(c.Socket(), c.device)

	resp, err := socketAuthSvc.GenerateAccessTokenForApp(ctx, refreshToken, steamID)
	if err != nil {
		return fmt.Errorf("failed to generate access token: %w", err)
	}

	if err := c.SetAccessToken(resp.GetAccessToken()); err != nil {
		return err
	}

	err = c.Web().Authenticate(ctx, c.device.PlatformType, refreshToken, resp.GetAccessToken())
	if err != nil {
		return fmt.Errorf("web auth failed during refresh: %w", err)
	}

	return nil
}

// StartRefreshLoop runs a periodic checker that validates and refreshes the web session.
// Blocks until the context ctx is canceled, then triggers a socket [Session.Disconnect].
// Subsequent calls are safe and will only start a single refresh loop.
func (c *Session) StartRefreshLoop(ctx context.Context) {
	c.refreshLoopOnce.Do(func() {
		ticker := time.NewTicker(c.refreshJobInterval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				goto shutdown
			case <-ticker.C:
				if web := c.Web(); web != nil && web.IsAuthenticated() {
					if isAlive, _ := web.Verify(ctx); !isAlive {
						if err := c.Refresh(ctx); err != nil {
							c.Logger().Warn("Periodic session refresh failed", log.Err(err))
						}
					}
				}
			}
		}

	shutdown:
		c.Logger().Debug("Session refresh loop stopped")
	})
}

// Disconnect gracefully terminates the active socket connection.
// Returns an error if socket shutdown fails.
func (c *Session) Disconnect() error {
	return c.socket.Disconnect()
}

// Close permanently shuts down the session manager and releases resources.
// Can be safely called multiple times; subsequent calls use cached state.
func (c *Session) Close() error {
	c.closed.Store(true)
	return c.socket.Close()
}

// Logger returns the thread-safe, configured [log.Logger] instance.
func (c *Session) Logger() log.Logger {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.logger
}

// EnrichLogger appends metadata fields to the thread-safe session logger context.
func (c *Session) EnrichLogger(account string, steamID id.ID) {
	c.mu.Lock()
	defer c.mu.Unlock()

	var logFields []any
	if account != "" && c.enrichedAccount == "" {
		logFields = append(logFields, log.String("account", account))
		c.enrichedAccount = account
	}

	if steamID != 0 && c.enrichedSteamID == 0 {
		logFields = append(logFields, log.Uint64("steam_id", steamID.Uint64()))
		c.enrichedSteamID = steamID
	}

	if len(logFields) > 0 {
		c.logger = c.logger.With(logFields...)
	}
}

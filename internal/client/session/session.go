// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package session orchestrates session credential lifecycles, OAuth2 tokens, and cookie synchronization across Steam transports.
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
	// ErrMissingCredentials indicates missing or uninitialized session tokens/credentials.
	ErrMissingCredentials = errors.New("session: missing required credentials")
	// ErrSocketNotConnected indicates that session token updates cannot be synchronized because the socket is offline.
	ErrSocketNotConnected = errors.New("session: cannot refresh session: socket is not connected")
	// ErrNoCommunityClient indicates that the community client requested for WebAPI operations is unavailable.
	ErrNoCommunityClient = errors.New("session: no community client available")
	// ErrNilCredentials indicates an attempt to log in using nil credentials.
	ErrNilCredentials = errors.New("session: cannot login with nil credentials")
)

// AuthenticatorProvider performs network logon sequences against Steam Connection Managers.
type AuthenticatorProvider interface {
	LogOn(ctx context.Context, details *auth.LogOnDetails, server socket.CMServer) error
}

// WebSessionProvider manages OIDC web sessions and cookie synchronization across Steam web domains.
type WebSessionProvider interface {
	HTTP() *http.Client
	SessionID(baseURL string) string
	Verify(ctx context.Context) (bool, error)
	Authenticate(ctx context.Context, platformType pb.EAuthTokenPlatformType, refreshToken, accessToken string) error
	IsAuthenticated() bool
}

// SocketProvider encapsulates socket operations required by Session.
type SocketProvider interface {
	auth.SocketProvider
	IsConnected() bool
	UpdateLogger(logger log.Logger)
	UpdateServers(servers []socket.CMServer)
	Send(ctx context.Context, build socket.PayloadBuilder, opts ...socket.SendOption) error
	SendSync(ctx context.Context, build socket.PayloadBuilder, opts ...socket.SendOption) (*protocol.Packet, error)
	RegisterServiceHandler(method string, handler socket.Handler)
	Disconnect() error
	Close() error
}

// WebSessionFactory constructs custom WebSessionProvider instances.
type WebSessionFactory func(steamID id.ID, logger log.Logger, r any) WebSessionProvider

// CommunityClientFactory constructs custom community requester instances.
type CommunityClientFactory func(httpDoer aoni.HTTPDoer, sess community.SessionProvider, logger log.Logger) community.Requester

// Config configures the Session manager behavior and fallback providers.
type Config struct {
	RefreshJobInterval time.Duration
	Device             *auth.DeviceConfig
	Storage            storage.Provider
	HTTP               any
	WebAPIBase         string
	Bus                *bus.Bus
	Logger             log.Logger
	Authenticator      AuthenticatorProvider
	WebFactory         WebSessionFactory
	CommunityFactory   CommunityClientFactory
}

// ResolveDefaults populates zero-value fields in Config with defaults.
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
		cfg.HTTP = aoni.NewClient(nil)
	}

	if cfg.Device == nil {
		d := auth.DefaultDeviceConfig()
		cfg.Device = &d
	}

	if cfg.WebFactory == nil {
		cfg.WebFactory = func(steamID id.ID, logger log.Logger, r any) WebSessionProvider {
			return websession.New(steamID, logger, r)
		}
	}

	if cfg.CommunityFactory == nil {
		cfg.CommunityFactory = func(httpDoer aoni.HTTPDoer, sess community.SessionProvider, logger log.Logger) community.Requester {
			return community.NewClient(aoni.NewHTTPDoerAdapter(httpDoer), sess).WithLogger(logger)
		}
	}
}

// Session manages user authentication state, OAuth tokens, and session renewal loops.
//
// Thread Safety:
//   - All methods are safe for concurrent access.
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
	http      any

	webFactory       WebSessionFactory
	communityFactory CommunityClientFactory

	unified   *service.Client
	socketAPI *service.Client

	refreshLoopOnce    sync.Once
	refreshJobInterval time.Duration
	refreshSF          *generic.SingleFlight[struct{}]

	closed atomic.Bool

	logonDetails *auth.LogOnDetails
	logonServer  socket.CMServer

	enrichedAccount string
	enrichedSteamID id.ID
}

// New constructs an initialized Session orchestrator.
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

// Storage returns the configured persistent storage provider.
func (c *Session) Storage() storage.Provider { return c.storage }

// SteamID returns the active user's 64-bit Steam ID, or 0 if unauthenticated.
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

// AccessToken returns the current OAuth2 access token, or an empty string if absent.
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

// RefreshToken returns the current OAuth2 refresh token, or an empty string if absent.
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

// Community returns the active community requester, lazily instantiating it if uninitialized.
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

// Web returns the active WebSessionProvider, lazily instantiating it if uninitialized.
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

// Socket returns the service client configured for low-level socket transport.
func (c *Session) Socket() *service.Client {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.socketAPI
}

// Unified returns the service client configured for WebAPI HTTP transport.
func (c *Session) Unified() *service.Client {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.unified
}

// IsAuthenticated reports whether the current web session contains valid cookies.
func (c *Session) IsAuthenticated() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.web != nil && c.web.IsAuthenticated()
}

// IsSocketConnected reports whether the socket connection is currently active.
func (c *Session) IsSocketConnected() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.socket != nil && c.socket.IsConnected()
}

// SetLogonServer updates the target Connection Manager server address.
func (c *Session) SetLogonServer(s socket.CMServer) {
	c.mu.Lock()
	c.logonServer = s
	c.mu.Unlock()
}

// SetAPIKey configures WebAPI service clients with the specified Steam WebAPI key.
func (c *Session) SetAPIKey(key string) {
	c.mu.Lock()
	c.unified = c.unified.WithAPIKey(key)
	c.socketAPI = c.socketAPI.WithAPIKey(key)
	c.mu.Unlock()
}

// SetAccessToken updates the OAuth2 access token across active socket and API clients.
//
// Returns:
//   - ErrSocketNotConnected if no active socket session exists.
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

// LogOn executes authentication, performs an initial token refresh, and auto-fetches WebAPI keys.
//
// Returns:
//   - ErrNilCredentials if details is nil.
//   - Error if authentication, initial refresh, or key retrieval fails.
func (c *Session) LogOn(ctx context.Context, server socket.CMServer, details *auth.LogOnDetails) error {
	if details == nil {
		return ErrNilCredentials
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

// GetOrRegisterAPIKey retrieves or registers a Steam WebAPI key for the given domain.
func (c *Session) GetOrRegisterAPIKey(ctx context.Context, name string) (string, error) {
	comm := c.Community()
	if comm == nil {
		return "", ErrNoCommunityClient
	}

	return comm.GetOrRegisterAPIKey(ctx, name)
}

// Reconnect resets web states and re-authenticates using cached logon details.
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

// Verify checks if the current web session cookies are valid.
func (c *Session) Verify(ctx context.Context) (bool, error) {
	return c.Web().Verify(ctx)
}

// Refresh performs single-flight deduplicated OAuth2 token generation and web session re-authentication.
//
// Returns:
//   - module.ErrClosed if the session is closed.
//   - ErrMissingCredentials if refresh token or SteamID is missing.
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

	if token := resp.GetAccessToken(); token != "" {
		if err := c.SetAccessToken(token); err != nil {
			return err
		}
	}

	err = c.Web().Authenticate(ctx, c.device.PlatformType, refreshToken, resp.GetAccessToken())
	if err != nil {
		return fmt.Errorf("web auth failed during refresh: %w", err)
	}

	return nil
}

// StartRefreshLoop starts a periodic background worker verifying web session health and performing automated renewals.
func (c *Session) StartRefreshLoop(ctx context.Context) {
	c.refreshLoopOnce.Do(func() {
		ticker := time.NewTicker(c.refreshJobInterval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				c.Logger().Debug("Session refresh loop stopped")
				return
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
	})
}

// Disconnect gracefully disconnects the network socket connection.
func (c *Session) Disconnect() error {
	return c.socket.Disconnect()
}

// Close permanently shuts down the session manager and socket connection.
func (c *Session) Close() error {
	c.closed.Store(true)
	return c.socket.Close()
}

// Logger returns the configured logger.
func (c *Session) Logger() log.Logger {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.logger
}

// EnrichLogger appends account name and SteamID attributes to the logger context.
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

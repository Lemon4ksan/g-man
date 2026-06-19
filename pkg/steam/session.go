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

	"github.com/lemon4ksan/aoni"
	"github.com/lemon4ksan/miyako/bus"
	"github.com/lemon4ksan/miyako/generic"

	"github.com/lemon4ksan/g-man/pkg/log"
	pb "github.com/lemon4ksan/g-man/pkg/protobuf/steam"
	"github.com/lemon4ksan/g-man/pkg/steam/auth"
	"github.com/lemon4ksan/g-man/pkg/steam/auth/websession"
	"github.com/lemon4ksan/g-man/pkg/steam/community"
	"github.com/lemon4ksan/g-man/pkg/steam/id"
	"github.com/lemon4ksan/g-man/pkg/steam/module"
	"github.com/lemon4ksan/g-man/pkg/steam/service"
	"github.com/lemon4ksan/g-man/pkg/steam/socket"
	tr "github.com/lemon4ksan/g-man/pkg/steam/transport"
	"github.com/lemon4ksan/g-man/pkg/storage"
	"github.com/lemon4ksan/g-man/pkg/storage/memory"
)

type authenticator interface {
	LogOn(ctx context.Context, details *auth.LogOnDetails, server socket.CMServer) error
}

type webSession interface {
	HTTP() *http.Client
	SessionID(baseURL string) string
	Verify(ctx context.Context) (bool, error)
	Authenticate(ctx context.Context, platformType pb.EAuthTokenPlatformType, refreshToken, accessToken string) error
	IsAuthenticated() bool
}

type communityClient interface {
	community.Requester
	GetOrRegisterAPIKey(ctx context.Context, domain string) (string, error)
}

// WebSessionFactory constructs a webSession instance.
type WebSessionFactory func(steamID id.ID, logger log.Logger, baseDoer aoni.HTTPDoer) webSession

// CommunityClientFactory constructs a communityClient instance.
type CommunityClientFactory func(httpClient *http.Client, sessionID func(string) string, logger log.Logger) communityClient

// SessionManager manages the session state of the client.
//
// It handles OAuth2 token refreshing, session validation, and OIDC synchronization
// for web domains. Use [NewSessionManager] to create new instances of the manager.
type SessionManager struct {
	mu sync.RWMutex

	auth      authenticator
	web       webSession
	community communityClient
	socket    SocketProvider
	logger    log.Logger
	storage   storage.Provider
	device    *auth.DeviceConfig
	bus       *bus.Bus
	http      aoni.HTTPDoer // Global HTTP client

	webFactory       WebSessionFactory
	communityFactory CommunityClientFactory

	unified   *service.Client // WebAPI (HTTP)
	socketAPI *service.Client // CM (TCP/WS)

	verifyTicker *time.Ticker
	closed       atomic.Bool

	refreshSF *generic.SingleFlight[struct{}]

	enrichedAccount string
	enrichedSteamID id.ID
}

// NewSessionManager creates a new session manager.
func NewSessionManager(cfg Config, bus *bus.Bus, logger log.Logger, sock SocketProvider) *SessionManager {
	device := cfg.Device
	if device == nil {
		d := auth.DefaultDeviceConfig()
		device = &d
	}

	c := &SessionManager{
		bus:          bus,
		logger:       logger,
		socket:       sock,
		storage:      cfg.Storage,
		device:       device,
		verifyTicker: time.NewTicker(5 * time.Minute),
		http:         cfg.HTTP,
		refreshSF:    generic.NewSingleFlight[struct{}](),
	}

	if c.storage == nil {
		c.storage = memory.New()
	}

	c.webFactory = cfg.WebFactory
	if c.webFactory == nil {
		c.webFactory = func(steamID id.ID, logger log.Logger, baseDoer aoni.HTTPDoer) webSession {
			return websession.New(steamID, logger, baseDoer)
		}
	}

	c.communityFactory = cfg.CommunityFactory
	if c.communityFactory == nil {
		c.communityFactory = func(httpClient *http.Client, sessionID func(string) string, logger log.Logger) communityClient {
			return community.NewClient(
				httpClient,
				sessionID,
				community.WithLogger(logger),
			)
		}
	}

	webTransport := tr.NewHTTPTransport(cfg.HTTP, service.WebAPIBase)
	c.unified = service.New(webTransport)

	c.auth = auth.NewAuthenticator(
		sock,
		auth.NewAuthenticationService(c.unified, device),
		bus,
		auth.WithLogger(c.logger),
		auth.WithStorage(auth.NewKVStore(c.storage.KV("auth"))),
	)

	socketTransport := tr.NewSocketTransport(sock)
	c.socketAPI = service.New(socketTransport)

	return c
}

// Storage returns the session storage provider.
func (c *SessionManager) Storage() storage.Provider { return c.storage }

// Clients returns the underlying unified and socket clients.
func (c *SessionManager) Clients() (unified, socketAPI *service.Client) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.unified, c.socketAPI
}

// LogOn connects to the CM and performs the login sequence.
func (c *SessionManager) LogOn(
	ctx context.Context,
	server socket.CMServer,
	details *auth.LogOnDetails,
) error {
	c.enrichLogger(details.AccountName, details.SteamID)

	if err := c.auth.LogOn(ctx, details, server); err != nil {
		return fmt.Errorf("login: %w", err)
	}

	c.enrichLogger(details.AccountName, details.SteamID)

	c.mu.Lock()
	if c.web == nil {
		c.web = c.webFactory(details.SteamID, c.logger, c.http)
	}

	c.mu.Unlock()

	if err := c.Refresh(ctx); err != nil {
		return fmt.Errorf("initial token refresh failed: %w", err)
	}

	c.mu.Lock()
	if c.community == nil {
		c.community = c.communityFactory(
			c.web.HTTP(),
			c.web.SessionID,
			c.logger,
		)
	}

	c.mu.Unlock()

	apiKey, err := c.community.GetOrRegisterAPIKey(ctx, "g-man-bot.dev")
	if err != nil {
		c.getLogger().Warn("Could not auto-fetch API Key", log.Err(err))
		return err
	}

	c.getLogger().Info("WebAPI Key acquired automatically", log.String("key", apiKey[:4]+"***"))

	c.mu.Lock()
	c.unified = c.unified.WithAPIKey(apiKey)
	c.socketAPI = c.socketAPI.WithAPIKey(apiKey)
	c.mu.Unlock()

	return nil
}

// IsAuthenticated reports whether the current session is authenticated.
func (c *SessionManager) IsAuthenticated() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.web != nil && c.web.IsAuthenticated()
}

// Verify checks if the current web session is still valid.
func (c *SessionManager) Verify(ctx context.Context) (bool, error) {
	c.mu.RLock()
	web := c.web
	c.mu.RUnlock()

	if web == nil {
		return false, nil
	}

	return web.Verify(ctx)
}

// Refresh is the central method for refreshing all tokens (Access and Web tokens).
// It is protected by single-flight double-checked locking to avoid Thundering Herd.
func (c *SessionManager) Refresh(ctx context.Context) error {
	if c.closed.Load() {
		return module.ErrClosed
	}

	_, err := c.refreshSF.Do("refresh", func() (struct{}, error) {
		return struct{}{}, c.doRefresh(ctx)
	})

	return err
}

func (c *SessionManager) doRefresh(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Check if session is actually dead before doing heavy work
	if c.web != nil {
		if isAlive, _ := c.web.Verify(ctx); isAlive {
			return nil
		}
	}

	c.logger.Info("Refreshing Steam session tokens...")

	sess := c.socket.Session()
	if sess == nil {
		return errors.New("cannot refresh session: socket is not connected")
	}

	socketAuthSvc := auth.NewAuthenticationService(c.socketAPI, c.device)

	resp, err := socketAuthSvc.GenerateAccessTokenForApp(ctx, sess.RefreshToken(), sess.SteamID())
	if err != nil {
		return fmt.Errorf("failed to generate access token: %w", err)
	}

	newAccessToken := resp.GetAccessToken()
	sess.SetAccessToken(newAccessToken)

	c.unified = c.unified.WithAccessToken(newAccessToken)
	c.socketAPI = c.socketAPI.WithAccessToken(newAccessToken)

	err = c.web.Authenticate(ctx, c.device.PlatformType, sess.RefreshToken(), sess.AccessToken())
	if err != nil {
		return fmt.Errorf("web auth failed during refresh: %w", err)
	}

	return nil
}

// StartRefreshLoop starts the refresh loop.
func (c *SessionManager) StartRefreshLoop(ctx context.Context) error {
	defer c.verifyTicker.Stop()

	for {
		select {
		case <-ctx.Done():
			goto shutdown
		case <-c.verifyTicker.C:
			if c.web != nil && c.web.IsAuthenticated() {
				go func() {
					isAlive, _ := c.web.Verify(ctx)
					if !isAlive && ctx.Err() == nil {
						if err := c.Refresh(ctx); err != nil {
							c.getLogger().Warn("Periodic session refresh failed", log.Err(err))
						}
					}
				}()
			}
		}
	}

shutdown:
	c.getLogger().Debug("Orchestrator shutting down...")

	return c.Disconnect()
}

// Disconnect disconnects the socket.
func (c *SessionManager) Disconnect() error {
	return c.socket.Disconnect()
}

// Close closes the session manager.
func (c *SessionManager) Close() error {
	c.closed.Store(true)
	return c.socket.Close()
}

func (c *SessionManager) getLogger() log.Logger {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.logger
}

func (c *SessionManager) enrichLogger(account string, steamID id.ID) {
	c.mu.Lock()
	defer c.mu.Unlock()

	var logFields []log.Field
	if account != "" && c.enrichedAccount == "" {
		logFields = append(logFields, log.String("account", account))
		c.enrichedAccount = account
	}

	if steamID != 0 && c.enrichedSteamID == 0 {
		logFields = append(logFields, log.SteamID(steamID.Uint64()))
		c.enrichedSteamID = steamID
	}

	if len(logFields) > 0 {
		c.logger = c.logger.With(logFields...)
	}
}

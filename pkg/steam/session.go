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
	pb "github.com/lemon4ksan/g-man/pkg/protobuf/steam"
	"github.com/lemon4ksan/g-man/pkg/steam/auth"
	"github.com/lemon4ksan/g-man/pkg/steam/auth/websession"
	"github.com/lemon4ksan/g-man/pkg/steam/community"
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

	unified   *service.Client // WebAPI (HTTP)
	socketAPI *service.Client // CM (TCP/WS)

	verifyTicker *time.Ticker
	closed       atomic.Bool
}

func NewSessionManager(cfg Config, bus *bus.Bus, logger log.Logger, sock SocketProvider) *SessionManager {
	c := &SessionManager{
		bus:          bus,
		logger:       logger,
		socket:       sock,
		storage:      cfg.Storage,
		device:       cfg.Device,
		verifyTicker: time.NewTicker(5 * time.Minute),
	}

	if c.storage == nil {
		c.storage = memory.New()
	}

	webTransport := tr.NewHTTPTransport(cfg.HTTP, service.WebAPIBase)
	c.unified = service.New(webTransport)

	c.auth = auth.NewAuthenticator(
		sock,
		auth.NewAuthenticationService(c.unified, cfg.Device),
		bus,
		auth.WithLogger(c.logger),
		auth.WithStorage(c.storage.Auth()),
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
	if err := c.auth.LogOn(ctx, details, server); err != nil {
		return fmt.Errorf("login: %w", err)
	}

	c.mu.Lock()
	if c.web == nil {
		c.web = websession.New(details.SteamID, c.logger)
	}

	c.mu.Unlock()

	if err := c.Refresh(ctx); err != nil {
		return fmt.Errorf("initial token refresh failed: %w", err)
	}

	c.mu.Lock()
	if c.community == nil {
		comm := community.NewClient(c.web.HTTP(), c.web.SessionID, community.WithLogger(c.logger))
		c.community = comm
	}

	c.mu.Unlock()

	apiKey, err := c.community.GetOrRegisterAPIKey(ctx, "g-man-bot.dev")
	if err != nil {
		c.logger.Warn("Could not auto-fetch API Key", log.Err(err))
		return err
	}

	c.logger.Info("WebAPI Key acquired automatically", log.String("key", apiKey[:4]+"***"))

	c.mu.Lock()
	c.unified = c.unified.WithAPIKey(apiKey)
	c.socketAPI = c.socketAPI.WithAPIKey(apiKey)
	c.mu.Unlock()

	return nil
}

// Refresh is the central method for refreshing all tokens (Access and Web tokens).
func (c *SessionManager) Refresh(ctx context.Context) error {
	if c.closed.Load() {
		return module.ErrClosed
	}

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
							c.logger.Warn("Periodic session refresh failed", log.Err(err))
						}
					}
				}()
			}
		}
	}

shutdown:
	c.logger.Debug("Orchestrator shutting down...")

	return c.Disconnect()
}

func (c *SessionManager) Disconnect() error {
	return c.socket.Disconnect()
}

func (c *SessionManager) Close() error {
	c.closed.Store(true)
	return c.socket.Close()
}

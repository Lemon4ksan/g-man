// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package steam

import (
	"context"

	"github.com/lemon4ksan/miyako/generic"
	"github.com/lemon4ksan/miyako/log"

	"github.com/lemon4ksan/g-man/pkg/steam/auth"
	"github.com/lemon4ksan/g-man/pkg/steam/client"
	"github.com/lemon4ksan/g-man/pkg/steam/sys/directory"
)

var (
	// ErrNotRunning is returned when the client is not running.
	ErrNotRunning = client.ErrNotRunning
	// ErrSocketDisabled is returned when attempting socket operations while the transport layer is disabled.
	ErrSocketDisabled = client.ErrSocketDisabled
)

// Client acts as the central hub connecting socket, authentication, and modules.
// Create new instances of Client using [NewClient] or [NewReadyClient].
type Client = client.Client

// Config aggregates configurations for all core subsystems of [Client].
// Use [DefaultConfig] to initialize a configuration with standard settings.
type Config = client.Config

// DefaultConfig returns the baseline [Config] with standard defaults.
var DefaultConfig = client.DefaultConfig

// Option defines a functional configuration option for [Client].
type Option = client.Option

var (
	// WithLogger sets a custom [log.Logger] for [Client].
	WithLogger = client.WithLogger
	// WithModule adds a [module.Module] to [Client] and initializes it immediately.
	WithModule = client.WithModule
	// WithSocket sets a custom [SocketProvider] for [Client].
	WithSocket = client.WithSocket
	// WithREST sets a custom [aoni.Client] for [Client].
	WithREST = client.WithREST
	// WithBus sets a custom [bus.Bus] for [Client].
	WithBus = client.WithBus
	// WithStorage sets a custom [storage.Provider] for [Client].
	WithStorage = client.WithStorage
	// WithSession sets a custom [session.Session] for [Client].
	WithSession = client.WithSession
	// WithAuthenticator sets a custom [AuthenticatorProvider] for [Client].
	WithAuthenticator = client.WithAuthenticator
	// WithWebFactory sets a custom [WebSessionFactory] for [Client].
	WithWebFactory = client.WithWebFactory
	// WithCommunityFactory sets a custom [CommunityClientFactory] for [Client].
	WithCommunityFactory = client.WithCommunityFactory
)

// NewClient initializes and returns a new [Client] with the given [Config] and [Option] list.
// Returns an error if option application fails or configuration is invalid.
var NewClient = client.New

// NewReadyClient creates a [Client], configures a default logger if none is provided, connects to the optimal server, and performs logon.
// It returns an error if CM server discovery fails, connection fails, or login is rejected.
// It returns an error if the context ctx is canceled or details is nil.
func NewReadyClient(ctx context.Context, cfg Config, details *auth.LogOnDetails, opts ...Option) (*Client, error) {
	logger := log.New(log.DefaultConfig(log.LevelInfo))
	opts = append([]Option{WithLogger(logger)}, opts...)

	c, err := client.New(cfg, opts...)
	if err != nil {
		return nil, err
	}

	if err := c.Run(); err != nil {
		return nil, err
	}

	srv, err := directory.New(c).GetOptimalCMServer(ctx)
	if err != nil {
		_ = c.Close()
		return nil, err
	}

	if err = c.ConnectAndLogin(ctx, srv, details); err != nil {
		_ = c.Close()
		return nil, err
	}

	return c, nil
}

// GetModule returns the first registered module matching type T from the [Client].
// Returns the zero value of T if no matching module is registered.
// Returns the zero value of T if c is nil.
func GetModule[T any](c *Client) T {
	if c == nil {
		return generic.Zero[T]()
	}

	for _, m := range c.Modules() {
		if typed, ok := m.(T); ok {
			return typed
		}
	}

	return generic.Zero[T]()
}

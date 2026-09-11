// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package steam provides a high-level facade for interacting with the Steam Network.
// It exposes the core Client, configuration, and options from the underlying
// implementations while maintaining a clean, unified API surface.
package steam

import (
	"context"

	log "github.com/lemon4ksan/foundation/async/logkit"
	"github.com/lemon4ksan/foundation/generic"

	"github.com/lemon4ksan/g-man/pkg/steam/auth"
	"github.com/lemon4ksan/g-man/pkg/steam/client"
	"github.com/lemon4ksan/g-man/pkg/steam/sys/directory"
)

var (
	// ErrNotRunning is returned when attempting to use a client that hasn't been started.
	ErrNotRunning = client.ErrNotRunning
	// ErrSocketDisabled is returned when attempting to use socket features while the socket is disabled.
	ErrSocketDisabled = client.ErrSocketDisabled
)

// Client is the primary orchestrator for Steam Network interactions.
// It manages the connection lifecycle, dispatching, and module execution.
type Client = client.Client

// Config holds the immutable settings for the Steam Client.
type Config = client.Config

// DefaultConfig returns a secure, production-ready default configuration.
var DefaultConfig = client.DefaultConfig

// Option applies functional configuration to the Client during instantiation.
type Option = client.Option

var (
	WithLogger           = client.WithLogger
	WithModule           = client.WithModule
	WithSocket           = client.WithSocket
	WithREST             = client.WithREST
	WithFastClient       = client.WithFastClient
	WithBus              = client.WithBus
	WithStorage          = client.WithStorage
	WithSession          = client.WithSession
	WithAuthenticator    = client.WithAuthenticator
	WithWebFactory       = client.WithWebFactory
	WithCommunityFactory = client.WithCommunityFactory
	WithProxy            = client.WithProxy
)

// NewClient instantiates a new Client with the provided configuration and options.
var NewClient = client.New

// NewReadyClient constructs a new Client, retrieves an optimal Connection Manager (CM) server
// from the Steam Directory, and performs a full login sequence.
//
// This is a convenience method for establishing a ready-to-use authenticated session.
func NewReadyClient(ctx context.Context, cfg Config, details *auth.LogOnDetails, opts ...Option) (*Client, error) {
	c, err := client.New(cfg, ensureLoggerOption(opts)...)
	if err != nil {
		return nil, err
	}

	if err := startAndLogin(ctx, c, details); err != nil {
		_ = c.Close()
		return nil, err
	}

	return c, nil
}

// ensureLoggerOption appends a default logger to the options if one isn't explicitly provided.
// It ensures that the client always has a valid logging sink.
func ensureLoggerOption(opts []Option) []Option {
	logger := log.New(log.DefaultConfig(log.LevelInfo))
	return append([]Option{WithLogger(logger)}, opts...)
}

// startAndLogin manages the sequence of starting the client, resolving a CM server, and authenticating.
func startAndLogin(ctx context.Context, c *Client, details *auth.LogOnDetails) error {
	if err := c.Run(); err != nil {
		return err
	}

	srv, err := directory.New(c).GetOptimalCMServer(ctx)
	if err != nil {
		return err
	}

	return c.ConnectAndLogin(ctx, srv, details)
}

// GetModule searches the Client's registered modules and returns the first instance matching type T.
// Returns the zero value of T if no matching module is found.
// This generic accessor provides type-safe retrieval of subsystems (e.g., TF2, Trading).
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

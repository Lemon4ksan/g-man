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
	ErrNotRunning     = client.ErrNotRunning
	ErrSocketDisabled = client.ErrSocketDisabled
)

type Client = client.Client

type Config = client.Config

var DefaultConfig = client.DefaultConfig

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
)

var NewClient = client.New

// NewReadyClient constructs a Client, connects to an optimal Connection Manager server, and logs in.
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

// GetModule returns the first registered module matching type T.
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

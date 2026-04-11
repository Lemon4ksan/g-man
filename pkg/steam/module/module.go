// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package module provides extensible plugins system for 'steam.Client'.
package module

import (
	"context"
	"errors"

	"sync"
	"sync/atomic"

	"github.com/lemon4ksan/g-man/pkg/bus"
	"github.com/lemon4ksan/g-man/pkg/log"
	"github.com/lemon4ksan/g-man/pkg/rest"
	"github.com/lemon4ksan/g-man/pkg/steam/community"
	"github.com/lemon4ksan/g-man/pkg/steam/id"
	"github.com/lemon4ksan/g-man/pkg/steam/protocol/enums"
	"github.com/lemon4ksan/g-man/pkg/steam/service"
	"github.com/lemon4ksan/g-man/pkg/steam/socket"
	"github.com/lemon4ksan/g-man/pkg/storage"
)

var (
	ErrClientClosed     = errors.New("steam: client is closed")
	ErrNotAuthenticated = errors.New("steam: not authenticated")
)

// InitContext provides the module with access to the necessary client resources
// during the initialization phase, without exposing lifecycle management methods (Close, Connect).
type InitContext interface {
	// Storage returns the configured storage provider.
	Storage() storage.Provider

	// Bus provides access to the event bus for subscribing/publishing internal messages.
	Bus() *bus.Bus

	// Logger returns the configured logger.
	Logger() log.Logger

	// Service returns a client for working with the official Steam APIs (Unified, WebAPI, Legacy).
	// This client is compatible with the functions [service.Unified], [service.WebAPI], etc.
	Service() service.Doer

	// Rest returns a client for making http rest api calls.
	Rest() rest.Requester

	// RegisterPacketHandler registers a handler for low-level EMsg (TCP/UDP).
	RegisterPacketHandler(eMsg enums.EMsg, handler socket.Handler)

	// RegisterServiceHandler registers a handler for Protobuf services (Unified Services).
	RegisterServiceHandler(method string, handler socket.Handler)

	// GetModule allows you to find another module if there are dependencies between them.
	Module(name string) Module

	// UnregisterPacketHandler removes the handler from socket for freeing memory.
	UnregisterPacketHandler(eMsg enums.EMsg)

	// UnregisterServiceHandler removes the service handler from socket for freeing memory.
	UnregisterServiceHandler(method string)
}

type AuthContext interface {
	// Community returns an authorized community client for working with community endpoint.
	// This client is compatible with [community.Get], [community.PostForm], etc.
	Community() community.Requester

	// SteamID returns the steam id of the authorized user.
	SteamID() id.ID
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

// Auth defines the contract for pluggable extensions that require authorized clients.
type Auth interface {
	Module

	// StartAuthed is called after a successful Steam login and WebSession creation.
	StartAuthed(ctx context.Context, auth AuthContext) error
}

// Base provides a standard implementation of the module lifecycle.
// Other modules should embed it:
//
//	type YourModule struct {
//		module.Base
//		... specific fields
//	}
type Base struct {
	NameStr string

	Logger log.Logger
	Bus    *bus.Bus

	State atomic.Int32

	Ctx    context.Context
	Cancel context.CancelFunc
	Wg     sync.WaitGroup
}

func New(name string) Base {
	return Base{
		NameStr: name,
		Logger:  log.Discard,
	}
}

func (b *Base) Name() string { return b.NameStr }

func (b *Base) Start(ctx context.Context) error {
	b.Ctx, b.Cancel = context.WithCancel(ctx)
	return nil
}

func (b *Base) Init(ctx InitContext) error {
	b.Logger = ctx.Logger().With(log.Module(b.NameStr))
	b.Bus = ctx.Bus()

	if b.Ctx == nil || b.Ctx.Err() != nil {
		// For tests
		b.Ctx, b.Cancel = context.WithCancel(context.Background())
	}

	b.State.Store(0)
	return nil
}

func (b *Base) Close() error {
	if b.Cancel != nil {
		b.Cancel()
	}

	b.Wg.Wait()
	return nil
}

func (b *Base) Go(fn func(ctx context.Context)) {
	b.Wg.Go(func() {
		fn(b.Ctx)
	})
}

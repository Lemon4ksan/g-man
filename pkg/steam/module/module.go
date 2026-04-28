// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

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
	// ErrClientClosed is returned when an operation is attempted on a shut-down client.
	ErrClientClosed = errors.New("steam: client is closed")

	// ErrNotAuthenticated is returned when a module requires an active session but the client is not logged in.
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

	// Module allows you to find another module if there are dependencies between them.
	Module(name string) Module

	// UnregisterPacketHandler removes the handler from socket for freeing memory.
	UnregisterPacketHandler(eMsg enums.EMsg)

	// UnregisterServiceHandler removes the service handler from socket for freeing memory.
	UnregisterServiceHandler(method string)
}

// AuthContext provides resources that become available only after a successful
// Steam authentication and web session establishment.
type AuthContext interface {
	// Community returns an authorized community client for working with community endpoint.
	// This client is compatible with [community.Get], [community.PostForm], etc.
	Community() community.Requester

	// SteamID returns the steam id of the authorized user.
	SteamID() id.ID
}

// Module defines the contract for pluggable extensions (e.g., Trade, Chat, GC).
// All modules must implement this interface to be loaded by the Steam client.
type Module interface {
	// Name returns a unique identifier for the module.
	Name() string

	// Init is called during client creation. Use this to register packet handlers
	// and subscribe to bus events.
	Init(init InitContext) error

	// Start is called when the client starts running. Use this to launch
	// background tasks (tickers, pollers). The context is cancelled when the client closes.
	Start(ctx context.Context) error
}

// Auth defines the contract for pluggable extensions that require authorized clients
// and depend on a valid user session (cookies, steamID).
type Auth interface {
	Module

	// StartAuthed is called after a successful Steam login and WebSession creation.
	// It is triggered every time the client re-authenticates.
	StartAuthed(ctx context.Context, auth AuthContext) error
}

// Base provides a standard implementation of the module lifecycle.
// It handles boilerplate like logging setup, event bus storage, and background
// task synchronization. Other modules should embed it:
//
//	type YourModule struct {
//		module.Base
//		... specific fields
//	}
type Base struct {
	// NameStr is the unique name of the module used for logging.
	NameStr string

	// Logger is a scoped logger for the module (pre-filled with module name).
	Logger log.Logger
	// Bus is the shared event bus for the client.
	Bus *bus.Bus

	// State is an atomic status indicator for the module.
	State atomic.Int32

	// Ctx is the module's internal context, cancelled when the module stops.
	Ctx context.Context
	// Cancel stops all background tasks associated with this module.
	Cancel context.CancelFunc
	// Wg tracks background goroutines to ensure graceful shutdown.
	Wg sync.WaitGroup
}

// New creates a new Base module with the given name.
func New(name string) Base {
	return Base{
		NameStr: name,
		Logger:  log.Discard,
	}
}

// Name returns the module identifier.
func (b *Base) Name() string { return b.NameStr }

// Start initializes the module's lifecycle context.
func (b *Base) Start(ctx context.Context) error {
	b.Ctx, b.Cancel = context.WithCancel(ctx)
	return nil
}

// Init sets up common dependencies like Logger and Bus.
func (b *Base) Init(ctx InitContext) error {
	b.Logger = ctx.Logger().With(log.Module(b.NameStr))
	b.Bus = ctx.Bus()

	if b.Ctx == nil || b.Ctx.Err() != nil {
		// For tests where Start might not be called explicitly
		b.Ctx, b.Cancel = context.WithCancel(context.Background())
	}

	b.State.Store(0)

	return nil
}

// Close gracefully shuts down the module by cancelling its context and waiting
// for all spawned goroutines (via Go() method) to finish.
func (b *Base) Close() error {
	if b.Cancel != nil {
		b.Cancel()
	}

	b.Wg.Wait()

	return nil
}

// Go spawns a background goroutine that is tracked by the module's WaitGroup.
// The provided function should respect the module's context (b.Ctx) for cancellation.
func (b *Base) Go(fn func(ctx context.Context)) {
	b.Wg.Go(func() {
		fn(b.Ctx)
	})
}

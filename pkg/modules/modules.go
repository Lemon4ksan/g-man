// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package models provides structured used across
package modules

import (
	"context"
	"errors"

	"sync"
	"sync/atomic"

	"github.com/lemon4ksan/g-man/pkg/log"
	"github.com/lemon4ksan/g-man/pkg/steam/bus"
	"github.com/lemon4ksan/g-man/pkg/steam/community"
	"github.com/lemon4ksan/g-man/pkg/steam/protocol"
	"github.com/lemon4ksan/g-man/pkg/steam/service"
	"github.com/lemon4ksan/g-man/pkg/steam/socket"
)

var (
	ErrClientClosed = errors.New("steam: client is closed")
	ErrNotAuthenticated = errors.New("steam: not authenticated")
)

// InitContext provides the module with access to the necessary client resources
// during the initialization phase, without exposing lifecycle management methods (Close, Connect).
type InitContext interface {
	// Bus provides access to the event bus for subscribing/publishing internal messages.
	Bus() *bus.Bus

	// Logger returns the configured logger.
	Logger() log.Logger

	// Service returns a client for working with the official Steam APIs (Unified, WebAPI, Legacy).
	// This client is compatible with the functions [service.Unified], [service.WebAPI], etc.
	Service() service.Requester

	// RegisterPacketHandler registers a handler for low-level EMsg (TCP/UDP).
	RegisterPacketHandler(eMsg protocol.EMsg, handler socket.Handler)

	// RegisterServiceHandler registers a handler for Protobuf services (Unified Services).
	RegisterServiceHandler(method string, handler socket.Handler)

	// GetModule allows you to find another module if there are dependencies between them.
	GetModule(name string) Module

	// UnregisterPacketHandler removes the handler from socket for freeing memory.
	UnregisterPacketHandler(eMsg protocol.EMsg)

	// UnregisterServiceHandler removes the service handler from socket for freeing memory.
	UnregisterServiceHandler(method string)
}

type AuthContext interface {
	// Community returns an authorized community client for working with community endpoint.
	// This client is compatible with [community.Get], [community.PostForm], etc.
	Community() community.Requester

	// SteamID returns the steam id of the authorized user.
	SteamID() uint64
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

// ModuleAuth defines the contract for pluggable extensions that require authorized clients.
type ModuleAuth interface {
	Module

	// StartAuthed is called after a successful Steam login and WebSession creation.
	StartAuthed(ctx context.Context, auth AuthContext) error
}

// BaseModule provides a standard implementation of the module lifecycle.
// Other modules should embed it:
//
//	type YourModule struct {
//		modules.BaseModule
//		... specific fields
//	}
type BaseModule struct {
	NameStr string

	Logger log.Logger
	Bus    *bus.Bus

	State atomic.Int32

	Ctx    context.Context
	Cancel context.CancelFunc
	Wg     sync.WaitGroup
}

func NewBase(name string) BaseModule {
	return BaseModule{
		NameStr: name,
	}
}

func (b *BaseModule) Name() string { return b.NameStr }

func (b *BaseModule) Init(ctx InitContext) error {
    b.Logger = ctx.Logger().WithModule(b.NameStr)
    b.Bus = ctx.Bus()

    if b.Ctx == nil || b.Ctx.Err() != nil {
        b.Ctx, b.Cancel = context.WithCancel(context.Background())
    }
    
    b.State.Store(0)
    return nil
}

func (b *BaseModule) Close() error {
    if b.Cancel != nil {
        b.Cancel()
    }
    
    b.Wg.Wait()
    b.Wg = sync.WaitGroup{}
    return nil
}

func (b *BaseModule) Go(fn func(ctx context.Context)) {
	b.Wg.Go(func() {
		fn(b.Ctx)
	})
}

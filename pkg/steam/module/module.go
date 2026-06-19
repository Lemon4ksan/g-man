// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package module

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/lemon4ksan/aoni"
	"github.com/lemon4ksan/miyako/bus"
	"github.com/lemon4ksan/miyako/generic"
	"github.com/lemon4ksan/miyako/kata"
	"github.com/lemon4ksan/miyako/sync/spinlock"

	"github.com/lemon4ksan/g-man/pkg/log"
	"github.com/lemon4ksan/g-man/pkg/steam/community"
	"github.com/lemon4ksan/g-man/pkg/steam/id"
	"github.com/lemon4ksan/g-man/pkg/steam/protocol/enums"
	"github.com/lemon4ksan/g-man/pkg/steam/service"
	"github.com/lemon4ksan/g-man/pkg/steam/socket"
	"github.com/lemon4ksan/g-man/pkg/storage"
)

// State represents the lifecycle state of a module.
type State int32

const (
	// StateNew indicates the module is created but not yet initialized.
	StateNew State = iota
	// StateStarted indicates the module is running.
	StateStarted
	// StateClosed indicates the module has been shut down.
	StateClosed
)

// Event represents a trigger that drives a module state transition.
type Event int32

const (
	// EventStart triggers the transition from New to Started.
	EventStart Event = iota
	// EventClose triggers the transition to Closed.
	EventClose
)

var (
	// ErrClosed is returned when an operation is attempted on a shut-down client.
	ErrClosed = errors.New("steam: client is closed")

	// ErrNotAuthenticated is returned when a module requires an active session but the client is not logged in.
	ErrNotAuthenticated = errors.New("steam: not authenticated")
)

// Get is a generic helper to retrieve a typed module from the client initialization context,
// avoiding verbose manual type assertions and custom error handling.
func Get[T any](init InitContext, name string) (T, error) {
	mod := init.Module(name)
	if mod == nil {
		return generic.Zero[T](), fmt.Errorf("module %q not registered", name)
	}

	typed, ok := mod.(T)
	if !ok {
		return generic.Zero[T](), fmt.Errorf(
			"module %q has invalid type %T (expected %T)",
			name, mod, generic.Zero[T](),
		)
	}

	return typed, nil
}

// InitContext provides the module with access to the necessary client resources
// during the initialization phase, without exposing lifecycle management methods.
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
	Rest() aoni.Requester

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

// Module defines the contract for pluggable extensions.
// All modules must implement this interface to be loaded by the Steam client.
type Module interface {
	// Name returns a unique identifier for the module.
	Name() string

	// Init is called during client creation. Use this to register packet handlers
	// and subscribe to bus events.
	Init(init InitContext) error

	// Start is called when the client starts running. Use this to launch
	// background tasks (tickers, pollers). The context is canceled when the client closes.
	Start(ctx context.Context) error
}

// Dependent is an optional interface modules can implement to declare their dependencies.
type Dependent interface {
	Module
	Dependencies() []string
}

// Auth defines the contract for pluggable extensions that require authorized clients
// and depend on a valid user session.
type Auth interface {
	Module

	// StartAuthed is called after a successful Steam login and WebSession creation.
	// It is triggered every time the client re-authenticates.
	StartAuthed(ctx context.Context, auth AuthContext) error
}

// Base provides a standard implementation of the module lifecycle.
//
// It handles boilerplate like logging setup, event bus storage, and background
// task synchronization. The [Base.Fsm] field manages lifecycle state transitions,
// while the [Base.Wg] field tracks goroutines for graceful shutdown.
//
// Create new instances of Base using the [New] constructor.
type Base struct {
	// NameStr is the unique name of the module used for logging.
	NameStr string

	// Logger is a scoped logger for the module (pre-filled with module name).
	Logger log.Logger
	// Bus is the shared event bus for the client.
	Bus *bus.Bus

	// Fsm is a typed finite state machine tracking the module's lifecycle.
	Fsm *kata.FSM[State, Event]

	// Ctx is the module's internal context, cancelled when the module stops.
	Ctx context.Context
	// Cancel stops all background tasks associated with this module.
	Cancel context.CancelFunc
	// Wg tracks background goroutines to ensure graceful shutdown.
	Wg *sync.WaitGroup

	// Deps is a list of names of other modules that this module depends on.
	Deps []string

	mu *spinlock.SpinLock
}

// New creates a new Base module with the given name.
// Configure dependencies on the returned module using [Base.WithDeps].
func New(name string) Base {
	fsm := kata.NewFSM[State, Event](StateNew)
	fsm.AddRules(
		kata.TransitionRule[State, Event]{From: StateNew, Event: EventStart, To: StateStarted},
		kata.TransitionRule[State, Event]{From: StateStarted, Event: EventClose, To: StateClosed},
		kata.TransitionRule[State, Event]{From: StateNew, Event: EventClose, To: StateClosed},
	)

	return Base{
		NameStr: name,
		Logger:  log.Discard,
		Fsm:     fsm,
		Wg:      new(sync.WaitGroup),
		mu:      new(spinlock.SpinLock),
	}
}

// Name returns the module identifier.
func (b *Base) Name() string { return b.NameStr }

// Dependencies returns the list of module names that this module depends on.
func (b *Base) Dependencies() []string {
	return b.Deps
}

// WithDeps sets the dependencies for the module and returns the base module.
//
// If no arguments are passed, the dependencies slice is initialized as empty.
// Since the base module uses pointer-based synchronization fields, this builder
// is safe to call and copy by value.
func (b Base) WithDeps(deps ...string) Base {
	b.Deps = deps
	return b
}

// Init sets up common dependencies like Logger and Bus.
//
// The init argument must not be nil. If nil is passed, this method will panic
// during initialization of the Logger and Bus.
func (b *Base) Init(ctx InitContext) error {
	b.Logger = ctx.Logger().With(log.Module(b.NameStr))
	b.Bus = ctx.Bus()

	if b.Fsm == nil {
		fsm := kata.NewFSM[State, Event](StateNew)
		fsm.AddRules(
			kata.TransitionRule[State, Event]{From: StateNew, Event: EventStart, To: StateStarted},
			kata.TransitionRule[State, Event]{From: StateStarted, Event: EventClose, To: StateClosed},
			kata.TransitionRule[State, Event]{From: StateNew, Event: EventClose, To: StateClosed},
		)
		b.Fsm = fsm
	}

	if b.Wg == nil {
		b.Wg = new(sync.WaitGroup)
	}

	if b.mu == nil {
		b.mu = new(spinlock.SpinLock)
	}

	b.mu.Lock()

	if b.Ctx == nil || b.Ctx.Err() != nil {
		b.Ctx, b.Cancel = context.WithCancel(context.Background())
	}

	b.mu.Unlock()

	return nil
}

// Start initializes the module's lifecycle context.
func (b *Base) Start(ctx context.Context) error {
	b.mu.Lock()
	b.Ctx, b.Cancel = context.WithCancel(ctx)
	b.mu.Unlock()

	_ = b.Fsm.Transition(context.Background(), EventStart)

	return nil
}

// Close gracefully shuts down the module by cancelling its context and waiting
// for all spawned goroutines to finish.
func (b *Base) Close() error {
	b.mu.Lock()
	cancel := b.Cancel
	b.mu.Unlock()

	_ = b.Fsm.Transition(context.Background(), EventClose)

	if cancel != nil {
		cancel()
	}

	if b.Wg != nil {
		b.Wg.Wait()
	}

	return nil
}

// State returns the current lifecycle state of the module.
func (b *Base) State() State {
	return b.Fsm.CurrentState()
}

// Go spawns a background goroutine that is tracked by the module's WaitGroup.
//
// The provided function fn must not be nil. If nil is passed, Go panics
// inside the spawned goroutine.
// The function should respect the module's context for cancellation.
func (b *Base) Go(fn func(ctx context.Context)) {
	if b.Wg == nil {
		b.Wg = new(sync.WaitGroup)
	}

	if b.mu == nil {
		b.mu = new(spinlock.SpinLock)
	}

	b.mu.Lock()
	mCtx := b.Ctx
	b.mu.Unlock()

	b.Wg.Go(func() {
		fn(mCtx)
	})
}

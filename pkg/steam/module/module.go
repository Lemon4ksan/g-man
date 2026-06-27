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

	"github.com/lemon4ksan/g-man/pkg/log"
	"github.com/lemon4ksan/g-man/pkg/steam/community"
	"github.com/lemon4ksan/g-man/pkg/steam/id"
	"github.com/lemon4ksan/g-man/pkg/steam/protocol/enums"
	"github.com/lemon4ksan/g-man/pkg/steam/service"
	"github.com/lemon4ksan/g-man/pkg/steam/socket"
	"github.com/lemon4ksan/g-man/pkg/storage"
)

// State defines the current lifecycle stage of a module.
type State int32

const (
	// StateNew indicates the module is created but not yet initialized.
	StateNew State = iota
	// StateStarted indicates the module is running.
	StateStarted
	// StateClosed indicates the module has been shut down.
	StateClosed
)

// String returns a human-readable representation of the State.
func (s State) String() string {
	switch s {
	case StateNew:
		return "new"
	case StateStarted:
		return "started"
	case StateClosed:
		return "closed"
	default:
		return "unknown"
	}
}

// Event defines a trigger that drives a module state transition.
type Event int32

const (
	// EventStart triggers the transition from New to Started.
	EventStart Event = iota
	// EventClose triggers the transition to Closed.
	EventClose
)

// String returns a human-readable representation of the Event.
func (e Event) String() string {
	switch e {
	case EventStart:
		return "start"
	case EventClose:
		return "close"
	default:
		return "unknown"
	}
}

var (
	// ErrClosed is returned when an operation is executed on a closed or shutting down module.
	ErrClosed = errors.New("steam: client is closed")
	// ErrNotAuthenticated is returned when a module tries to access a session before successful authentication.
	ErrNotAuthenticated = errors.New("steam: not authenticated")
)

// Get retrieves a typed module from the specified [InitContext] using its unique name.
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

// InitContext exposes client configuration parameters and resources to a module during its initialization phase.
type InitContext interface {
	Storage() storage.Provider
	Bus() *bus.Bus
	Logger() log.Logger
	Service() service.Doer
	Rest() aoni.Requester
	RegisterPacketHandler(eMsg enums.EMsg, handler socket.Handler)
	RegisterServiceHandler(method string, handler socket.Handler)
	Module(name string) Module
	UnregisterPacketHandler(eMsg enums.EMsg)
	UnregisterServiceHandler(method string)
}

// AuthContext exposes resources that are only available after successful authentication.
type AuthContext interface {
	Community() community.Requester
	SteamID() id.ID
}

// Module defines the required lifecycle and identity contract for all pluggable extensions.
type Module interface {
	Name() string
	Init(init InitContext) error
	Start(ctx context.Context) error
}

// Dependent defines an optional interface to specify dependencies on other client modules.
type Dependent interface {
	Module
	Dependencies() []string
}

// Auth defines the lifecycle contract for modules that depend on an authenticated user session.
type Auth interface {
	Module
	StartAuthed(ctx context.Context, auth AuthContext) error
}

// Base provides a standard implementation of the pluggable module lifecycle contract.
type Base struct {
	NameStr string
	Logger  log.Logger
	Bus     *bus.Bus
	Fsm     *kata.FSM[State, Event]
	Ctx     context.Context
	Cancel  context.CancelFunc
	Wg      *sync.WaitGroup
	Deps    []string

	mu *sync.Mutex
}

// New creates a new [Base] module initialized with the specified name and state.
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
		mu:      new(sync.Mutex),
	}
}

// Name returns the unique string identifier of the [Base] module.
func (b *Base) Name() string { return b.NameStr }

// Dependencies returns the list of module names that this [Base] module depends on.
func (b *Base) Dependencies() []string {
	return b.Deps
}

// WithDeps sets the dependency slice for the [Base] module and returns it.
func (b Base) WithDeps(deps ...string) Base {
	b.Deps = deps
	return b
}

// Init configures common scoped dependencies such as [log.Logger] and [bus.Bus] using the provided [InitContext].
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
		b.mu = new(sync.Mutex)
	}

	b.mu.Lock()

	if b.Ctx == nil || b.Ctx.Err() != nil {
		b.Ctx, b.Cancel = context.WithCancel(context.Background())
	}

	b.mu.Unlock()

	return nil
}

// Start activates the [Base] module's lifecycle using the provided context.
func (b *Base) Start(ctx context.Context) error {
	b.mu.Lock()
	b.Ctx, b.Cancel = context.WithCancel(ctx)
	b.mu.Unlock()

	_ = b.Fsm.Transition(context.Background(), EventStart)

	return nil
}

// Close shuts down the [Base] module, cancels its context, and waits for tracked background tasks to complete.
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

// State returns the current [State] of the [Base] module's lifecycle.
func (b *Base) State() State {
	return b.Fsm.CurrentState()
}

// IsNew returns true if the module is in StateNew.
func (b *Base) IsNew() bool { return b.State() == StateNew }

// IsStarted returns true if the module is in StateStarted.
func (b *Base) IsStarted() bool { return b.State() == StateStarted }

// IsClosed returns true if the module is in StateClosed.
func (b *Base) IsClosed() bool { return b.State() == StateClosed }

// Go spawns an asynchronous background task tracked by the internal [sync.WaitGroup].
func (b *Base) Go(fn func(ctx context.Context)) {
	if b.Wg == nil {
		b.Wg = new(sync.WaitGroup)
	}

	if b.mu == nil {
		b.mu = new(sync.Mutex)
	}

	b.mu.Lock()
	mCtx := b.Ctx
	b.mu.Unlock()

	b.Wg.Go(func() {
		fn(mCtx)
	})
}

// AuthBase extends [Base] to provide boilerplate-free state management for authorized modules.
// It automatically tracks the active [AuthContext] and provides thread-safe helpers.
type AuthBase struct {
	Base

	authMu  sync.RWMutex
	authCtx AuthContext
}

// NewAuthBase creates a new [AuthBase] module with the specified name.
func NewAuthBase(name string) AuthBase {
	return AuthBase{
		Base: New(name),
	}
}

// StartAuthed caches the authenticated context and transitions the module.
func (ab *AuthBase) StartAuthed(ctx context.Context, auth AuthContext) error {
	ab.authMu.Lock()
	ab.authCtx = auth
	ab.authMu.Unlock()

	return nil
}

// AuthContext returns the currently cached [AuthContext], or nil if not authenticated.
func (ab *AuthBase) AuthContext() AuthContext {
	ab.authMu.RLock()
	defer ab.authMu.RUnlock()
	return ab.authCtx
}

// SteamID returns the authenticated SteamID, or 0 if not authenticated.
func (ab *AuthBase) SteamID() id.ID {
	ab.authMu.RLock()
	defer ab.authMu.RUnlock()

	if ab.authCtx == nil {
		return 0
	}

	return ab.authCtx.SteamID()
}

// Community returns the authorized community requester, or nil if not authenticated.
func (ab *AuthBase) Community() community.Requester {
	ab.authMu.RLock()
	defer ab.authMu.RUnlock()

	if ab.authCtx == nil {
		return nil
	}

	return ab.authCtx.Community()
}

// IsAuthenticated returns true if an active authenticated context is present.
func (ab *AuthBase) IsAuthenticated() bool {
	ab.authMu.RLock()
	defer ab.authMu.RUnlock()
	return ab.authCtx != nil
}

// ClearAuth clears the active authentication context (e.g. on logout/reconnect).
func (ab *AuthBase) ClearAuth() {
	ab.authMu.Lock()
	ab.authCtx = nil
	ab.authMu.Unlock()
}

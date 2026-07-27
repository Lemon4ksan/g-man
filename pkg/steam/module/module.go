// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package module defines extensible plugin interfaces and base lifecycle state machines for Steam client extensions.
package module

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/lemon4ksan/aoni/request"
	"github.com/lemon4ksan/miyako/bus"
	"github.com/lemon4ksan/miyako/generic"
	"github.com/lemon4ksan/miyako/kata"
	"github.com/lemon4ksan/miyako/log"

	"github.com/lemon4ksan/g-man/pkg/steam/community"
	"github.com/lemon4ksan/g-man/pkg/steam/id"
	"github.com/lemon4ksan/g-man/pkg/steam/protocol/enums"
	"github.com/lemon4ksan/g-man/pkg/steam/service"
	"github.com/lemon4ksan/g-man/pkg/steam/socket"
	"github.com/lemon4ksan/g-man/pkg/storage"
)

type State int32

const (
	StateNew State = iota
	StateStarted
	StateClosed
)

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

type Event int32

const (
	EventStart Event = iota
	EventClose
)

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
	// ErrClosed indicates an operation was performed on a closed module or client.
	ErrClosed = errors.New("steam: client is closed")
	// ErrNotAuthenticated indicates authenticated resources were accessed before user logon.
	ErrNotAuthenticated = errors.New("steam: not authenticated")
)

// Get retrieves a typed module instance by name from an InitContext.
func Get[T any](init InitContext, name string) (T, error) {
	mod := init.Module(name)
	if mod == nil {
		return generic.Zero[T](), fmt.Errorf("module %q not registered", name)
	}

	typed, ok := mod.(T)
	if !ok {
		return generic.Zero[T](), fmt.Errorf(
			"module %q has invalid type %T (expected %T)",
			name,
			mod,
			generic.Zero[T](),
		)
	}

	return typed, nil
}

// InitContext provides client configuration, event bus, and packet registration handlers to initializing modules.
type InitContext interface {
	Storage() storage.Provider
	Bus() *bus.Bus
	Logger() log.Logger
	Service() service.Doer
	Rest() request.Requester
	RegisterPacketHandler(eMsg enums.EMsg, handler socket.Handler)
	RegisterServiceHandler(method string, handler socket.Handler)
	Module(name string) Module
	UnregisterPacketHandler(eMsg enums.EMsg)
	UnregisterServiceHandler(method string)
}

// AuthContext provides authenticated resources available after user logon.
type AuthContext interface {
	Community() community.Requester
	SteamID() id.ID
}

// Module defines required lifecycle methods for extensions.
type Module interface {
	Name() string
	Init(init InitContext) error
	Start(ctx context.Context) error
}

// Dependent specifies explicit inter-module dependencies.
type Dependent interface {
	Module
	Dependencies() []string
}

// Auth defines lifecycle methods for modules dependent on authenticated user sessions.
type Auth interface {
	Module
	StartAuthed(ctx context.Context, auth AuthContext) error
}

// Base provides standard lifecycle state machine management, logger binding, and task waitgroup tracking.
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

// New constructs a Base module.
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

func (b *Base) Name() string { return b.NameStr }

func (b *Base) Dependencies() []string { return b.Deps }

func (b Base) WithDeps(deps ...string) Base {
	b.Deps = deps

	return b
}

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

func (b *Base) Start(ctx context.Context) error {
	b.mu.Lock()
	b.Ctx, b.Cancel = context.WithCancel(ctx)
	b.mu.Unlock()

	_ = b.Fsm.Transition(context.Background(), EventStart)

	return nil
}

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

func (b *Base) State() State { return b.Fsm.CurrentState() }

func (b *Base) IsNew() bool { return b.State() == StateNew }

func (b *Base) IsStarted() bool { return b.State() == StateStarted }

func (b *Base) IsClosed() bool { return b.State() == StateClosed }

// Go launches a background task tracked by the internal WaitGroup.
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

// AuthBase extends Base with thread-safe AuthContext storage.
type AuthBase struct {
	Base

	authMu  sync.RWMutex
	authCtx AuthContext
}

// NewAuthBase constructs an AuthBase instance.
func NewAuthBase(name string) AuthBase {
	return AuthBase{
		Base: New(name),
	}
}

func (ab *AuthBase) StartAuthed(ctx context.Context, auth AuthContext) error {
	ab.authMu.Lock()
	ab.authCtx = auth
	ab.authMu.Unlock()

	return nil
}

func (ab *AuthBase) AuthContext() AuthContext {
	ab.authMu.RLock()
	defer ab.authMu.RUnlock()

	return ab.authCtx
}

func (ab *AuthBase) SteamID() id.ID {
	ab.authMu.RLock()
	defer ab.authMu.RUnlock()

	if ab.authCtx == nil {
		return 0
	}

	return ab.authCtx.SteamID()
}

func (ab *AuthBase) Community() community.Requester {
	ab.authMu.RLock()
	defer ab.authMu.RUnlock()

	if ab.authCtx == nil {
		return nil
	}

	return ab.authCtx.Community()
}

func (ab *AuthBase) IsAuthenticated() bool {
	ab.authMu.RLock()
	defer ab.authMu.RUnlock()

	return ab.authCtx != nil
}

func (ab *AuthBase) ClearAuth() {
	ab.authMu.Lock()
	ab.authCtx = nil
	ab.authMu.Unlock()
}

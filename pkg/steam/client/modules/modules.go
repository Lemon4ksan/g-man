// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package modules provides a lifecycle manager for Steam client extensions.
// It coordinates initialization, startup, and shutdown sequences for registered
// components, and supports dynamic activation based on client status.
//
// Use [Manager] as the main lifecycle orchestrator.
//
// Basic usage:
//
//	mgr := modules.New(stateProvider, initCtx, authCtx)
//	if err := mgr.Add(myModule); err != nil {
//		log.Fatal(err)
//	}
//	if err := mgr.InitAll(ctx); err != nil {
//		log.Fatal(err)
//	}
package modules

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"sync"

	"github.com/lemon4ksan/miyako/lifecycle"

	"github.com/lemon4ksan/g-man/pkg/steam/module"
)

var (
	// ErrNilModule is returned when attempting to add a nil module.
	ErrNilModule = errors.New("modules: cannot add or register nil module")
	// ErrDuplicate is returned when attempting to add a module that already exists.
	ErrDuplicate = errors.New("modules: duplicate module")
)

// Error details an execution failure during a module lifecycle transition.
type Error struct {
	Op     string
	Module string
	Err    error
}

func (e Error) Error() string {
	return fmt.Sprintf("modules: %s for %q failed: %v", e.Op, e.Module, e.Err)
}

func (e Error) Unwrap() error {
	return e.Err
}

// StateProvider provides status information regarding client execution states.
// Implementations of this interface are used by [Manager] to determine if modules
// should be dynamically initialized and started upon registration.
type StateProvider interface {
	// IsRunning reports whether the client background systems are active.
	IsRunning() bool
	// IsAuthorized reports whether the client has completed user authorization.
	IsAuthorized() bool
}

// Manager orchestrates the execution lifecycle and dependencies of registered modules.
// It manages transitions through initialization, startup, authorization, and shutdown.
// Use [New] to create an instance.
type Manager struct {
	orchestrator  *lifecycle.Orchestrator
	stateProvider StateProvider

	mu      sync.RWMutex
	modules map[string]module.Module

	initCtx module.InitContext
	authCtx module.AuthContext
}

// New creates a new [Manager] using the provided state provider and contexts.
// Returns an empty manager if arguments are nil, though nil arguments may cause
// runtime panics during subsequent module registration.
func New(
	stateProvider StateProvider,
	initCtx module.InitContext,
	authCtx module.AuthContext,
) *Manager {
	return &Manager{
		orchestrator:  lifecycle.NewOrchestrator(),
		modules:       make(map[string]module.Module),
		stateProvider: stateProvider,
		initCtx:       initCtx,
		authCtx:       authCtx,
	}
}

// Get retrieves a registered [module.Module] by its name.
// Returns nil if no module with the specified name is found, or if name is empty.
func (m *Manager) Get(name string) module.Module {
	m.mu.RLock()
	defer m.mu.RUnlock()

	return m.modules[name]
}

// Add registers a [module.Module] with the manager's internal registry.
// Returns an error if a module with the same name is already registered.
// Returns an error if mod is nil.
func (m *Manager) Add(mod module.Module) error {
	if mod == nil {
		return ErrNilModule
	}

	if _, exists := m.modules[mod.Name()]; exists {
		return fmt.Errorf("%w: '%q' already registered", ErrDuplicate, mod.Name())
	}

	m.modules[mod.Name()] = mod

	m.orchestrator.Register(&moduleAdapter{Mod: mod, InitCtx: m.initCtx})

	return nil
}

// All returns a slice of all currently registered [module.Module] instances.
// Returns an empty slice if no modules are registered.
func (m *Manager) All() []module.Module {
	m.mu.RLock()
	defer m.mu.RUnlock()

	return slices.Collect(maps.Values(m.modules))
}

// Register adds a [module.Module] and immediately attempts to sync its state.
// If the [StateProvider] reports the client is running, the module is initialized and started.
// If the [StateProvider] reports the client is authorized, any [module.Auth] module is also started.
// Returns an error if addition, initialization, or startup fails.
// Returns an error if mod or ctx is nil.
func (m *Manager) Register(ctx context.Context, mod module.Module) error {
	if mod == nil {
		return ErrNilModule
	}

	if err := m.Add(mod); err != nil {
		return err
	}

	if m.stateProvider.IsRunning() {
		if err := mod.Init(m.initCtx); err != nil {
			return &Error{Op: "dynamic init", Module: mod.Name(), Err: err}
		}

		if err := mod.Start(ctx); err != nil {
			return &Error{Op: "dynamic start", Module: mod.Name(), Err: err}
		}
	}

	if m.stateProvider.IsAuthorized() {
		if authMod, ok := mod.(module.Auth); ok {
			if err := authMod.StartAuthed(ctx, m.authCtx); err != nil {
				return &Error{Op: "dynamic start authed", Module: mod.Name(), Err: err}
			}
		}
	}

	return nil
}

// InitAll triggers initialization for all registered modules.
// Returns an error if any module initialization fails or if the context ctx is canceled.
func (m *Manager) InitAll(ctx context.Context) error {
	return m.orchestrator.InitAll(ctx)
}

// StartAll starts the execution loop for all registered modules.
// Returns an error if any module fails to start or if the context ctx is canceled.
func (m *Manager) StartAll(ctx context.Context) error {
	return m.orchestrator.StartAll(ctx)
}

// StopAll stops all registered modules in reverse dependency order.
// Returns an error if any module fails to stop or if the context ctx is canceled.
func (m *Manager) StopAll(ctx context.Context) error {
	return m.orchestrator.StopAll(ctx)
}

// StartAuthedAll starts the authenticated routines for registered [module.Auth] modules.
// It uses the internal context provided at manager creation.
// Returns an error if any authenticated module fails to start or if the context ctx is canceled.
func (m *Manager) StartAuthedAll(ctx context.Context) error {
	for _, mod := range m.All() {
		if authMod, ok := mod.(module.Auth); ok {
			if err := authMod.StartAuthed(ctx, m.authCtx); err != nil {
				return &Error{Op: "start authed", Module: mod.Name(), Err: err}
			}
		}
	}

	return nil
}

type moduleAdapter struct {
	Mod     module.Module
	InitCtx module.InitContext
	Cancel  context.CancelFunc
}

func (a *moduleAdapter) Name() string { return a.Mod.Name() }

func (a *moduleAdapter) Dependencies() []string {
	if dep, ok := a.Mod.(module.Dependent); ok {
		return dep.Dependencies()
	}

	return nil
}

func (a *moduleAdapter) Init(ctx context.Context) error {
	return a.Mod.Init(a.InitCtx)
}

func (a *moduleAdapter) Start(ctx context.Context) error {
	startCtx, cancel := context.WithCancel(ctx)
	a.Cancel = cancel
	return a.Mod.Start(startCtx)
}

func (a *moduleAdapter) Stop(ctx context.Context) error {
	if a.Cancel != nil {
		a.Cancel()
	}

	if closer, ok := a.Mod.(interface{ Close() error }); ok {
		return closer.Close()
	}

	return nil
}

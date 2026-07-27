// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package modules provides a lifecycle manager for Steam client extensions.
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
	// ErrNilModule indicates an attempt to register or operate on a nil module instance.
	ErrNilModule = errors.New("modules: cannot add or register nil module")
	// ErrDuplicate indicates that a module with the same identifier is already registered.
	ErrDuplicate = errors.New("modules: duplicate module")
)

// Error describes an execution failure encountered during a module lifecycle transition.
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

// StateProvider reports background and authentication states of the host Steam client.
// Implementations drive dynamic module initialization during late registration.
type StateProvider interface {
	IsRunning() bool
	IsAuthorized() bool
}

// Manager orchestrates lifecycle state transitions and dependency ordering for client extensions.
//
// Thread Safety:
//   - All public methods are safe for concurrent usage across multiple goroutines.
type Manager struct {
	orchestrator  *lifecycle.Orchestrator
	stateProvider StateProvider

	mu      sync.RWMutex
	modules map[string]module.Module

	initCtx module.InitContext
	authCtx module.AuthContext
}

// New creates a lifecycle Manager configured with state provider and module initialization contexts.
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

// Get retrieves a registered module by its string identifier.
//
// Returns:
//   - The matching module instance, or nil if not registered or if name is empty.
func (m *Manager) Get(name string) module.Module {
	m.mu.RLock()
	defer m.mu.RUnlock()

	return m.modules[name]
}

// Add registers a module in the internal registry and lifecycle orchestrator.
//
// Thread Safety:
//   - Thread-safe. Acquires an exclusive write lock.
//
// Returns:
//   - ErrNilModule if mod is nil.
//   - ErrDuplicate wrapped error if a module with the same name exists.
func (m *Manager) Add(mod module.Module) error {
	if mod == nil {
		return ErrNilModule
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.modules[mod.Name()]; exists {
		return fmt.Errorf("%w: '%q' already registered", ErrDuplicate, mod.Name())
	}

	m.modules[mod.Name()] = mod
	m.orchestrator.Register(&moduleAdapter{Mod: mod, InitCtx: m.initCtx})

	return nil
}

// All returns a point-in-time snapshot of all currently registered modules.
func (m *Manager) All() []module.Module {
	m.mu.RLock()
	defer m.mu.RUnlock()

	return slices.Collect(maps.Values(m.modules))
}

// Register adds a module and synchronizes its operational state with the client.
// If the client is currently running or authorized, the newly added module is dynamically initialized and started.
//
// Returns:
//   - ErrNilModule if mod is nil.
//   - Error wrapping the underlying cause if registration, initialization, or startup fails.
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

// InitAll executes the initialization phase across all registered modules.
func (m *Manager) InitAll(ctx context.Context) error {
	return m.orchestrator.InitAll(ctx)
}

// StartAll executes the startup phase across all registered modules according to dependency order.
func (m *Manager) StartAll(ctx context.Context) error {
	return m.orchestrator.StartAll(ctx)
}

// StopAll halts all active modules in reverse dependency order.
func (m *Manager) StopAll(ctx context.Context) error {
	return m.orchestrator.StopAll(ctx)
}

// StartAuthedAll executes the authenticated startup sequence for all modules implementing module.Auth.
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

// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package steam

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"sync"

	"github.com/lemon4ksan/miyako/kata"
	"github.com/lemon4ksan/miyako/lifecycle"

	"github.com/lemon4ksan/g-man/pkg/steam/module"
)

// ModuleManager manages the lifecycle of modules.
type ModuleManager struct {
	orchestrator *lifecycle.Orchestrator

	mu      sync.RWMutex
	modules map[string]module.Module

	initCtx module.InitContext
	authCtx module.AuthContext
	fsm     *kata.FSM[State, Event]
}

// Get returns the module with the given name, or nil if not found.
func (m *ModuleManager) Get(name string) module.Module {
	m.mu.RLock()
	defer m.mu.RUnlock()

	return m.modules[name]
}

// Add adds a module to the manager. If the module is already registered, it returns an error.
func (m *ModuleManager) Add(mod module.Module) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.modules[mod.Name()]; exists {
		return fmt.Errorf("module %q already registered", mod.Name())
	}

	m.modules[mod.Name()] = mod
	if m.orchestrator == nil {
		m.orchestrator = lifecycle.NewOrchestrator()
	}

	m.orchestrator.Register(&moduleAdapter{mod: mod, initCtx: m.initCtx})

	return nil
}

// All returns a slice containing all registered modules.
func (m *ModuleManager) All() []module.Module {
	m.mu.RLock()
	defer m.mu.RUnlock()

	return slices.Collect(maps.Values(m.modules))
}

// Register registers a module with the manager.
func (m *ModuleManager) Register(ctx context.Context, mod module.Module) error {
	if err := m.Add(mod); err != nil {
		return err
	}

	var state State
	if m.fsm != nil {
		state = m.fsm.CurrentState()
	}

	if state >= StateRunning {
		if err := mod.Init(m.initCtx); err != nil {
			return fmt.Errorf("module %q: dynamic init failed: %w", mod.Name(), err)
		}

		if err := mod.Start(ctx); err != nil {
			return fmt.Errorf("module %q: dynamic start failed: %w", mod.Name(), err)
		}
	}

	if state == StateAuthorized {
		if authMod, ok := mod.(module.Auth); ok {
			if err := authMod.StartAuthed(ctx, m.authCtx); err != nil {
				return fmt.Errorf("module %q: dynamic start authed failed: %w", mod.Name(), err)
			}
		}
	}

	return nil
}

// InitAll initializes all registered modules.
func (m *ModuleManager) InitAll(ctx context.Context) error {
	m.mu.Lock()
	if m.orchestrator == nil {
		m.orchestrator = lifecycle.NewOrchestrator()
	}

	o := m.orchestrator
	m.mu.Unlock()

	return o.InitAll(ctx)
}

// StartAll starts all registered modules.
func (m *ModuleManager) StartAll(ctx context.Context) error {
	m.mu.Lock()
	if m.orchestrator == nil {
		m.orchestrator = lifecycle.NewOrchestrator()
	}

	o := m.orchestrator
	m.mu.Unlock()

	return o.StartAll(ctx)
}

// StopAll stops all registered modules.
func (m *ModuleManager) StopAll(ctx context.Context) error {
	m.mu.Lock()
	if m.orchestrator == nil {
		m.orchestrator = lifecycle.NewOrchestrator()
	}

	o := m.orchestrator
	m.mu.Unlock()

	return o.StopAll(ctx)
}

// StartAuthedAll starts all registered modules with the given auth context.
func (m *ModuleManager) StartAuthedAll(ctx context.Context, authCtx module.AuthContext) error {
	m.mu.RLock()
	defer m.mu.RUnlock()

	for _, mod := range m.modules {
		if authMod, ok := mod.(module.Auth); ok {
			if err := authMod.StartAuthed(ctx, authCtx); err != nil {
				return fmt.Errorf("module %q: start authed failed: %w", mod.Name(), err)
			}
		}
	}

	return nil
}

type moduleAdapter struct {
	mod     module.Module
	initCtx module.InitContext
	cancel  context.CancelFunc
}

func (a *moduleAdapter) Name() string { return a.mod.Name() }

func (a *moduleAdapter) Dependencies() []string {
	if dep, ok := a.mod.(module.Dependent); ok {
		return dep.Dependencies()
	}

	return nil
}

func (a *moduleAdapter) Init(ctx context.Context) error {
	return a.mod.Init(a.initCtx)
}

func (a *moduleAdapter) Start(ctx context.Context) error {
	startCtx, cancel := context.WithCancel(ctx)
	a.cancel = cancel
	return a.mod.Start(startCtx)
}

func (a *moduleAdapter) Stop(ctx context.Context) error {
	if a.cancel != nil {
		a.cancel()
	}

	if closer, ok := a.mod.(interface{ Close() error }); ok {
		return closer.Close()
	}

	return nil
}

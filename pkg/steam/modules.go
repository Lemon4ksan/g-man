// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package steam

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"

	"github.com/lemon4ksan/g-man/pkg/steam/module"
)

type ModuleManager struct {
	modules map[string]module.Module
	mu      sync.RWMutex

	initCtx module.InitContext
	authCtx module.AuthContext
	state   *atomic.Int32
}

func (m *ModuleManager) Get(name string) module.Module {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.modules[name]
}

func (m *ModuleManager) Add(mod module.Module) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.modules[mod.Name()] = mod
}

func (m *ModuleManager) Register(ctx context.Context, mod module.Module) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	name := mod.Name()
	if _, exists := m.modules[name]; exists {
		return fmt.Errorf("module %q already registered", name)
	}

	m.modules[name] = mod

	currentState := State(m.state.Load())

	if currentState >= StateRunning {
		if err := mod.Init(m.initCtx); err != nil {
			return err
		}

		if err := mod.Start(ctx); err != nil {
			return err
		}
	}

	if currentState == StateAuthorized {
		if authed, ok := mod.(module.Auth); ok {
			if err := authed.StartAuthed(ctx, m.authCtx); err != nil {
				return err
			}
		}
	}

	return nil
}

func (m *ModuleManager) InitAll(ctx module.InitContext) error {
	m.mu.RLock()

	current := make([]module.Module, 0, len(m.modules))
	for _, mod := range m.modules {
		current = append(current, mod)
	}

	m.mu.RUnlock()

	var errs []error
	for _, mod := range current {
		if err := mod.Init(ctx); err != nil {
			errs = append(errs, fmt.Errorf("failed to init module \"%s\": %w", mod.Name(), err))
		}
	}

	return errors.Join(errs...)
}

func (m *ModuleManager) StartAll(ctx context.Context) error {
	m.mu.RLock()

	currentModules := make([]module.Module, 0, len(m.modules))
	for _, mod := range m.modules {
		currentModules = append(currentModules, mod)
	}

	m.mu.RUnlock()

	var errs []error
	for _, mod := range currentModules {
		if err := mod.Start(ctx); err != nil {
			errs = append(errs, fmt.Errorf("failed to start module %q: %w", mod.Name(), err))
		}
	}

	return errors.Join(errs...)
}

func (m *ModuleManager) StartAuthedAll(ctx context.Context, actx module.AuthContext) error {
	m.mu.RLock()

	mods := make([]module.Module, 0, len(m.modules))
	for _, mod := range m.modules {
		mods = append(mods, mod)
	}

	m.mu.RUnlock()

	var errs []error
	for _, mod := range mods {
		if authedMod, ok := mod.(module.Auth); ok {
			if err := authedMod.StartAuthed(ctx, actx); err != nil {
				errs = append(errs, fmt.Errorf("module %q failed StartAuthed: %w", mod.Name(), err))
			}
		}
	}

	return errors.Join(errs...)
}

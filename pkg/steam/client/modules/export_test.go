// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package modules

import (
	"github.com/lemon4ksan/miyako/lifecycle"

	"github.com/lemon4ksan/g-man/pkg/steam/module"
)

type ModuleAdapter = moduleAdapter

func (m *Manager) Orchestrator() *lifecycle.Orchestrator {
	return m.orchestrator
}

func (m *Manager) Modules() map[string]module.Module {
	return m.modules
}

func (m *Manager) StateProvider() StateProvider {
	return m.stateProvider
}

func (m *Manager) InitCtx() module.InitContext {
	return m.initCtx
}

func (m *Manager) AuthCtx() module.AuthContext {
	return m.authCtx
}

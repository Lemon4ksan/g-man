// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package modules_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/lemon4ksan/g-man/internal/client/modules"
	"github.com/lemon4ksan/g-man/pkg/steam/id"
	"github.com/lemon4ksan/g-man/pkg/steam/module"
	steammock "github.com/lemon4ksan/g-man/pkg/test/mock"
)

type testModule struct {
	name      string
	initFunc  func(ctx module.InitContext) error
	startFunc func(ctx context.Context) error
}

func (tm *testModule) Name() string {
	return tm.name
}

func (tm *testModule) Init(ctx module.InitContext) error {
	if tm.initFunc != nil {
		return tm.initFunc(ctx)
	}

	return nil
}

func (tm *testModule) Start(ctx context.Context) error {
	if tm.startFunc != nil {
		return tm.startFunc(ctx)
	}

	return nil
}

type testAuthModule struct {
	testModule
	startAuthedFunc func(ctx context.Context, authCtx module.AuthContext) error
}

func (tam *testAuthModule) StartAuthed(ctx context.Context, authCtx module.AuthContext) error {
	if tam.startAuthedFunc != nil {
		return tam.startAuthedFunc(ctx, authCtx)
	}

	return nil
}

type testDependentModule struct {
	testModule
	dependencies []string
}

func (tdm *testDependentModule) Dependencies() []string {
	return tdm.dependencies
}

type testCloserModule struct {
	testModule
	closeFunc func() error
}

func (tcm *testCloserModule) Close() error {
	if tcm.closeFunc != nil {
		return tcm.closeFunc()
	}

	return nil
}

type mockStateProvider struct {
	running bool
	authed  bool
}

func (sp *mockStateProvider) IsRunning() bool {
	return sp.running
}

func (sp *mockStateProvider) IsAuthorized() bool {
	return sp.authed
}

// newTestManager is a helper to construct a Manager with standard test dependencies.
func newTestManager(t *testing.T, sp modules.StateProvider) (*modules.Manager, module.InitContext, module.AuthContext) {
	t.Helper()

	initCtx := steammock.NewInitContext()
	authCtx := steammock.NewAuthContext(id.ID(12345))
	m := modules.New(sp, initCtx, authCtx)

	return m, initCtx, authCtx
}

func TestNew_ValidInputs_CreatesManagerWithCorrectFields(t *testing.T) {
	t.Parallel()

	stateProv := &mockStateProvider{}
	initCtx := steammock.NewInitContext()
	authCtx := steammock.NewAuthContext(id.ID(12345))

	m := modules.New(stateProv, initCtx, authCtx)

	assert.NotNil(t, m)
	assert.NotNil(t, m.Orchestrator())
	assert.NotNil(t, m.Modules())
	assert.Equal(t, stateProv, m.StateProvider())
	assert.Equal(t, initCtx, m.InitCtx())
	assert.Equal(t, authCtx, m.AuthCtx())
}

func TestManager_Get_ExistingAndNonExistent_ReturnsExpectedModule(t *testing.T) {
	t.Parallel()

	m, _, _ := newTestManager(t, &mockStateProvider{})
	mod := &testModule{name: "mod1"}

	err := m.Add(mod)
	assert.NoError(t, err)

	assert.Equal(t, mod, m.Get("mod1"))
	assert.Nil(t, m.Get("non-existent"))
}

func TestManager_Add_MultipleModules_ManagesDuplicatesAndReturnsAll(t *testing.T) {
	t.Parallel()

	m, _, _ := newTestManager(t, &mockStateProvider{})

	mod1 := &testModule{name: "mod1"}
	mod2 := &testModule{name: "mod2"}

	err := m.Add(mod1)
	assert.NoError(t, err)

	err = m.Add(mod2)
	assert.NoError(t, err)

	err = m.Add(mod1)
	assert.ErrorIs(t, err, modules.ErrDuplicate)

	mNilOrch := modules.New(nil, nil, nil)
	err = mNilOrch.Add(mod1)
	assert.NoError(t, err)
	assert.NotNil(t, mNilOrch.Orchestrator())

	all := m.All()
	assert.Len(t, all, 2)
	assert.Contains(t, all, module.Module(mod1))
	assert.Contains(t, all, module.Module(mod2))
}

func TestManager_Register_VariousStates_HandlesLifecycle(t *testing.T) {
	t.Parallel()

	t.Run("already_registered", func(t *testing.T) {
		t.Parallel()
		m, _, _ := newTestManager(t, &mockStateProvider{})
		mod := &testModule{name: "mod1"}

		err := m.Add(mod)
		assert.NoError(t, err)

		err = m.Register(t.Context(), mod)
		assert.ErrorIs(t, err, modules.ErrDuplicate)
	})

	t.Run("not_running_and_not_authorized", func(t *testing.T) {
		t.Parallel()

		sp := &mockStateProvider{running: false, authed: false}
		m, _, _ := newTestManager(t, sp)

		initCalled := false
		startCalled := false
		mod := &testModule{
			name: "mod1",
			initFunc: func(ctx module.InitContext) error {
				initCalled = true
				return nil
			},
			startFunc: func(ctx context.Context) error {
				startCalled = true
				return nil
			},
		}

		err := m.Register(t.Context(), mod)
		assert.NoError(t, err)
		assert.False(t, initCalled)
		assert.False(t, startCalled)
	})

	t.Run("running_and_authorized", func(t *testing.T) {
		t.Parallel()

		sp := &mockStateProvider{running: true, authed: true}
		m, initCtx, authCtx := newTestManager(t, sp)

		var (
			initCalledWith        module.InitContext
			startAuthedCalledWith module.AuthContext
		)

		startCalled := false

		mod := &testAuthModule{
			testModule: testModule{
				name: "mod1",
				initFunc: func(ctx module.InitContext) error {
					initCalledWith = ctx
					return nil
				},
				startFunc: func(ctx context.Context) error {
					startCalled = true
					return nil
				},
			},
			startAuthedFunc: func(ctx context.Context, actx module.AuthContext) error {
				startAuthedCalledWith = actx
				return nil
			},
		}

		err := m.Register(t.Context(), mod)
		assert.NoError(t, err)
		assert.Equal(t, initCtx, initCalledWith)
		assert.True(t, startCalled)
		assert.Equal(t, authCtx, startAuthedCalledWith)
	})

	t.Run("running_but_init_fails", func(t *testing.T) {
		t.Parallel()

		sp := &mockStateProvider{running: true, authed: false}
		m, _, _ := newTestManager(t, sp)

		errFailed := errors.New("init-failed")
		mod := &testModule{
			name: "mod1",
			initFunc: func(ctx module.InitContext) error {
				return errFailed
			},
		}

		err := m.Register(t.Context(), mod)

		var modErr *modules.Error
		assert.ErrorAs(t, err, &modErr)
		assert.Equal(t, "dynamic init", modErr.Op)
		assert.Equal(t, "mod1", modErr.Module)
		assert.ErrorIs(t, errFailed, modErr.Err)
	})

	t.Run("running_but_start_fails", func(t *testing.T) {
		t.Parallel()

		sp := &mockStateProvider{running: true, authed: false}
		m, _, _ := newTestManager(t, sp)

		errFailed := errors.New("start-failed")
		mod := &testModule{
			name: "mod1",
			initFunc: func(ctx module.InitContext) error {
				return nil
			},
			startFunc: func(ctx context.Context) error {
				return errFailed
			},
		}

		err := m.Register(t.Context(), mod)

		var modErr *modules.Error
		assert.ErrorAs(t, err, &modErr)
		assert.Equal(t, "dynamic start", modErr.Op)
		assert.Equal(t, "mod1", modErr.Module)
		assert.ErrorIs(t, errFailed, modErr.Err)
	})

	t.Run("authorized_but_start_authed_fails", func(t *testing.T) {
		t.Parallel()

		sp := &mockStateProvider{running: false, authed: true}
		m, _, _ := newTestManager(t, sp)

		errFailed := errors.New("auth-failed")
		mod := &testAuthModule{
			testModule: testModule{
				name: "mod1",
			},
			startAuthedFunc: func(ctx context.Context, actx module.AuthContext) error {
				return errFailed
			},
		}

		err := m.Register(t.Context(), mod)

		var modErr *modules.Error
		assert.ErrorAs(t, err, &modErr)
		assert.Equal(t, "dynamic start authed", modErr.Op)
		assert.Equal(t, "mod1", modErr.Module)
		assert.ErrorIs(t, errFailed, modErr.Err)
	})
}

func TestManager_LifecycleAll_VariousScenarios_RunsCorrectly(t *testing.T) {
	t.Parallel()

	t.Run("lazy_init_orchestrator_on_all", func(t *testing.T) {
		t.Parallel()

		m1 := modules.New(nil, nil, nil)
		err := m1.InitAll(t.Context())
		assert.NoError(t, err)
		assert.NotNil(t, m1.Orchestrator())

		m2 := modules.New(nil, nil, nil)
		err = m2.StartAll(t.Context())
		assert.NoError(t, err)
		assert.NotNil(t, m2.Orchestrator())

		m3 := modules.New(nil, nil, nil)
		err = m3.StopAll(t.Context())
		assert.NoError(t, err)
		assert.NotNil(t, m3.Orchestrator())
	})

	t.Run("delegation_and_success", func(t *testing.T) {
		t.Parallel()
		m, _, _ := newTestManager(t, &mockStateProvider{})

		initCalled := false
		startCalled := false
		closeCalled := false

		mod := &testCloserModule{
			testModule: testModule{
				name: "mod1",
				initFunc: func(ctx module.InitContext) error {
					initCalled = true
					return nil
				},
				startFunc: func(ctx context.Context) error {
					startCalled = true
					return nil
				},
			},
			closeFunc: func() error {
				closeCalled = true
				return nil
			},
		}

		err := m.Add(mod)
		assert.NoError(t, err)

		err = m.InitAll(t.Context())
		assert.NoError(t, err)
		assert.True(t, initCalled)

		err = m.StartAll(t.Context())
		assert.NoError(t, err)
		assert.True(t, startCalled)

		err = m.StopAll(t.Context())
		assert.NoError(t, err)
		assert.True(t, closeCalled)
	})

	t.Run("error_propagation", func(t *testing.T) {
		t.Parallel()
		mInit, _, _ := newTestManager(t, &mockStateProvider{})
		_ = mInit.Add(&testModule{
			name: "mod-fail",
			initFunc: func(ctx module.InitContext) error {
				return errors.New("init-fail")
			},
		})
		err := mInit.InitAll(t.Context())
		assert.Error(t, err)

		mStart, _, _ := newTestManager(t, &mockStateProvider{})
		_ = mStart.Add(&testModule{
			name: "mod-fail",
			startFunc: func(ctx context.Context) error {
				return errors.New("start-fail")
			},
		})
		err = mStart.InitAll(t.Context())
		assert.NoError(t, err)
		err = mStart.StartAll(t.Context())
		assert.Error(t, err)

		mStop, _, _ := newTestManager(t, &mockStateProvider{})
		_ = mStop.Add(&testCloserModule{
			testModule: testModule{name: "mod-fail"},
			closeFunc: func() error {
				return errors.New("close-fail")
			},
		})
		err = mStop.InitAll(t.Context())
		assert.NoError(t, err)
		err = mStop.StartAll(t.Context())
		assert.NoError(t, err)
		err = mStop.StopAll(t.Context())
		assert.NoError(t, err)
	})
}

func TestManager_StartAuthedAll_VariousScenarios_RunsExpected(t *testing.T) {
	t.Parallel()

	t.Run("no_auth_modules", func(t *testing.T) {
		t.Parallel()
		m, _, _ := newTestManager(t, &mockStateProvider{})
		mod := &testModule{name: "mod1"}
		_ = m.Add(mod)

		err := m.StartAuthedAll(t.Context())
		assert.NoError(t, err)
	})

	t.Run("with_auth_modules_success", func(t *testing.T) {
		t.Parallel()
		m, _, authCtx := newTestManager(t, &mockStateProvider{})

		called := false
		mod := &testAuthModule{
			testModule: testModule{name: "mod1"},
			startAuthedFunc: func(ctx context.Context, actx module.AuthContext) error {
				called = true

				assert.Equal(t, authCtx, actx)

				return nil
			},
		}
		_ = m.Add(mod)

		err := m.StartAuthedAll(t.Context())
		assert.NoError(t, err)
		assert.True(t, called)
	})

	t.Run("with_auth_modules_failure", func(t *testing.T) {
		t.Parallel()
		m, _, _ := newTestManager(t, &mockStateProvider{})

		errFailed := errors.New("auth-fail")

		mod := &testAuthModule{
			testModule: testModule{name: "mod1"},
			startAuthedFunc: func(ctx context.Context, actx module.AuthContext) error {
				return errFailed
			},
		}
		_ = m.Add(mod)

		err := m.StartAuthedAll(t.Context())

		var moduleErr *modules.Error
		assert.ErrorAs(t, err, &moduleErr)
		assert.Equal(t, "start authed", moduleErr.Op)
		assert.Equal(t, "mod1", moduleErr.Module)
		assert.ErrorContains(t, moduleErr.Err, "auth-fail")
	})
}

func TestModuleAdapter_VariousStates_HandlesLifecycle(t *testing.T) {
	t.Parallel()

	t.Run("name", func(t *testing.T) {
		t.Parallel()

		mod := &testModule{name: "adapter-test"}
		adapter := &modules.ModuleAdapter{Mod: mod}
		assert.Equal(t, "adapter-test", adapter.Name())
	})

	t.Run("dependencies_non_dependent", func(t *testing.T) {
		t.Parallel()

		mod := &testModule{name: "non-dep"}
		adapter := &modules.ModuleAdapter{Mod: mod}
		assert.Nil(t, adapter.Dependencies())
	})

	t.Run("dependencies_dependent", func(t *testing.T) {
		t.Parallel()

		mod := &testDependentModule{
			testModule:   testModule{name: "dep"},
			dependencies: []string{"dep1", "dep2"},
		}
		adapter := &modules.ModuleAdapter{Mod: mod}
		assert.Equal(t, []string{"dep1", "dep2"}, adapter.Dependencies())
	})

	t.Run("init", func(t *testing.T) {
		t.Parallel()

		initCtx := steammock.NewInitContext()

		var calledWith module.InitContext

		mod := &testModule{
			name: "init-test",
			initFunc: func(ctx module.InitContext) error {
				calledWith = ctx
				return nil
			},
		}
		adapter := &modules.ModuleAdapter{Mod: mod, InitCtx: initCtx}
		err := adapter.Init(t.Context())
		assert.NoError(t, err)
		assert.Equal(t, initCtx, calledWith)
	})

	t.Run("start_and_stop_closer_module", func(t *testing.T) {
		t.Parallel()

		startCalled := false

		var startCtx context.Context

		mod := &testCloserModule{
			testModule: testModule{
				name: "start-stop-test",
				startFunc: func(ctx context.Context) error {
					startCalled = true
					startCtx = ctx
					return nil
				},
			},
			closeFunc: func() error {
				return errors.New("close-err")
			},
		}

		adapter := &modules.ModuleAdapter{Mod: mod}

		err := adapter.Start(t.Context())
		assert.NoError(t, err)
		assert.True(t, startCalled)
		assert.NotNil(t, startCtx)
		assert.NoError(t, startCtx.Err())

		err = adapter.Stop(t.Context())
		assert.ErrorContains(t, err, "close-err")
		assert.ErrorIs(t, startCtx.Err(), context.Canceled)
	})

	t.Run("stop_non_closer_module_and_no_cancel", func(t *testing.T) {
		t.Parallel()

		mod := &testModule{name: "non-closer"}
		adapter := &modules.ModuleAdapter{Mod: mod}

		err := adapter.Stop(t.Context())
		assert.NoError(t, err)
	})
}

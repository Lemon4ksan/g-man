// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package modules_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/lemon4ksan/g-man/pkg/steam/client/modules"
	"github.com/lemon4ksan/g-man/pkg/steam/id"
	"github.com/lemon4ksan/g-man/pkg/steam/module"
	steammock "github.com/lemon4ksan/g-man/test/mock"
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

func TestNew(t *testing.T) {
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

func TestManager_Get(t *testing.T) {
	initCtx := steammock.NewInitContext()
	authCtx := steammock.NewAuthContext(id.ID(12345))
	m := modules.New(&mockStateProvider{}, initCtx, authCtx)
	mod := &testModule{name: "mod1"}

	err := m.Add(mod)
	assert.NoError(t, err)

	assert.Equal(t, mod, m.Get("mod1"))
	assert.Nil(t, m.Get("non-existent"))
}

func TestManager_Add_And_All(t *testing.T) {
	initCtx := steammock.NewInitContext()
	authCtx := steammock.NewAuthContext(id.ID(12345))
	m := modules.New(&mockStateProvider{}, initCtx, authCtx)

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

func TestManager_Register(t *testing.T) {
	initCtx := steammock.NewInitContext()
	authCtx := steammock.NewAuthContext(id.ID(12345))

	t.Run("Already registered", func(t *testing.T) {
		m := modules.New(&mockStateProvider{}, initCtx, authCtx)
		mod := &testModule{name: "mod1"}

		err := m.Add(mod)
		assert.NoError(t, err)

		err = m.Register(t.Context(), mod)
		assert.ErrorIs(t, err, modules.ErrDuplicate)
	})

	t.Run("Not running and not authorized", func(t *testing.T) {
		sp := &mockStateProvider{running: false, authed: false}
		m := modules.New(sp, initCtx, authCtx)

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

	t.Run("Running and authorized", func(t *testing.T) {
		sp := &mockStateProvider{running: true, authed: true}
		m := modules.New(sp, initCtx, authCtx)

		var initCalledWith module.InitContext

		startCalled := false

		var startAuthedCalledWith module.AuthContext

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

	t.Run("Running but init fails", func(t *testing.T) {
		sp := &mockStateProvider{running: true, authed: false}
		m := modules.New(sp, initCtx, authCtx)

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

	t.Run("Running but start fails", func(t *testing.T) {
		sp := &mockStateProvider{running: true, authed: false}
		m := modules.New(sp, initCtx, authCtx)

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

	t.Run("Authorized but start authed fails", func(t *testing.T) {
		sp := &mockStateProvider{running: false, authed: true}
		m := modules.New(sp, initCtx, authCtx)

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

func TestManager_LifecycleAll(t *testing.T) {
	t.Run("Lazy init orchestrator on all", func(t *testing.T) {
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

	t.Run("Delegation and success", func(t *testing.T) {
		initCtx := steammock.NewInitContext()
		authCtx := steammock.NewAuthContext(id.ID(12345))
		m := modules.New(&mockStateProvider{}, initCtx, authCtx)

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

	t.Run("Error propagation", func(t *testing.T) {
		initCtx := steammock.NewInitContext()
		authCtx := steammock.NewAuthContext(id.ID(12345))

		mInit := modules.New(&mockStateProvider{}, initCtx, authCtx)
		mInit.Add(&testModule{
			name: "mod-fail",
			initFunc: func(ctx module.InitContext) error {
				return errors.New("init-fail")
			},
		})
		err := mInit.InitAll(t.Context())
		assert.Error(t, err)

		mStart := modules.New(&mockStateProvider{}, initCtx, authCtx)
		mStart.Add(&testModule{
			name: "mod-fail",
			startFunc: func(ctx context.Context) error {
				return errors.New("start-fail")
			},
		})
		err = mStart.InitAll(t.Context())
		assert.NoError(t, err)
		err = mStart.StartAll(t.Context())
		assert.Error(t, err)

		mStop := modules.New(&mockStateProvider{}, initCtx, authCtx)
		mStop.Add(&testCloserModule{
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

func TestManager_StartAuthedAll(t *testing.T) {
	initCtx := steammock.NewInitContext()
	authCtx := steammock.NewAuthContext(id.ID(12345))

	t.Run("No auth modules", func(t *testing.T) {
		m := modules.New(&mockStateProvider{}, initCtx, authCtx)
		mod := &testModule{name: "mod1"}
		m.Add(mod)

		err := m.StartAuthedAll(t.Context())
		assert.NoError(t, err)
	})

	t.Run("With auth modules - success", func(t *testing.T) {
		m := modules.New(&mockStateProvider{}, initCtx, authCtx)

		called := false
		mod := &testAuthModule{
			testModule: testModule{name: "mod1"},
			startAuthedFunc: func(ctx context.Context, actx module.AuthContext) error {
				called = true

				assert.Equal(t, authCtx, actx)

				return nil
			},
		}
		m.Add(mod)

		err := m.StartAuthedAll(t.Context())
		assert.NoError(t, err)
		assert.True(t, called)
	})

	t.Run("With auth modules - failure", func(t *testing.T) {
		m := modules.New(&mockStateProvider{}, initCtx, authCtx)

		errFailed := errors.New("auth-fail")

		mod := &testAuthModule{
			testModule: testModule{name: "mod1"},
			startAuthedFunc: func(ctx context.Context, actx module.AuthContext) error {
				return errFailed
			},
		}
		m.Add(mod)

		err := m.StartAuthedAll(t.Context())

		var moduleErr *modules.Error
		assert.ErrorAs(t, err, &moduleErr)
		assert.Equal(t, "start authed", moduleErr.Op)
		assert.Equal(t, "mod1", moduleErr.Module)
		assert.ErrorContains(t, moduleErr.Err, "auth-fail")
	})
}

func TestModuleAdapter(t *testing.T) {
	t.Run("Name", func(t *testing.T) {
		mod := &testModule{name: "adapter-test"}
		adapter := &modules.ModuleAdapter{Mod: mod}
		assert.Equal(t, "adapter-test", adapter.Name())
	})

	t.Run("Dependencies - non-dependent", func(t *testing.T) {
		mod := &testModule{name: "non-dep"}
		adapter := &modules.ModuleAdapter{Mod: mod}
		assert.Nil(t, adapter.Dependencies())
	})

	t.Run("Dependencies - dependent", func(t *testing.T) {
		mod := &testDependentModule{
			testModule:   testModule{name: "dep"},
			dependencies: []string{"dep1", "dep2"},
		}
		adapter := &modules.ModuleAdapter{Mod: mod}
		assert.Equal(t, []string{"dep1", "dep2"}, adapter.Dependencies())
	})

	t.Run("Init", func(t *testing.T) {
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

	t.Run("Start and Stop - closer module", func(t *testing.T) {
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

	t.Run("Stop - non-closer module and no cancel", func(t *testing.T) {
		mod := &testModule{name: "non-closer"}
		adapter := &modules.ModuleAdapter{Mod: mod}

		err := adapter.Stop(t.Context())
		assert.NoError(t, err)
	})
}

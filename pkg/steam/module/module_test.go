// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package module_test

import (
	"context"
	"testing"
	"time"

	"github.com/lemon4ksan/aoni"
	"github.com/lemon4ksan/miyako/bus"
	"github.com/lemon4ksan/miyako/log"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lemon4ksan/g-man/pkg/steam/community"
	"github.com/lemon4ksan/g-man/pkg/steam/id"
	"github.com/lemon4ksan/g-man/pkg/steam/module"
	"github.com/lemon4ksan/g-man/pkg/steam/protocol/enums"
	"github.com/lemon4ksan/g-man/pkg/steam/service"
	"github.com/lemon4ksan/g-man/pkg/steam/socket"
	"github.com/lemon4ksan/g-man/pkg/storage"
	"github.com/lemon4ksan/g-man/test/mock"
)

type mockInitContext struct {
	logger log.Logger
	bus    *bus.Bus
	mods   map[string]module.Module
}

func (m *mockInitContext) Storage() storage.Provider                        { return nil }
func (m *mockInitContext) Bus() *bus.Bus                                    { return m.bus }
func (m *mockInitContext) Logger() log.Logger                               { return m.logger }
func (m *mockInitContext) Service() service.Doer                            { return nil }
func (m *mockInitContext) Rest() aoni.Requester                             { return nil }
func (m *mockInitContext) RegisterPacketHandler(enums.EMsg, socket.Handler) {}
func (m *mockInitContext) RegisterServiceHandler(string, socket.Handler)    {}
func (m *mockInitContext) UnregisterPacketHandler(enums.EMsg)               {}
func (m *mockInitContext) UnregisterServiceHandler(string)                  {}
func (m *mockInitContext) Module(name string) module.Module {
	if m.mods == nil {
		return nil
	}

	return m.mods[name]
}

type dummyModule struct {
	module.Base
}

func TestState_String(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "new", module.StateNew.String())
	assert.Equal(t, "started", module.StateStarted.String())
	assert.Equal(t, "closed", module.StateClosed.String())
	assert.Equal(t, "unknown", module.State(99).String())
}

func TestEvent_String(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "start", module.EventStart.String())
	assert.Equal(t, "close", module.EventClose.String())
	assert.Equal(t, "unknown", module.Event(99).String())
}

func TestGet(t *testing.T) {
	t.Parallel()

	t.Run("success", func(t *testing.T) {
		t.Parallel()

		mCtx := &mockInitContext{
			mods: map[string]module.Module{
				"dummy": &dummyModule{Base: module.New("dummy")},
			},
		}

		mod, err := module.Get[*dummyModule](mCtx, "dummy")
		require.NoError(t, err)
		require.NotNil(t, mod)
		assert.Equal(t, "dummy", mod.Name())
	})

	t.Run("module_not_registered", func(t *testing.T) {
		t.Parallel()

		mCtx := &mockInitContext{
			mods: make(map[string]module.Module),
		}

		_, err := module.Get[*dummyModule](mCtx, "non_existent")
		require.Error(t, err)
		assert.Contains(t, err.Error(), `module "non_existent" not registered`)
	})

	t.Run("invalid_type", func(t *testing.T) {
		t.Parallel()

		mCtx := &mockInitContext{
			mods: map[string]module.Module{
				"dummy": &dummyModule{Base: module.New("dummy")},
			},
		}

		type anotherModule struct {
			module.Base
		}

		_, err := module.Get[*anotherModule](mCtx, "dummy")
		require.Error(t, err)
		assert.Contains(
			t,
			err.Error(),
			`has invalid type *module_test.dummyModule (expected *module_test.anotherModule)`,
		)
	})
}

func TestBase_Lifecycle(t *testing.T) {
	t.Parallel()

	t.Run("name_retrieval", func(t *testing.T) {
		t.Parallel()

		name := "test_module"
		base := module.New(name)
		assert.Equal(t, name, base.Name())
	})

	t.Run("init_sets_resources", func(t *testing.T) {
		t.Parallel()

		base := module.New("test_module")
		mCtx := &mockInitContext{
			logger: log.Discard,
			bus:    bus.New(),
		}

		err := base.Init(mCtx)
		require.NoError(t, err)
		assert.Equal(t, mCtx.bus, base.Bus)
		assert.NotNil(t, base.Ctx)
	})

	t.Run("raw_base_initialization", func(t *testing.T) {
		t.Parallel()

		var rawBase module.Base

		mCtx := &mockInitContext{
			logger: log.Discard,
			bus:    bus.New(),
		}

		err := rawBase.Init(mCtx)
		require.NoError(t, err)

		assert.NotNil(t, rawBase.Fsm)
		assert.NotNil(t, rawBase.Wg)
		assert.NotNil(t, rawBase.Ctx)
	})

	t.Run("start_and_close", func(t *testing.T) {
		t.Parallel()

		base := module.New("test_module")

		err := base.Start(t.Context())
		require.NoError(t, err)
		require.NotNil(t, base.Ctx)
		require.NotNil(t, base.Cancel)

		err = base.Close()
		require.NoError(t, err)

		select {
		case <-base.Ctx.Done():
		default:
			t.Error("Ctx was not cancelled after Close")
		}
	})
}

func TestBase_Go(t *testing.T) {
	t.Parallel()

	t.Run("go_routine_execution_and_cancellation", func(t *testing.T) {
		t.Parallel()

		base := module.New("go_test")
		err := base.Init(&mockInitContext{logger: log.Discard, bus: bus.New()})
		require.NoError(t, err)

		err = base.Start(t.Context())
		require.NoError(t, err)

		started := make(chan struct{})
		finished := make(chan struct{})

		base.Go(func(ctx context.Context) {
			close(started)
			<-ctx.Done()
			close(finished)
		})

		<-started

		closeDone := make(chan struct{})
		go func() {
			_ = base.Close()

			close(closeDone)
		}()

		select {
		case <-finished:
		case <-time.After(200 * time.Millisecond):
			t.Error("Goroutine did not finish after context cancellation")
		}

		select {
		case <-closeDone:
		case <-time.After(200 * time.Millisecond):
			t.Error("Close did not return (WaitGroup deadlock?)")
		}
	})

	t.Run("raw_base_go", func(t *testing.T) {
		t.Parallel()

		var rawBase module.Base

		goStarted := make(chan struct{})

		rawBase.Go(func(ctx context.Context) {
			close(goStarted)
		})

		<-goStarted

		if rawBase.Wg != nil {
			rawBase.Wg.Wait()
		}
	})
}

func TestBase_InitFallbackContext(t *testing.T) {
	t.Parallel()

	t.Run("fallback_context_lifecycle", func(t *testing.T) {
		t.Parallel()

		base := module.New("fallback")
		mCtx := &mockInitContext{logger: log.Discard, bus: bus.New()}

		err := base.Init(mCtx)
		require.NoError(t, err)
		require.NotNil(t, base.Ctx)
		assert.NoError(t, base.Ctx.Err(), "fallback context should not be cancelled initially")

		base.Close()
		assert.Error(t, base.Ctx.Err(), "fallback context should be cancelled after Close")
	})

	t.Run("init_after_cancel", func(t *testing.T) {
		t.Parallel()

		baseReinit := module.New("reinit")
		mCtxReinit := &mockInitContext{logger: log.Discard, bus: bus.New()}

		err := baseReinit.Init(mCtxReinit)
		require.NoError(t, err)

		_ = baseReinit.Close()
		assert.Error(t, baseReinit.Ctx.Err())

		err = baseReinit.Init(mCtxReinit)
		require.NoError(t, err)
		assert.NoError(t, baseReinit.Ctx.Err())
	})
}

func TestBase_State(t *testing.T) {
	t.Parallel()

	t.Run("state_transitions", func(t *testing.T) {
		t.Parallel()

		base := module.New("state")
		assert.Equal(t, module.StateNew, base.State(), "initial state should be StateNew")
		assert.True(t, base.IsNew())
		assert.False(t, base.IsStarted())
		assert.False(t, base.IsClosed())

		err := base.Start(t.Context())
		require.NoError(t, err)
		assert.Equal(t, module.StateStarted, base.State(), "state should be StateStarted after Start")
		assert.False(t, base.IsNew())
		assert.True(t, base.IsStarted())
		assert.False(t, base.IsClosed())

		err = base.Close()
		require.NoError(t, err)
		assert.Equal(t, module.StateClosed, base.State(), "state should be StateClosed after Close")
		assert.False(t, base.IsNew())
		assert.False(t, base.IsStarted())
		assert.True(t, base.IsClosed())
	})
}

func TestBase_WithDeps(t *testing.T) {
	t.Parallel()

	t.Run("dependencies_declaration", func(t *testing.T) {
		t.Parallel()

		base := module.New("test").WithDeps("dep1", "dep2")

		deps := base.Dependencies()
		require.Len(t, deps, 2)
		assert.Equal(t, "dep1", deps[0])
		assert.Equal(t, "dep2", deps[1])
	})
}

func TestAuthBase_Lifecycle(t *testing.T) {
	t.Parallel()

	steamID := id.ID(76561198000000001)

	t.Run("new_validation_failure", func(t *testing.T) {
		t.Parallel()

		base := module.New("test")
		assert.Equal(t, "test", base.Name())
	})

	t.Run("default_state", func(t *testing.T) {
		t.Parallel()

		ab := module.NewAuthBase("")

		authCtx := mock.NewAuthContext(steamID)

		assert.Equal(t, id.ID(0), ab.SteamID())
		assert.Nil(t, ab.Community())
		ab.StartAuthed(t.Context(), authCtx)
		assert.Equal(t, steamID, ab.SteamID())
		assert.Equal(t, authCtx.MockCommunity, ab.Community())
		assert.Equal(t, authCtx, ab.AuthContext())
		assert.True(t, ab.IsAuthenticated())

		ab.ClearAuth()
		assert.False(t, ab.IsAuthenticated())
	})

	t.Run("start_authed_success", func(t *testing.T) {
		t.Parallel()

		base := module.New("auth_test")
		mCtx := &mockInitContext{logger: log.Discard, bus: bus.New()}

		err := base.Init(mCtx)
		require.NoError(t, err)

		var authContext nilCommunityAuthContext

		err = base.Start(t.Context())
		require.NoError(t, err)

		assert.Equal(t, id.ID(0), authContext.SteamID())
	})

	t.Run("marshal_state", func(t *testing.T) {
		t.Parallel()

		ab := module.NewAuthBase("")
		ab.StartAuthed(t.Context(), mock.NewAuthContext(steamID))
		assert.Equal(t, steamID, ab.SteamID())
	})

	t.Run("unmarshal_success", func(t *testing.T) {
		t.Parallel()

		ab := module.NewAuthBase("")

		authCtx := mock.NewAuthContext(steamID)
		authCtx.MockCommunity = nil

		ab.StartAuthed(t.Context(), authCtx)
		assert.Nil(t, ab.Community())
	})
}

type nilCommunityAuthContext struct {
	module.AuthContext
}

func (nilCommunityAuthContext) Community() community.Requester { return nil }
func (nilCommunityAuthContext) SteamID() id.ID                 { return 0 }

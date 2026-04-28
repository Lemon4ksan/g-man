// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package module

import (
	"context"
	"testing"
	"time"

	"github.com/lemon4ksan/g-man/pkg/bus"
	"github.com/lemon4ksan/g-man/pkg/log"
	"github.com/lemon4ksan/g-man/pkg/rest"
	"github.com/lemon4ksan/g-man/pkg/steam/protocol/enums"
	"github.com/lemon4ksan/g-man/pkg/steam/service"
	"github.com/lemon4ksan/g-man/pkg/steam/socket"
	"github.com/lemon4ksan/g-man/pkg/storage"
)

type mockInitContext struct {
	logger log.Logger
	bus    *bus.Bus
}

func (m *mockInitContext) Storage() storage.Provider                        { return nil }
func (m *mockInitContext) Bus() *bus.Bus                                    { return m.bus }
func (m *mockInitContext) Logger() log.Logger                               { return m.logger }
func (m *mockInitContext) Service() service.Doer                            { return nil }
func (m *mockInitContext) Rest() rest.Requester                             { return nil }
func (m *mockInitContext) RegisterPacketHandler(enums.EMsg, socket.Handler) {}
func (m *mockInitContext) RegisterServiceHandler(string, socket.Handler)    {}
func (m *mockInitContext) Module(string) Module                             { return nil }
func (m *mockInitContext) UnregisterPacketHandler(enums.EMsg)               {}
func (m *mockInitContext) UnregisterServiceHandler(string)                  {}

func TestBase_Lifecycle(t *testing.T) {
	name := "test_module"
	base := New(name)

	if base.Name() != name {
		t.Errorf("expected name %s, got %s", name, base.Name())
	}

	t.Run("Init sets resources", func(t *testing.T) {
		mCtx := &mockInitContext{
			logger: log.Discard,
			bus:    bus.New(),
		}

		err := base.Init(mCtx)
		if err != nil {
			t.Fatalf("Init failed: %v", err)
		}

		if base.Bus != mCtx.bus {
			t.Error("Bus was not set during Init")
		}

		if base.Ctx == nil {
			t.Error("Ctx should be initialized during Init even if Start wasn't called")
		}
	})

	t.Run("Start and Close", func(t *testing.T) {
		ctx := context.Background()

		if err := base.Start(ctx); err != nil {
			t.Fatalf("Start failed: %v", err)
		}

		if base.Ctx == nil || base.Cancel == nil {
			t.Fatal("Start did not initialize Ctx or Cancel")
		}

		if err := base.Close(); err != nil {
			t.Errorf("Close returned error: %v", err)
		}

		select {
		case <-base.Ctx.Done():
		default:
			t.Error("Ctx was not cancelled after Close")
		}
	})
}

func TestBase_Go(t *testing.T) {
	base := New("go_test")
	_ = base.Init(&mockInitContext{logger: log.Discard, bus: bus.New()})
	_ = base.Start(context.Background())

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
}

func TestBase_InitFallbackContext(t *testing.T) {
	base := New("fallback")
	mCtx := &mockInitContext{logger: log.Discard, bus: bus.New()}

	err := base.Init(mCtx)
	if err != nil {
		t.Fatal(err)
	}

	if base.Ctx == nil {
		t.Fatal("expected fallback context to be created in Init")
	}

	if base.Ctx.Err() != nil {
		t.Error("fallback context should not be cancelled initially")
	}

	base.Close()

	if base.Ctx.Err() == nil {
		t.Error("fallback context should be cancelled after Close")
	}
}

func TestBase_State(t *testing.T) {
	base := New("state")
	if base.State.Load() != 0 {
		t.Error("initial state should be 0")
	}

	base.State.Store(1)

	if base.State.Load() != 1 {
		t.Error("state was not updated")
	}
}

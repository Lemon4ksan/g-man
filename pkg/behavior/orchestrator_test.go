// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package behavior

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/lemon4ksan/miyako/bus"
	"github.com/lemon4ksan/miyako/log"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lemon4ksan/g-man/pkg/steam/module"
	"github.com/lemon4ksan/g-man/pkg/steam/protocol/enums"
	"github.com/lemon4ksan/g-man/pkg/steam/socket"
)

type mockBehavior struct {
	name      string
	runCalled chan struct{}
	runFunc   func(ctx context.Context) error
}

func (m *mockBehavior) Name() string {
	return m.name
}

func (m *mockBehavior) Run(ctx context.Context) error {
	if m.runCalled != nil {
		select {
		case m.runCalled <- struct{}{}:
		default:
		}
	}

	if m.runFunc != nil {
		return m.runFunc(ctx)
	}

	<-ctx.Done()

	return nil
}

func TestNewOrchestrator(t *testing.T) {
	t.Parallel()

	bBus := bus.New()
	logger := log.Discard
	o := NewOrchestrator(bBus, logger)
	require.NotNil(t, o)

	t.Run("getters", func(t *testing.T) {
		t.Parallel()
		assert.Equal(t, bBus, o.Bus())
		assert.Equal(t, logger, o.Logger())
	})
}

func TestOrchestrator_Register(t *testing.T) {
	t.Parallel()

	o := NewOrchestrator(bus.New(), log.Discard)
	b := &mockBehavior{name: "test"}
	o.Register(b)

	assert.Equal(t, 1, o.Count())
}

func TestOrchestrator_StartStop(t *testing.T) {
	t.Parallel()

	o := NewOrchestrator(bus.New(), log.Discard)
	b1Called := make(chan struct{}, 1)
	b2Called := make(chan struct{}, 1)
	b1 := &mockBehavior{name: "b1", runCalled: b1Called}
	b2 := &mockBehavior{name: "b2", runCalled: b2Called}

	o.Register(b1)
	o.Register(b2)

	err := o.Start(t.Context())
	require.NoError(t, err)

	select {
	case <-b1Called:
	case <-time.After(1 * time.Second):
		t.Fatal("timeout waiting for b1 to start")
	}

	select {
	case <-b2Called:
	case <-time.After(1 * time.Second):
		t.Fatal("timeout waiting for b2 to start")
	}

	o.Stop()
}

func TestOrchestrator_StartAlreadyRunning(t *testing.T) {
	t.Parallel()

	o := NewOrchestrator(bus.New(), log.Discard)
	err := o.Start(t.Context())
	require.NoError(t, err)

	err = o.Start(t.Context())
	assert.Error(t, err, "expected error when starting already running orchestrator")

	o.Stop()
}

func TestOrchestrator_StopNotRunning(t *testing.T) {
	t.Parallel()

	o := NewOrchestrator(bus.New(), log.Discard)
	assert.NotPanics(t, func() { o.Stop() })
}

func TestOrchestrator_BehaviorError(t *testing.T) {
	t.Parallel()

	o := NewOrchestrator(bus.New(), log.Discard)
	bCalled := make(chan struct{}, 1)
	b := &mockBehavior{
		name:      "failing",
		runCalled: bCalled,
		runFunc: func(ctx context.Context) error {
			return errors.New("behavior failed")
		},
	}
	o.Register(b)

	err := o.Start(t.Context())
	require.NoError(t, err)

	select {
	case <-bCalled:
	case <-time.After(1 * time.Second):
		t.Fatal("timeout waiting for failing behavior to run")
	}

	o.Stop()
}

func TestOrchestrator_BehaviorStop(t *testing.T) {
	t.Parallel()

	o := NewOrchestrator(bus.New(), log.Discard)
	stopped := make(chan struct{})
	b := &mockBehavior{
		name: "stopping",
		runFunc: func(ctx context.Context) error {
			<-ctx.Done()
			close(stopped)
			return nil
		},
	}
	o.Register(b)

	err := o.Start(t.Context())
	require.NoError(t, err)

	o.Stop()

	select {
	case <-stopped:
	case <-time.After(100 * time.Millisecond):
		t.Fatal("behavior did not stop in time")
	}
}

type mockInitContext struct {
	module.InitContext
	bBus   *bus.Bus
	logger log.Logger
}

func (m *mockInitContext) Bus() *bus.Bus                                                 { return m.bBus }
func (m *mockInitContext) Logger() log.Logger                                            { return m.logger }
func (m *mockInitContext) RegisterPacketHandler(eMsg enums.EMsg, handler socket.Handler) {}
func (m *mockInitContext) RegisterServiceHandler(method string, handler socket.Handler)  {}

func TestOrchestrator_ModuleInterface(t *testing.T) {
	t.Parallel()

	o := NewModule()
	assert.Equal(t, "behavior", o.Name())

	mCtx := &mockInitContext{
		bBus:   bus.New(),
		logger: log.Discard,
	}

	err := o.Init(mCtx)
	require.NoError(t, err)
	assert.Equal(t, mCtx.bBus, o.Bus())

	err = o.Start(t.Context())
	require.NoError(t, err)

	err = o.Close()
	require.NoError(t, err)
}

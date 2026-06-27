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
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lemon4ksan/g-man/pkg/log"
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

type spyLogger struct {
	log.Logger
	infoMsg     string
	infoFields  []log.Field
	warnMsg     string
	warnFields  []log.Field
	errorMsg    string
	errorFields []log.Field
}

func (s *spyLogger) Info(msg string, fields ...log.Field) {
	s.infoMsg = msg
	s.infoFields = fields
}

func (s *spyLogger) Warn(msg string, fields ...log.Field) {
	s.warnMsg = msg
	s.warnFields = fields
}

func (s *spyLogger) Error(msg string, fields ...log.Field) {
	s.errorMsg = msg
	s.errorFields = fields
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

func TestLogAdapter(t *testing.T) {
	t.Parallel()

	t.Run("log_adapter_formatting", func(t *testing.T) {
		t.Parallel()

		spy := &spyLogger{}
		adapter := &logAdapter{l: spy}

		adapter.Info("test_info", "key1", "val1", "key2", 42)
		assert.Equal(t, "test_info", spy.infoMsg)
		require.Len(t, spy.infoFields, 2)
		assert.Equal(t, log.Any("key1", "val1"), spy.infoFields[0])
		assert.Equal(t, log.Any("key2", 42), spy.infoFields[1])

		adapter.Warn("test_warn", 123, "not_ok_key", "key3", "val3")
		assert.Equal(t, "test_warn", spy.warnMsg)
		require.Len(t, spy.warnFields, 1)
		assert.Equal(t, log.Any("key3", "val3"), spy.warnFields[0])

		adapter.Error("test_error", "key_without_value")
		assert.Equal(t, "test_error", spy.errorMsg)
		assert.Empty(t, spy.errorFields)
	})
}

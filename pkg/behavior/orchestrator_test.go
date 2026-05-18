// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package behavior

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/lemon4ksan/g-man/pkg/log"
)

type mockBehavior struct {
	name      string
	runCalled bool
	runFunc   func(ctx context.Context) error
	mu        sync.Mutex
}

func (m *mockBehavior) Name() string {
	return m.name
}

func (m *mockBehavior) Run(ctx context.Context) error {
	m.mu.Lock()
	m.runCalled = true
	m.mu.Unlock()

	if m.runFunc != nil {
		return m.runFunc(ctx)
	}

	<-ctx.Done()

	return nil
}

func TestNewOrchestrator(t *testing.T) {
	o := NewOrchestrator(log.Discard)
	if o == nil {
		t.Fatal("expected orchestrator to be created")
	}

	if o.Logger() == nil {
		t.Error("expected logger to be set")
	}
}

func TestOrchestrator_Register(t *testing.T) {
	o := NewOrchestrator(log.Discard)
	b := &mockBehavior{name: "test"}
	o.Register(b)

	if len(o.behaviors) != 1 {
		t.Errorf("expected 1 behavior, got %d", len(o.behaviors))
	}

	if o.behaviors[0] != b {
		t.Error("registered behavior mismatch")
	}
}

func TestOrchestrator_StartStop(t *testing.T) {
	o := NewOrchestrator(log.Discard)
	b1 := &mockBehavior{name: "b1"}
	b2 := &mockBehavior{name: "b2"}

	o.Register(b1)
	o.Register(b2)

	ctx := context.Background()
	if err := o.Start(ctx); err != nil {
		t.Fatalf("failed to start orchestrator: %v", err)
	}

	if !o.running {
		t.Error("orchestrator should be running")
	}

	// Wait a bit for goroutines to start
	time.Sleep(10 * time.Millisecond)

	b1.mu.Lock()
	b1Called := b1.runCalled
	b1.mu.Unlock()
	b2.mu.Lock()
	b2Called := b2.runCalled
	b2.mu.Unlock()

	if !b1Called || !b2Called {
		t.Error("expected all behaviors to be started")
	}

	o.Stop()

	if o.running {
		t.Error("orchestrator should not be running")
	}
}

func TestOrchestrator_StartAlreadyRunning(t *testing.T) {
	o := NewOrchestrator(log.Discard)
	if err := o.Start(context.Background()); err != nil {
		t.Fatalf("failed to start: %v", err)
	}

	if err := o.Start(context.Background()); err == nil {
		t.Error("expected error when starting already running orchestrator")
	}
}

func TestOrchestrator_StopNotRunning(t *testing.T) {
	o := NewOrchestrator(log.Discard)
	// Should not panic or error
	o.Stop()
}

func TestOrchestrator_BehaviorError(t *testing.T) {
	o := NewOrchestrator(log.Discard)
	b := &mockBehavior{
		name: "failing",
		runFunc: func(ctx context.Context) error {
			return errors.New("behavior failed")
		},
	}
	o.Register(b)

	if err := o.Start(context.Background()); err != nil {
		t.Fatalf("failed to start: %v", err)
	}

	// Give it time to run and fail
	time.Sleep(10 * time.Millisecond)

	o.Stop()
}

func TestOrchestrator_BehaviorStop(t *testing.T) {
	o := NewOrchestrator(log.Discard)
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

	if err := o.Start(context.Background()); err != nil {
		t.Fatalf("failed to start: %v", err)
	}

	o.Stop()

	select {
	case <-stopped:
		// Success
	case <-time.After(100 * time.Millisecond):
		t.Error("behavior did not stop in time")
	}
}

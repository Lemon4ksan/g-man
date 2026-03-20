// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package guard

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/lemon4ksan/g-man/pkg/modules/auth"
	"github.com/lemon4ksan/g-man/test"
)

type mockConfService struct {
	getConfFunc func() (*ConfirmationsList, error)
	respondChan chan confResponseCall
}

type confResponseCall struct {
	ID     uint64
	Accept bool
	Key    string
}

func newMockConfService() *mockConfService {
	return &mockConfService{
		respondChan: make(chan confResponseCall, 10),
	}
}

func (m *mockConfService) GetConfirmations(ctx context.Context, deviceID string, steamID uint64, confKey string, timestamp int64) (*ConfirmationsList, error) {
	if m.getConfFunc != nil {
		return m.getConfFunc()
	}
	return &ConfirmationsList{Success: true, Confirmations: []*Confirmation{}}, nil
}

func (m *mockConfService) RespondToConfirmation(ctx context.Context, conf *Confirmation, accept bool, deviceID string, steamID uint64, confKey string, timestamp int64) error {
	m.respondChan <- confResponseCall{
		ID:     conf.ID,
		Accept: accept,
		Key:    confKey,
	}
	return nil
}

func validConfig() Config {
	cfg := DefaultConfig()
	cfg.IdentitySecret = "dGhpcyBpcyBhIHZhbGlkIGJhc2U2NCBrZXk="
	cfg.DeviceID = "android:12345"
	cfg.PollInterval = 10 * time.Millisecond
	cfg.MaxBackoff = 50 * time.Millisecond
	cfg.RateLimit = 1 * time.Millisecond
	return cfg
}

func setupGuard(t *testing.T, cfg Config) (*Guardian, *test.MockInitContext, *mockConfService) {
	t.Helper()
	g, err := New(cfg)
	if err != nil {
		t.Fatalf("failed to create Guardian: %v", err)
	}

	ictx := test.NewMockInitContext()

	if err := g.Init(ictx); err != nil {
		t.Fatalf("failed to init Guardian: %v", err)
	}

	mSvc := newMockConfService()

	g.mu.Lock()
	g.service = mSvc
	g.mu.Unlock()

	t.Cleanup(func() {
		_ = g.Close()
	})

	return g, ictx, mSvc
}

func TestGuardian_PollingLifecycle(t *testing.T) {
	g, ictx, mSvc := setupGuard(t, validConfig())
	sub := ictx.Bus().Subscribe(&ConfirmationReceivedEvent{})

	mSvc.getConfFunc = func() (*ConfirmationsList, error) {
		return &ConfirmationsList{
			Success: true,
			Confirmations: []*Confirmation{
				{ID: 101, Type: ConfTypeTrade, Title: "Trade #1"},
			},
		}, nil
	}

	if err := g.StartPolling(); err != nil {
		t.Fatalf("StartPolling failed: %v", err)
	}

	select {
	case ev := <-sub.C():
		confEv := ev.(*ConfirmationReceivedEvent)
		if confEv.Confirmation.ID != 101 {
			t.Errorf("expected conf ID 101, got %d", confEv.Confirmation.ID)
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("timed out waiting for confirmation")
	}

	g.StopPolling()
	if g.State.Load() != StateStopped {
		t.Errorf("expected state Stopped, got %d", g.State.Load())
	}

	mSvc.getConfFunc = func() (*ConfirmationsList, error) {
		t.Error("polling loop should have been stopped, but FetchConfirmations was called")
		return nil, errors.New("stopped")
	}

	time.Sleep(50 * time.Millisecond)
}

func TestGuardian_RestartIdempotency(t *testing.T) {
	g, _, _ := setupGuard(t, validConfig())

	for i := 0; i < 3; i++ {
		_ = g.StartPolling()
		time.Sleep(10 * time.Millisecond)
	}

	g.StopPolling()

	if g.State.Load() != StateStopped {
		t.Error("failed to stop polling cleanly")
	}
}

func TestGuardian_AutoAccept(t *testing.T) {
	cfg := validConfig()
	cfg.AutoAccept = true
	cfg.AutoAcceptTypes = []ConfirmationType{ConfTypeTrade}

	g, _, mSvc := setupGuard(t, cfg)

	mSvc.getConfFunc = func() (*ConfirmationsList, error) {
		return &ConfirmationsList{
			Success: true,
			Confirmations: []*Confirmation{
				{ID: 201, Type: ConfTypeTrade, Title: "Auto-Accept Me"},
			},
		}, nil
	}

	_ = g.StartPolling()

	select {
	case call := <-mSvc.respondChan:
		if call.ID != 201 || !call.Accept {
			t.Errorf("unexpected auto-accept call: %+v", call)
		}
	case <-time.After(200 * time.Millisecond):
		t.Fatal("timed out waiting for auto-accept")
	}
}

func TestGuardian_HandleStateChange(t *testing.T) {
	g, _, _ := setupGuard(t, validConfig())

	_ = g.StartPolling()

	g.handleStateChange(&auth.StateEvent{New: auth.StateDisconnected})

	time.Sleep(50 * time.Millisecond)

	if g.State.Load() != StateStopped {
		t.Errorf("expected state Stopped after disconnect, got %d", g.State.Load())
	}
}

func TestGuardian_Close(t *testing.T) {
	g, _, _ := setupGuard(t, validConfig())
	_ = g.StartPolling()

	if err := g.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}

	if g.State.Load() != StateClosed {
		t.Error("State should be Closed")
	}

	select {
	case <-g.Ctx.Done():
		// OK
	default:
		t.Error("BaseModule context was not canceled after Close")
	}
}

// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package processor

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/lemon4ksan/g-man/pkg/trading"
	"github.com/lemon4ksan/g-man/pkg/trading/web/offer"
)

type backpackMock struct {
	mu          sync.Mutex
	lockedItems map[uint64]bool
	lockCalls   int
	unlockCalls int
}

func newBackpackMock() *backpackMock {
	return &backpackMock{lockedItems: make(map[uint64]bool)}
}

func (m *backpackMock) LockItems(ids []uint64) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.lockCalls++
	for _, id := range ids {
		m.lockedItems[id] = true
	}
}

func (m *backpackMock) UnlockItems(ids []uint64) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.unlockCalls++
	for _, id := range ids {
		delete(m.lockedItems, id)
	}
}

type mockManager struct {
	mu           sync.Mutex
	acceptCalls  int
	declineCalls int
	sendCalls    int
	lastParams   trading.OfferParams
	shouldFail   bool
}

func (m *mockManager) AcceptOffer(ctx context.Context, id uint64) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.shouldFail {
		return errors.New("steam error")
	}

	m.acceptCalls++

	return nil
}

func (m *mockManager) DeclineOffer(ctx context.Context, id uint64) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.declineCalls++

	return nil
}

func (m *mockManager) SendOffer(ctx context.Context, p trading.OfferParams) (uint64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.sendCalls++
	m.lastParams = p

	return 444, nil
}

func (m *mockManager) GetEscrowDuration(ctx context.Context, id uint64) (Details, error) {
	return Details{}, nil
}

type mockOfferHandler struct {
	mu            sync.Mutex
	processCalls  int
	decision      offer.ActionDecision
	failedCalled  bool
	sleepDuration time.Duration
}

func (h *mockOfferHandler) ProcessOffer(ctx context.Context, off *offer.TradeOffer) (offer.ActionDecision, error) {
	h.mu.Lock()
	h.processCalls++
	h.mu.Unlock()

	if h.sleepDuration > 0 {
		time.Sleep(h.sleepDuration)
	}

	return h.decision, nil
}

func (h *mockOfferHandler) OnActionFailed(
	ctx context.Context,
	off *offer.TradeOffer,
	act offer.ActionType,
	reason string,
	err error,
) {
	h.mu.Lock()
	defer h.mu.Unlock()

	h.failedCalled = true
}

func TestProcessor_DuplicatePrevention(t *testing.T) {
	mockMgr := &mockManager{}
	mockHdl := &mockOfferHandler{sleepDuration: 50 * time.Millisecond}
	mockBp := newBackpackMock()

	p := NewProcessor(mockMgr, mockBp, mockHdl, WithLogger(nil))
	p.Start(context.Background())

	off := &offer.TradeOffer{ID: 111}

	p.Enqueue(off)
	p.Enqueue(off)

	waitForCondition(func() bool {
		mockHdl.mu.Lock()
		defer mockHdl.mu.Unlock()
		return mockHdl.processCalls > 0
	}, 1*time.Second)

	// Ждем завершения
	time.Sleep(100 * time.Millisecond)

	if mockHdl.processCalls != 1 {
		t.Errorf("expected offer to be processed only once, got %d calls", mockHdl.processCalls)
	}
}

func TestProcessor_SequentialProcessing(t *testing.T) {
	mockMgr := &mockManager{}
	mockHdl := &mockOfferHandler{
		sleepDuration: 100 * time.Millisecond,
		decision:      offer.ActionDecision{Action: offer.ActionSkip},
	}
	mockBp := newBackpackMock()

	p := NewProcessor(mockMgr, mockBp, mockHdl, WithLogger(nil))
	p.Start(context.Background())

	p.Enqueue(&offer.TradeOffer{ID: 1})
	p.Enqueue(&offer.TradeOffer{ID: 2})

	time.Sleep(20 * time.Millisecond)

	mockHdl.mu.Lock()
	if mockHdl.processCalls != 1 {
		t.Errorf("expected only 1 offer in processing due to sleep, got %d", mockHdl.processCalls)
	}

	mockHdl.mu.Unlock()

	waitForCondition(func() bool {
		mockHdl.mu.Lock()
		defer mockHdl.mu.Unlock()
		return mockHdl.processCalls == 2
	}, 1*time.Second)
}

func TestProcessor_LockUnlockLifecycle(t *testing.T) {
	tests := []struct {
		name          string
		action        offer.ActionType
		expectUnlock  bool
		expectManager bool
	}{
		{"Accept: no unlock (GC handles it)", offer.ActionAccept, false, true},
		{"Decline: must unlock", offer.ActionDecline, true, true},
		{"Skip: must unlock", offer.ActionSkip, true, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockMgr := &mockManager{}
			mockHdl := &mockOfferHandler{decision: offer.ActionDecision{Action: tt.action}}
			mockBp := newBackpackMock()

			p := NewProcessor(mockMgr, mockBp, mockHdl, WithLogger(nil))
			p.Start(context.Background())

			off := &offer.TradeOffer{
				ID:          999,
				ItemsToGive: []*trading.Item{{AssetID: 1}},
			}

			p.Enqueue(off)

			waitForCondition(func() bool {
				mockHdl.mu.Lock()
				defer mockHdl.mu.Unlock()
				return mockHdl.processCalls == 1
			}, 1*time.Second)

			time.Sleep(10 * time.Millisecond)

			if mockBp.lockCalls != 1 {
				t.Error("expected LockItems to be called")
			}

			if tt.expectUnlock && mockBp.unlockCalls != 1 {
				t.Error("expected UnlockItems to be called")
			}

			if !tt.expectUnlock && mockBp.unlockCalls != 0 {
				t.Error("expected UnlockItems NOT to be called")
			}
		})
	}
}

func TestProcessor_CounterAction(t *testing.T) {
	mockMgr := &mockManager{}
	mockBp := newBackpackMock()

	counterParams := &offer.CounterParams{
		Message:     "Balance please",
		Token:       "xyz",
		ItemsToGive: []*trading.Item{{AssetID: 1}},
	}

	mockHdl := &mockOfferHandler{
		decision: offer.ActionDecision{
			Action:        offer.ActionCounter,
			CounterParams: counterParams,
		},
	}

	p := NewProcessor(mockMgr, mockBp, mockHdl, WithLogger(nil))
	p.Start(context.Background())

	p.Enqueue(&offer.TradeOffer{ID: 555, OtherSteamID: 12345})

	waitForCondition(func() bool {
		mockMgr.mu.Lock()
		defer mockMgr.mu.Unlock()
		return mockMgr.sendCalls == 1
	}, 1*time.Second)

	if mockMgr.lastParams.CounteredID != 555 {
		t.Errorf("expected CounteredID to be 555, got %d", mockMgr.lastParams.CounteredID)
	}

	if mockMgr.lastParams.Token != "xyz" {
		t.Errorf("expected Token to be xyz, got %s", mockMgr.lastParams.Token)
	}
}

func TestProcessor_RetryAndFailure(t *testing.T) {
	mockMgr := &mockManager{shouldFail: true}
	mockHdl := &mockOfferHandler{decision: offer.ActionDecision{Action: offer.ActionAccept}}
	mockBp := newBackpackMock()

	p := NewProcessor(mockMgr, mockBp, mockHdl, WithLogger(nil))

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	p.Start(ctx)

	p.Enqueue(&offer.TradeOffer{ID: 777})

	waitForCondition(func() bool {
		mockHdl.mu.Lock()
		defer mockHdl.mu.Unlock()
		return mockHdl.failedCalled
	}, 1*time.Second)

	if !mockHdl.failedCalled {
		t.Error("expected OnActionFailed to be called after manager error")
	}
}

func waitForCondition(condition func() bool, timeout time.Duration) bool {
	start := time.Now()
	for time.Since(start) < timeout {
		if condition() {
			return true
		}

		time.Sleep(10 * time.Millisecond)
	}

	return false
}

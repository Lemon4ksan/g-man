// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package processor

import (
	"context"
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

func (m *backpackMock) GetLockedAssetIDs() []uint64 { return nil }

type mockManager struct {
	acceptCalls  int
	declineCalls int
	mu           sync.Mutex
}

func (m *mockManager) AcceptOffer(ctx context.Context, id uint64) error {
	m.mu.Lock()
	defer m.mu.Unlock()

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
	panic("unimplemented")
}

func (m *mockManager) GetEscrowDuration(ctx context.Context, id uint64) (Details, error) {
	return Details{}, nil
}

type mockOfferHandler struct {
	decision     offer.ActionDecision
	failedCalled bool
	failedAction offer.ActionType
	mu           sync.Mutex
}

func (h *mockOfferHandler) ProcessOffer(ctx context.Context, off *offer.TradeOffer) (offer.ActionDecision, error) {
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
	h.failedAction = act
}

func TestProcessor_EnqueueAndWorker(t *testing.T) {
	mockMgr := &mockManager{}
	mockHdl := &mockOfferHandler{}
	mockBp := newBackpackMock()

	p := NewProcessor(mockMgr, mockBp, mockHdl)
	p.Start(context.Background())

	off := &offer.TradeOffer{
		ID: 123,
		ItemsToGive: []*trading.Item{
			{AssetID: 100},
			{AssetID: 200},
		},
	}

	mockHdl.decision = offer.ActionDecision{Action: offer.ActionAccept}

	p.Enqueue(off)

	if mockBp.lockCalls != 1 {
		t.Errorf("expected LockItems to be called once, got %d", mockBp.lockCalls)
	}

	waitForCondition(func() bool {
		mockMgr.mu.Lock()
		defer mockMgr.mu.Unlock()
		return mockMgr.acceptCalls == 1
	}, 1*time.Second)

	if mockBp.unlockCalls != 1 {
		t.Errorf("expected UnlockItems to be called after processing, got %d", mockBp.unlockCalls)
	}
}

func TestProcessor_CounterFallback(t *testing.T) {
	mockMgr := &mockManager{}
	mockHdl := &mockOfferHandler{}
	mockBp := newBackpackMock()

	mockHdl.decision = offer.ActionDecision{Action: offer.ActionCounter}

	p := NewProcessor(mockMgr, mockBp, mockHdl)
	p.Start(context.Background())

	p.Enqueue(&offer.TradeOffer{ID: 456, ItemsToGive: []*trading.Item{{AssetID: 500}}})

	waitForCondition(func() bool {
		mockMgr.mu.Lock()
		defer mockMgr.mu.Unlock()
		return mockMgr.declineCalls == 1
	}, 1*time.Second)

	if mockMgr.declineCalls != 1 {
		t.Errorf("expected counter fallback to call DeclineOffer, got %d calls", mockMgr.declineCalls)
	}

	if !mockHdl.failedCalled {
		t.Error("expected OnActionFailed to be called for the initial counter failure")
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

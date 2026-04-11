// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package web

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/lemon4ksan/g-man/pkg/log"
	"github.com/lemon4ksan/g-man/pkg/trading"
)

type mockManager struct {
	mu                 sync.Mutex
	acceptCalls        int
	declineCalls       int
	escrowCalls        int
	acceptShouldError  bool
	declineShouldError bool
	escrowShouldError  bool
	escrowDetails      EscrowDetails
}

func (m *mockManager) AcceptOffer(ctx context.Context, offerID uint64) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.acceptCalls++
	if m.acceptShouldError {
		return errors.New("mock accept error")
	}

	return nil
}

func (m *mockManager) DeclineOffer(ctx context.Context, offerID uint64) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.declineCalls++
	if m.declineShouldError {
		return errors.New("mock decline error")
	}

	return nil
}

func (m *mockManager) GetEscrowDuration(ctx context.Context, offerID uint64) (EscrowDetails, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.escrowCalls++
	if m.escrowShouldError {
		return EscrowDetails{}, errors.New("mock escrow error")
	}

	return m.escrowDetails, nil
}

func (m *mockManager) GetAcceptCalls() int {
	m.mu.Lock()
	defer m.mu.Unlock()

	return m.acceptCalls
}

func (m *mockManager) GetDeclineCalls() int {
	m.mu.Lock()
	defer m.mu.Unlock()

	return m.declineCalls
}

func (m *mockManager) ResetCalls() {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.acceptCalls = 0
	m.declineCalls = 0
}

type mockOfferHandler struct {
	mu                   sync.Mutex
	decision             ActionDecision
	processErr           error
	failedAction         ActionType
	failedReason         string
	failedOfferID        uint64
	onActionFailedCalled bool
}

func (h *mockOfferHandler) ProcessOffer(ctx context.Context, offer *TradeOffer) (ActionDecision, error) {
	h.mu.Lock()
	defer h.mu.Unlock()

	return h.decision, h.processErr
}

func (h *mockOfferHandler) OnActionFailed(
	ctx context.Context,
	offer *TradeOffer,
	action ActionType,
	reason string,
	err error,
) {
	h.mu.Lock()
	defer h.mu.Unlock()

	h.onActionFailedCalled = true
	h.failedAction = action
	h.failedReason = reason
	h.failedOfferID = offer.ID
}

func (h *mockOfferHandler) SetDecision(d ActionDecision) {
	h.mu.Lock()
	defer h.mu.Unlock()

	h.decision = d
}

func (h *mockOfferHandler) WasFailedCalled() bool {
	h.mu.Lock()
	defer h.mu.Unlock()

	return h.onActionFailedCalled
}

func (h *mockOfferHandler) GetFailedAction() ActionType {
	h.mu.Lock()
	defer h.mu.Unlock()

	return h.failedAction
}

func TestProcessor_EnqueueAndWorker(t *testing.T) {
	mockMgr := &mockManager{}
	mockHdl := &mockOfferHandler{}

	p := NewProcessor(mockMgr, mockHdl, log.Discard)
	p.Start(t.Context())

	offer := &TradeOffer{
		ID: 123,
		ItemsToGive: []*trading.Item{
			{AssetID: 100},
			{AssetID: 200},
		},
	}

	mockHdl.SetDecision(ActionDecision{Action: ActionAccept})
	p.Enqueue(offer)

	waitForCondition(func() bool { return mockMgr.GetAcceptCalls() == 1 }, 1*time.Second)

	if mockMgr.GetAcceptCalls() != 1 {
		t.Errorf("expected AcceptOffer to be called 1 time, got %d", mockMgr.GetAcceptCalls())
	}

	mockMgr.ResetCalls()
	mockHdl.SetDecision(ActionDecision{Action: ActionDecline})
	p.Enqueue(offer)

	waitForCondition(func() bool { return mockMgr.GetDeclineCalls() == 1 }, 1*time.Second)

	if mockMgr.GetDeclineCalls() != 1 {
		t.Errorf("expected DeclineOffer to be called 1 time, got %d", mockMgr.GetDeclineCalls())
	}

	if p.IsInTrade(100) || p.IsInTrade(200) {
		t.Error("expected items to be unset from trade after decline")
	}
}

func TestProcessor_CounterFallback(t *testing.T) {
	mockMgr := &mockManager{}
	mockHdl := &mockOfferHandler{}
	mockHdl.SetDecision(ActionDecision{Action: ActionCounter})

	p := NewProcessor(mockMgr, mockHdl, log.Discard)
	p.Start(t.Context())

	p.Enqueue(&TradeOffer{ID: 456})

	waitForCondition(func() bool { return mockMgr.GetDeclineCalls() == 1 }, 1*time.Second)

	if mockMgr.GetDeclineCalls() != 1 {
		t.Errorf("expected counter fallback to call DeclineOffer, got %d calls", mockMgr.GetDeclineCalls())
	}

	if !mockHdl.WasFailedCalled() {
		t.Error("expected OnActionFailed to be called for the initial counter failure")
	}

	if mockHdl.GetFailedAction() != ActionCounter {
		t.Errorf("expected failed action to be ActionCounter, got %s", mockHdl.GetFailedAction())
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

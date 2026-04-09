// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package trading

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

func (h *mockOfferHandler) OnActionFailed(ctx context.Context, offer *TradeOffer, action ActionType, reason string, err error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.onActionFailedCalled = true
	h.failedAction = action
	h.failedReason = reason
	h.failedOfferID = offer.ID
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

	mockHdl.decision = ActionDecision{Action: ActionAccept}
	p.Enqueue(offer)

	time.Sleep(50 * time.Millisecond)

	if mockMgr.acceptCalls != 1 {
		t.Errorf("expected AcceptOffer to be called 1 time, got %d", mockMgr.acceptCalls)
	}
	if !p.IsInTrade(100) || !p.IsInTrade(200) {
		t.Error("expected items to remain in trade after accept")
	}

	mockMgr.acceptCalls = 0
	mockHdl.decision = ActionDecision{Action: ActionDecline}
	p.Enqueue(offer)

	time.Sleep(50 * time.Millisecond)

	if mockMgr.declineCalls != 1 {
		t.Errorf("expected DeclineOffer to be called 1 time, got %d", mockMgr.declineCalls)
	}
	if p.IsInTrade(100) || p.IsInTrade(200) {
		t.Error("expected items to be unset from trade after decline")
	}

	mockMgr.declineCalls = 0
	p.SetItemInTrade(100)
	mockHdl.decision = ActionDecision{Action: ActionSkip}
	p.Enqueue(offer)

	time.Sleep(50 * time.Millisecond)

	if mockMgr.declineCalls != 0 {
		t.Error("DeclineOffer should not be called on skip")
	}
	if p.IsInTrade(100) {
		t.Error("expected item to be unset from trade after skip")
	}
}

func TestProcessor_CounterFallback(t *testing.T) {
	mockMgr := &mockManager{}
	mockHdl := &mockOfferHandler{
		decision: ActionDecision{Action: ActionCounter},
	}
	p := NewProcessor(mockMgr, mockHdl, log.Discard)
	p.Start(t.Context())

	p.Enqueue(&TradeOffer{ID: 456})

	time.Sleep(50 * time.Millisecond)

	if mockMgr.declineCalls != 1 {
		t.Errorf("expected counter fallback to call DeclineOffer, got %d calls", mockMgr.declineCalls)
	}
	if !mockHdl.onActionFailedCalled {
		t.Error("expected OnActionFailed to be called for the initial counter failure")
	}
	if mockHdl.failedAction != ActionCounter {
		t.Errorf("expected failed action to be ActionCounter, got %s", mockHdl.failedAction)
	}
}

func TestProcessor_WithRetry(t *testing.T) {
	p := NewProcessor(nil, nil, log.Discard)

	attempts := 0
	err := p.withRetry(context.Background(), 3, func() error {
		attempts++
		return nil
	})
	if err != nil || attempts != 1 {
		t.Errorf("expected success on first try, got attempts=%d, err=%v", attempts, err)
	}

	attempts = 0
	expectedErr := errors.New("persistent error")
	err = p.withRetry(context.Background(), 2, func() error {
		attempts++
		return expectedErr
	})
	if !errors.Is(err, expectedErr) || attempts != 3 {
		t.Errorf("expected persistent error after all retries, got attempts=%d, err=%v", attempts, err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err = p.withRetry(ctx, 5, func() error {
		return errors.New("any error")
	})
	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled error, got %v", err)
	}
}

func TestProcessor_CheckEscrow(t *testing.T) {
	mockMgr := &mockManager{}
	p := NewProcessor(mockMgr, nil, log.Discard)

	ctx := context.Background()

	offerWithDate := &TradeOffer{ID: 1, EscrowEndDate: time.Now().Unix() + 3600}
	hasEscrow, err := p.CheckEscrow(ctx, offerWithDate)
	if err != nil || !hasEscrow {
		t.Errorf("expected true for escrow from date, got hasEscrow=%v, err=%v", hasEscrow, err)
	}
	if mockMgr.escrowCalls != 0 {
		t.Error("GetEscrowDuration should not be called if date exists")
	}

	mockMgr.escrowDetails = EscrowDetails{TheirDays: 7}
	offerWithoutDate := &TradeOffer{ID: 2}
	hasEscrow, err = p.CheckEscrow(ctx, offerWithoutDate)
	if err != nil || !hasEscrow {
		t.Errorf("expected true for escrow from API, got hasEscrow=%v, err=%v", hasEscrow, err)
	}
	if mockMgr.escrowCalls != 1 {
		t.Errorf("expected 1 call to GetEscrowDuration, got %d", mockMgr.escrowCalls)
	}

	mockMgr.escrowCalls = 0
	mockMgr.escrowDetails = EscrowDetails{TheirDays: 0}
	hasEscrow, err = p.CheckEscrow(ctx, offerWithoutDate)
	if err != nil || hasEscrow {
		t.Errorf("expected false for no escrow, got hasEscrow=%v, err=%v", hasEscrow, err)
	}
}

func TestProcessor_ItemTracking(t *testing.T) {
	p := NewProcessor(nil, nil, log.Discard)
	assetID := uint64(12345)

	if p.IsInTrade(assetID) {
		t.Error("expected item not to be in trade initially")
	}

	p.SetItemInTrade(assetID)
	if !p.IsInTrade(assetID) {
		t.Error("expected item to be in trade after Set")
	}

	p.UnsetItemInTrade(assetID)
	if p.IsInTrade(assetID) {
		t.Error("expected item not to be in trade after Unset")
	}
}

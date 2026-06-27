// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package processor

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/lemon4ksan/g-man/pkg/steam/protocol"
	"github.com/lemon4ksan/g-man/pkg/trading"
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

func (m *backpackMock) GetCalls() (lockCalls, unlockCalls int) {
	m.mu.Lock()
	defer m.mu.Unlock()

	return m.lockCalls, m.unlockCalls
}

type mockManager struct {
	mu            sync.Mutex
	acceptCalls   int
	declineCalls  int
	sendCalls     int
	lastParams    trading.OfferParams
	shouldFail    bool
	sendChan      chan trading.OfferParams
	acceptChan    chan uint64
	declineChan   chan uint64
	escrowDetails Details
	escrowErr     error
}

func (m *mockManager) AcceptOffer(ctx context.Context, id uint64) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.shouldFail {
		return errors.New("steam error")
	}

	m.acceptCalls++
	if m.acceptChan != nil {
		m.acceptChan <- id
	}

	return nil
}

func (m *mockManager) DeclineOffer(ctx context.Context, id uint64) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.declineCalls++
	if m.declineChan != nil {
		m.declineChan <- id
	}

	return nil
}

func (m *mockManager) SendOffer(ctx context.Context, p trading.OfferParams) (uint64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.sendCalls++

	m.lastParams = p
	if m.sendChan != nil {
		m.sendChan <- p
	}

	return 444, nil
}

func (m *mockManager) GetEscrowDuration(ctx context.Context, id uint64) (Details, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.escrowErr != nil {
		return Details{}, m.escrowErr
	}

	return m.escrowDetails, nil
}

type mockOfferHandler struct {
	mu           sync.Mutex
	processCalls int
	decision     trading.ActionDecision
	processErr   error
	failedCalled bool
	blockChan    chan struct{}
	lastCtx      context.Context
	processChan  chan uint64
	failedChan   chan error
}

func (h *mockOfferHandler) ProcessOffer(ctx context.Context, off *trading.TradeOffer) (trading.ActionDecision, error) {
	h.mu.Lock()
	h.processCalls++
	decision := h.decision
	block := h.blockChan
	h.lastCtx = ctx
	h.mu.Unlock()

	if h.processChan != nil {
		h.processChan <- off.ID
	}

	if h.processErr != nil {
		return trading.ActionDecision{}, h.processErr
	}

	if block != nil {
		<-block
	}

	return decision, nil
}

func (h *mockOfferHandler) OnActionFailed(
	ctx context.Context,
	off *trading.TradeOffer,
	act trading.ActionType,
	reason string,
	err error,
) {
	h.mu.Lock()
	h.failedCalled = true
	h.mu.Unlock()

	if h.failedChan != nil {
		h.failedChan <- err
	}
}

func (h *mockOfferHandler) GetProcessCalls() int {
	h.mu.Lock()
	defer h.mu.Unlock()

	return h.processCalls
}

func (h *mockOfferHandler) IsFailedCalled() bool {
	h.mu.Lock()
	defer h.mu.Unlock()

	return h.failedCalled
}

type testFixture struct {
	proc    *Processor
	manager *mockManager
	handler *mockOfferHandler
	bp      *backpackMock
}

func newTestFixture(t *testing.T, decision trading.ActionDecision, sleep time.Duration) *testFixture {
	t.Helper()

	mgr := &mockManager{
		sendChan:    make(chan trading.OfferParams, 10),
		acceptChan:  make(chan uint64, 10),
		declineChan: make(chan uint64, 10),
	}
	bp := newBackpackMock()
	hdl := &mockOfferHandler{
		decision:    decision,
		processChan: make(chan uint64, 10),
		failedChan:  make(chan error, 10),
	}
	p := New(mgr, bp, hdl)

	return &testFixture{
		proc:    p,
		manager: mgr,
		handler: hdl,
		bp:      bp,
	}
}

func TestDetails_HasHold(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		myDays    int
		theirDays int
		want      bool
	}{
		{"no_hold", 0, 0, false},
		{"my_hold_only", 3, 0, true},
		{"their_hold_only", 0, 5, true},
		{"both_holds", 3, 5, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			d := Details{MyDays: tt.myDays, TheirDays: tt.theirDays}

			got := d.HasHold()
			if got != tt.want {
				t.Errorf("expected HasHold() = %v, got %v", tt.want, got)
			}
		})
	}
}

func TestCheckEscrow(t *testing.T) {
	t.Parallel()

	t.Run("fast_path_escrow_end_date", func(t *testing.T) {
		t.Parallel()

		f := newTestFixture(t, trading.ActionDecision{}, 0)
		off := &trading.TradeOffer{ID: 1, EscrowEndDate: 1710000000}

		hold, err := f.proc.CheckEscrow(t.Context(), off)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if !hold {
			t.Errorf("expected hold to be true")
		}
	})

	t.Run("fetch_escrow_success_no_hold", func(t *testing.T) {
		t.Parallel()

		f := newTestFixture(t, trading.ActionDecision{}, 0)
		f.manager.escrowDetails = Details{MyDays: 0, TheirDays: 0}
		off := &trading.TradeOffer{ID: 2}

		hold, err := f.proc.CheckEscrow(t.Context(), off)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if hold {
			t.Errorf("expected hold to be false")
		}
	})

	t.Run("fetch_escrow_success_with_their_hold", func(t *testing.T) {
		t.Parallel()

		f := newTestFixture(t, trading.ActionDecision{}, 0)
		f.manager.escrowDetails = Details{MyDays: 0, TheirDays: 15}
		off := &trading.TradeOffer{ID: 3}

		hold, err := f.proc.CheckEscrow(t.Context(), off)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if !hold {
			t.Errorf("expected hold to be true")
		}
	})

	t.Run("fetch_escrow_error_aborts_on_timeout", func(t *testing.T) {
		t.Parallel()

		f := newTestFixture(t, trading.ActionDecision{}, 0)
		f.manager.escrowErr = errors.New("temporary error")
		off := &trading.TradeOffer{ID: 4}

		ctx, cancel := context.WithTimeout(t.Context(), 5*time.Millisecond)
		t.Cleanup(cancel)

		hold, err := f.proc.CheckEscrow(ctx, off)
		if hold {
			t.Errorf("expected hold to be false")
		}

		if !errors.Is(err, context.DeadlineExceeded) {
			t.Errorf("expected context.DeadlineExceeded error, got %v", err)
		}
	})

	t.Run("fetch_escrow_not_found_returns_error", func(t *testing.T) {
		t.Parallel()

		f := newTestFixture(t, trading.ActionDecision{}, 0)
		f.manager.escrowErr = ErrEscrowNotFound
		off := &trading.TradeOffer{ID: 5}

		hold, err := f.proc.CheckEscrow(t.Context(), off)
		if hold {
			t.Errorf("expected hold to be false")
		}

		if !errors.Is(err, ErrEscrowNotFound) {
			t.Errorf("expected error %v, got %v", ErrEscrowNotFound, err)
		}
	})
}

func TestEnqueue(t *testing.T) {
	t.Parallel()

	t.Run("duplicate_offers_processed_only_once", func(t *testing.T) {
		t.Parallel()

		f := newTestFixture(t, trading.ActionDecision{Action: trading.ActionSkip}, 0)
		f.proc.Start(t.Context())

		off := &trading.TradeOffer{ID: 111}

		f.proc.Enqueue(off)
		f.proc.Enqueue(off)

		ctx, cancel := context.WithTimeout(t.Context(), 500*time.Millisecond)
		t.Cleanup(cancel)

		select {
		case id := <-f.handler.processChan:
			if id != 111 {
				t.Errorf("expected offer ID 111, got %d", id)
			}
		case <-ctx.Done():
			t.Fatal("timeout waiting for process")
		}

		if f.handler.GetProcessCalls() != 1 {
			t.Errorf("expected 1 process call, got %d", f.handler.GetProcessCalls())
		}
	})

	t.Run("queue_is_full_drops_offer", func(t *testing.T) {
		t.Parallel()

		f := newTestFixture(t, trading.ActionDecision{Action: trading.ActionSkip}, 0)

		for i := range 500 {
			f.proc.Enqueue(&trading.TradeOffer{ID: uint64(i + 1)})
		}

		if len(f.proc.queue) != 500 {
			t.Errorf("expected queue length 500, got %d", len(f.proc.queue))
		}

		droppedOffer := &trading.TradeOffer{ID: 9999}
		f.proc.Enqueue(droppedOffer)

		if _, loaded := f.proc.processing.Load(uint64(9999)); loaded {
			t.Errorf("dropped offer should be deleted from the processing map")
		}
	})

	t.Run("processes_sequentially", func(t *testing.T) {
		t.Parallel()

		block := make(chan struct{})
		f := newTestFixture(t, trading.ActionDecision{Action: trading.ActionSkip}, 0)
		f.handler.blockChan = block
		f.proc.Start(t.Context())

		f.proc.Enqueue(&trading.TradeOffer{ID: 1})
		f.proc.Enqueue(&trading.TradeOffer{ID: 2})

		ctx, cancel := context.WithTimeout(t.Context(), 500*time.Millisecond)
		t.Cleanup(cancel)

		select {
		case id := <-f.handler.processChan:
			if id != 1 {
				t.Errorf("expected offer ID 1, got %d", id)
			}
		case <-ctx.Done():
			t.Fatal("timeout waiting for first offer to start processing")
		}

		if f.handler.GetProcessCalls() != 1 {
			t.Errorf("expected exactly 1 process call, got %d", f.handler.GetProcessCalls())
		}

		close(block)

		select {
		case id := <-f.handler.processChan:
			if id != 2 {
				t.Errorf("expected offer ID 2, got %d", id)
			}
		case <-ctx.Done():
			t.Fatal("timeout waiting for second offer to start processing")
		}

		if f.handler.GetProcessCalls() != 2 {
			t.Errorf("expected exactly 2 process calls, got %d", f.handler.GetProcessCalls())
		}
	})

	t.Run("processing_map_cleanup_after_five_seconds", func(t *testing.T) {
		t.Parallel()

		f := newTestFixture(t, trading.ActionDecision{Action: trading.ActionSkip}, 0)
		f.proc.Start(t.Context())

		off := &trading.TradeOffer{ID: 1000}
		f.proc.Enqueue(off)

		select {
		case <-f.handler.processChan:
		case <-t.Context().Done():
			t.Fatal("timeout waiting for process")
		}

		if _, loaded := f.proc.processing.Load(off.ID); !loaded {
			t.Errorf("expected offer to be in processing map")
		}

		timer := time.NewTimer(5100 * time.Millisecond)
		defer timer.Stop()

		select {
		case <-t.Context().Done():
			t.Fatal("context cancelled")
		case <-timer.C:
		}

		if _, loaded := f.proc.processing.Load(off.ID); loaded {
			t.Errorf("expected offer to be deleted from processing map after 5 seconds")
		}
	})
}

func TestHandleOffer(t *testing.T) {
	t.Parallel()

	t.Run("various_decisions_manages_item_locks", func(t *testing.T) {
		t.Parallel()

		tests := []struct {
			name          string
			action        trading.ActionType
			expectUnlock  bool
			expectManager bool
		}{
			{
				name:          "accept_no_unlock",
				action:        trading.ActionAccept,
				expectUnlock:  false,
				expectManager: true,
			},
			{
				name:          "decline_must_unlock",
				action:        trading.ActionDecline,
				expectUnlock:  true,
				expectManager: true,
			},
			{
				name:          "skip_must_unlock",
				action:        trading.ActionSkip,
				expectUnlock:  true,
				expectManager: false,
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				t.Parallel()

				f := newTestFixture(t, trading.ActionDecision{Action: tt.action}, 0)
				f.proc.Start(t.Context())

				off := &trading.TradeOffer{
					ID:          999,
					ItemsToGive: []*trading.Item{{AssetID: 1}},
				}

				f.proc.Enqueue(off)

				ctx, cancel := context.WithTimeout(t.Context(), 500*time.Millisecond)
				t.Cleanup(cancel)

				select {
				case <-f.handler.processChan:
				case <-ctx.Done():
					t.Fatal("timeout waiting for process")
				}

				if tt.expectManager {
					switch tt.action {
					case trading.ActionAccept:
						select {
						case <-f.manager.acceptChan:
						case <-ctx.Done():
							t.Fatal("timeout waiting for accept call")
						}

					case trading.ActionDecline:
						select {
						case <-f.manager.declineChan:
						case <-ctx.Done():
							t.Fatal("timeout waiting for decline call")
						}
					}
				}

				lockCalls, unlockCalls := f.bp.GetCalls()

				if lockCalls != 1 {
					t.Errorf("expected LockItems to be called once, got %d", lockCalls)
				}

				if tt.expectUnlock {
					if unlockCalls != 1 {
						t.Errorf("expected UnlockItems to be called once, got %d", unlockCalls)
					}
				} else {
					if unlockCalls != 0 {
						t.Errorf("expected UnlockItems NOT to be called, got %d", unlockCalls)
					}
				}
			})
		}
	})

	t.Run("counter_decision_sends_counter_offer_and_manages_locks", func(t *testing.T) {
		t.Parallel()

		counterParams := &trading.CounterParams{
			Message:     "Balance please",
			Token:       "xyz",
			ItemsToGive: []*trading.Item{{AssetID: 1}},
		}

		f := newTestFixture(t, trading.ActionDecision{
			Action:        trading.ActionCounter,
			CounterParams: counterParams,
		}, 0)
		f.proc.Start(t.Context())

		f.proc.Enqueue(&trading.TradeOffer{
			ID:           555,
			OtherSteamID: 12345,
			ItemsToGive:  []*trading.Item{{AssetID: 100}},
		})

		ctx, cancel := context.WithTimeout(t.Context(), 500*time.Millisecond)
		t.Cleanup(cancel)

		select {
		case params := <-f.manager.sendChan:
			if params.CounteredID != 555 {
				t.Errorf("expected CounteredID 555, got %d", params.CounteredID)
			}

			if params.Token != "xyz" {
				t.Errorf("expected Token 'xyz', got %q", params.Token)
			}

		case <-ctx.Done():
			t.Fatal("timeout waiting for SendOffer call")
		}

		lockCalls, unlockCalls := f.bp.GetCalls()
		if lockCalls != 1 {
			t.Errorf("expected LockItems once, got %d", lockCalls)
		}

		if unlockCalls != 1 {
			t.Errorf("expected UnlockItems once after counter-offer, got %d", unlockCalls)
		}
	})

	t.Run("executor_failure_retries_and_calls_on_action_failed", func(t *testing.T) {
		t.Parallel()

		f := newTestFixture(t, trading.ActionDecision{Action: trading.ActionAccept}, 0)
		f.manager.shouldFail = true

		ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
		t.Cleanup(cancel)

		f.proc.Start(ctx)

		f.proc.Enqueue(&trading.TradeOffer{ID: 777})

		waitCtx, waitCancel := context.WithTimeout(t.Context(), 500*time.Millisecond)
		t.Cleanup(waitCancel)

		select {
		case err := <-f.handler.failedChan:
			if err == nil {
				t.Fatalf("expected error, got nil")
			}
		case <-waitCtx.Done():
			t.Fatal("timeout waiting for OnActionFailed call")
		}

		if !f.handler.IsFailedCalled() {
			t.Errorf("expected OnActionFailed to be called")
		}
	})

	t.Run("valid_offer_propagates_transport_type", func(t *testing.T) {
		t.Parallel()

		f := newTestFixture(t, trading.ActionDecision{Action: trading.ActionSkip}, 0)
		f.proc.Start(t.Context())

		f.proc.Enqueue(&trading.TradeOffer{ID: 12345})

		ctx, cancel := context.WithTimeout(t.Context(), 500*time.Millisecond)
		t.Cleanup(cancel)

		select {
		case <-f.handler.processChan:
		case <-ctx.Done():
			t.Fatal("timeout waiting for process")
		}

		f.handler.mu.Lock()
		capturedCtx := f.handler.lastCtx
		f.handler.mu.Unlock()

		if capturedCtx == nil {
			t.Fatalf("expected non-nil context")
		}

		transport, ok := protocol.GetTransportType(capturedCtx)
		if !ok {
			t.Errorf("expected transport type to be present")
		}

		if transport != protocol.TransportWebAPI {
			t.Errorf("expected transport type %v, got %v", protocol.TransportWebAPI, transport)
		}
	})

	t.Run("handler_process_offer_error_unlocks_items", func(t *testing.T) {
		t.Parallel()

		f := newTestFixture(t, trading.ActionDecision{}, 0)
		f.handler.processErr = errors.New("processing error")
		f.proc.Start(t.Context())

		off := &trading.TradeOffer{
			ID:          102,
			ItemsToGive: []*trading.Item{{AssetID: 1}},
		}
		f.proc.Enqueue(off)

		select {
		case <-f.handler.processChan:
		case <-t.Context().Done():
			t.Fatal("timeout waiting for process")
		}

		lockCalls, unlockCalls := f.bp.GetCalls()
		if lockCalls != 1 {
			t.Errorf("expected 1 LockItems call, got %d", lockCalls)
		}

		if unlockCalls != 1 {
			t.Errorf("expected 1 UnlockItems call, got %d", unlockCalls)
		}
	})
}

func TestApplyAction(t *testing.T) {
	t.Parallel()

	t.Run("unknown_action_type_returns_error", func(t *testing.T) {
		t.Parallel()

		f := newTestFixture(t, trading.ActionDecision{}, 0)

		err := f.proc.applyAction(t.Context(), &trading.TradeOffer{ID: 1}, trading.ActionDecision{
			Action: trading.ActionType("unknown"),
		})
		if err == nil {
			t.Fatalf("expected error, got nil")
		}

		if !strings.Contains(err.Error(), "unknown action type") {
			t.Errorf("expected error containing 'unknown action type', got: %v", err)
		}
	})

	t.Run("counter_action_missing_params_returns_error", func(t *testing.T) {
		t.Parallel()

		f := newTestFixture(t, trading.ActionDecision{}, 0)

		err := f.proc.applyAction(t.Context(), &trading.TradeOffer{ID: 1}, trading.ActionDecision{
			Action:        trading.ActionCounter,
			CounterParams: nil,
		})
		if err == nil {
			t.Fatalf("expected error, got nil")
		}

		if !strings.Contains(err.Error(), "counter params are missing") {
			t.Errorf("expected error containing 'counter params are missing', got: %v", err)
		}
	})
}

func TestWithRetry(t *testing.T) {
	t.Parallel()

	t.Run("with_retry_successful_on_second_attempt", func(t *testing.T) {
		t.Parallel()

		f := newTestFixture(t, trading.ActionDecision{}, 0)

		attempts := 0

		err := f.proc.withRetry(t.Context(), 2, func() error {
			attempts++
			if attempts == 1 {
				return errors.New("temporary error")
			}

			return nil
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if attempts != 2 {
			t.Errorf("expected 2 attempts, got %d", attempts)
		}
	})

	t.Run("with_retry_zero_retries_breaks_immediately", func(t *testing.T) {
		t.Parallel()

		f := newTestFixture(t, trading.ActionDecision{}, 0)

		attempts := 0

		err := f.proc.withRetry(t.Context(), 0, func() error {
			attempts++
			return errors.New("permanent error")
		})
		if err == nil {
			t.Fatalf("expected error, got nil")
		}

		if attempts != 1 {
			t.Errorf("expected 1 attempt, got %d", attempts)
		}
	})
}

func TestOptions(t *testing.T) {
	t.Parallel()

	mgr := &mockManager{}
	bp := newBackpackMock()
	hdl := &mockOfferHandler{}

	p := New(mgr, bp, hdl)

	opt := WithLogger(nil)
	opt(p)

	if p.logger != nil {
		t.Errorf("expected p.logger to be nil after applying WithLogger(nil)")
	}
}

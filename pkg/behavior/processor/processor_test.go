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

	"github.com/lemon4ksan/miyako/bus"
	"github.com/lemon4ksan/miyako/log"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lemon4ksan/g-man/pkg/steam/id"
	"github.com/lemon4ksan/g-man/pkg/steam/protocol"
	"github.com/lemon4ksan/g-man/pkg/trading"
	"github.com/lemon4ksan/g-man/pkg/trading/engine"
	"github.com/lemon4ksan/g-man/pkg/trading/notifications"
	"github.com/lemon4ksan/g-man/pkg/trading/reason"
	"github.com/lemon4ksan/g-man/pkg/trading/review"
)

// mockExecutor implements TradeExecutor interface.
type mockExecutor struct {
	mu          sync.Mutex
	acceptedIDs []uint64
	declinedIDs []uint64
	acceptErr   error
	declineErr  error
	callsChan   chan uint64
}

func (m *mockExecutor) AcceptOffer(ctx context.Context, id uint64) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.acceptErr != nil {
		return m.acceptErr
	}

	m.acceptedIDs = append(m.acceptedIDs, id)
	if m.callsChan != nil {
		m.callsChan <- id
	}

	return nil
}

func (m *mockExecutor) DeclineOffer(ctx context.Context, id uint64) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.declineErr != nil {
		return m.declineErr
	}

	m.declinedIDs = append(m.declinedIDs, id)
	if m.callsChan != nil {
		m.callsChan <- id
	}

	return nil
}

// mockReviewChat implements review.ChatProvider.
type mockReviewChat struct {
	mu            sync.Mutex
	messages      []string
	adminMessages []string
}

func (m *mockReviewChat) SendMessage(ctx context.Context, steamID uint64, message string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.messages = append(m.messages, message)

	return nil
}

func (m *mockReviewChat) MessageAdmins(ctx context.Context, message string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.adminMessages = append(m.adminMessages, message)

	return nil
}

// mockNotifChat implements notifications.ChatProvider.
type mockNotifChat struct {
	reviewChat *mockReviewChat
}

func (m *mockNotifChat) SendMessage(ctx context.Context, steamID id.ID, message string) error {
	m.reviewChat.mu.Lock()
	defer m.reviewChat.mu.Unlock()

	m.reviewChat.messages = append(m.reviewChat.messages, message)

	return nil
}

// mockSchemaProvider implements review.SchemaProvider.
type mockSchemaProvider struct{}

func (m *mockSchemaProvider) GetName(sku string, useDefindex bool) string {
	return "Mock Item"
}

// mockConfigProvider implements notifications.ConfigProvider.
type mockConfigProvider struct{}

func (m *mockConfigProvider) GetTemplate(key string) string {
	return ""
}

func (m *mockConfigProvider) GetCommandPrefix() string {
	return "!"
}

type testFixture struct {
	proc       *Processor
	executor   *mockExecutor
	reviewChat *mockReviewChat
}

func newTestFixture(t *testing.T, eng *engine.Engine) *testFixture {
	t.Helper()

	logger := log.New(log.DefaultConfig(log.LevelError))
	ex := &mockExecutor{
		callsChan: make(chan uint64, 150),
	}

	reviewChat := &mockReviewChat{}
	notifChat := &mockNotifChat{reviewChat: reviewChat}
	cfg := &mockConfigProvider{}
	notifMgr := notifications.NewManager(notifChat, cfg, logger)

	schema := &mockSchemaProvider{}
	reviewer := review.New(schema, reviewChat, logger)

	proc := New(ex, eng, notifMgr, reviewer, bus.New(), logger)

	return &testFixture{
		proc:       proc,
		executor:   ex,
		reviewChat: reviewChat,
	}
}

func TestStart_QueueWithOffers_ProcessesOffersSequentially(t *testing.T) {
	t.Parallel()

	eng := engine.New()
	eng.Use(func(next engine.Handler) engine.Handler {
		return func(ctx *engine.TradeContext) error {
			ctx.Accept(reason.AcceptDonation)
			return nil
		}
	})

	f := newTestFixture(t, eng)
	go f.proc.Run(t.Context())

	offer1 := &trading.TradeOffer{
		ID:           1,
		OtherSteamID: id.ID(76561198000000001),
		ItemsToGive: []*trading.Item{
			{AssetID: 101, SKU: "5021;6"},
		},
	}
	offer2 := &trading.TradeOffer{
		ID:           2,
		OtherSteamID: id.ID(76561198000000002),
		ItemsToGive: []*trading.Item{
			{AssetID: 102, SKU: "5021;6"},
		},
	}

	f.proc.Enqueue(offer1)
	f.proc.Enqueue(offer2)

	ctx, cancel := context.WithTimeout(t.Context(), 500*time.Millisecond)
	t.Cleanup(cancel)

	for range 2 {
		select {
		case <-f.executor.callsChan:
		case <-ctx.Done():
			t.Fatal("timeout waiting for executor calls")
		}
	}

	f.executor.mu.Lock()
	assert.ElementsMatch(t, []uint64{1, 2}, f.executor.acceptedIDs)
	f.executor.mu.Unlock()

	f.reviewChat.mu.Lock()
	assert.Len(t, f.reviewChat.messages, 2)
	f.reviewChat.mu.Unlock()
}

func TestHandleOffer_ItemsAlreadyLocked_SkipsProcessing(t *testing.T) {
	t.Parallel()

	eng := engine.New()
	eng.Use(func(next engine.Handler) engine.Handler {
		return func(ctx *engine.TradeContext) error {
			ctx.Accept(reason.AcceptDonation)
			return nil
		}
	})

	f := newTestFixture(t, eng)

	offer := &trading.TradeOffer{
		ID:           11,
		OtherSteamID: id.ID(76561198000000002),
		ItemsToGive: []*trading.Item{
			{AssetID: 500, SKU: "5021;6"},
		},
	}

	f.proc.itemLocks.Lock(500)
	f.proc.busyItems[500] = 10

	f.proc.handleOffer(t.Context(), offer)

	f.proc.itemLocks.Unlock(500)

	f.executor.mu.Lock()
	assert.Empty(t, f.executor.acceptedIDs)
	assert.Empty(t, f.executor.declinedIDs)
	f.executor.mu.Unlock()
}

func TestHandleOffer_VariousVerdicts_ExecutesExpectedActions(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		action        trading.ActionType
		reason        reason.TradeReason
		expectAccept  bool
		expectDecline bool
		expectReview  bool
		expectNotif   bool
	}{
		{
			name:         "accept_verdict",
			action:       trading.ActionAccept,
			reason:       reason.AcceptDonation,
			expectAccept: true,
			expectNotif:  true,
		},
		{
			name:          "decline_verdict",
			action:        trading.ActionDecline,
			reason:        reason.DeclineBlacklisted,
			expectDecline: true,
			expectNotif:   true,
			expectReview:  true,
		},
		{
			name:         "review_verdict",
			action:       trading.ActionReview,
			reason:       reason.ReviewOverstocked,
			expectReview: true,
			expectNotif:  false,
		},
		{
			name:   "ignore_verdict",
			action: trading.ActionIgnore,
			reason: reason.ReviewInvalidItems,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			eng := engine.New()
			eng.Use(func(next engine.Handler) engine.Handler {
				return func(ctx *engine.TradeContext) error {
					ctx.Verdict.Action = tt.action
					ctx.Verdict.Reason = tt.reason
					return nil
				}
			})

			f := newTestFixture(t, eng)

			offer := &trading.TradeOffer{
				ID:           999,
				OtherSteamID: id.ID(76561198000000999),
				ItemsToGive: []*trading.Item{
					{AssetID: 999, SKU: "5021;6"},
				},
			}

			f.proc.handleOffer(t.Context(), offer)

			f.executor.mu.Lock()
			if tt.expectAccept {
				assert.Equal(t, []uint64{999}, f.executor.acceptedIDs)
			} else {
				assert.Empty(t, f.executor.acceptedIDs)
			}

			if tt.expectDecline {
				assert.Equal(t, []uint64{999}, f.executor.declinedIDs)
			} else {
				assert.Empty(t, f.executor.declinedIDs)
			}

			f.executor.mu.Unlock()

			f.reviewChat.mu.Lock()
			if tt.expectNotif {
				assert.NotEmpty(t, f.reviewChat.messages)
			} else {
				assert.Empty(t, f.reviewChat.messages)
			}

			if tt.expectReview {
				assert.NotEmpty(t, f.reviewChat.adminMessages)
			} else {
				assert.Empty(t, f.reviewChat.adminMessages)
			}

			f.reviewChat.mu.Unlock()
		})
	}
}

func TestHandleOffer_AcceptExecutorError_HandlesGracefully(t *testing.T) {
	t.Parallel()

	eng := engine.New()
	eng.Use(func(next engine.Handler) engine.Handler {
		return func(ctx *engine.TradeContext) error {
			ctx.Accept(reason.AcceptDonation)
			return nil
		}
	})

	f := newTestFixture(t, eng)
	f.executor.acceptErr = errors.New("steam accepted failed connection")

	offer := &trading.TradeOffer{
		ID:           888,
		OtherSteamID: id.ID(76561198000000888),
	}

	assert.NotPanics(t, func() {
		f.proc.handleOffer(t.Context(), offer)
	})
}

func TestHandleOffer_DeclineExecutorError_HandlesGracefully(t *testing.T) {
	t.Parallel()

	eng := engine.New()
	eng.Use(func(next engine.Handler) engine.Handler {
		return func(ctx *engine.TradeContext) error {
			ctx.Decline(reason.DeclineBlacklisted)
			return nil
		}
	})

	f := newTestFixture(t, eng)
	f.executor.declineErr = errors.New("steam decline failed connection")

	offer := &trading.TradeOffer{
		ID:           777,
		OtherSteamID: id.ID(76561198000000777),
	}

	assert.NotPanics(t, func() {
		f.proc.handleOffer(t.Context(), offer)
	})

	f.reviewChat.mu.Lock()
	assert.Empty(t, f.reviewChat.messages)
	assert.Empty(t, f.reviewChat.adminMessages)
	f.reviewChat.mu.Unlock()
}

func TestHandleOffer_EngineError_HandlesGracefully(t *testing.T) {
	t.Parallel()

	eng := engine.New()
	eng.Use(func(next engine.Handler) engine.Handler {
		return func(ctx *engine.TradeContext) error {
			return errors.New("some middleware processing error")
		}
	})

	f := newTestFixture(t, eng)

	offer := &trading.TradeOffer{
		ID:           666,
		OtherSteamID: id.ID(76561198000000666),
	}

	assert.NotPanics(t, func() {
		f.proc.handleOffer(t.Context(), offer)
	})

	f.executor.mu.Lock()
	assert.Empty(t, f.executor.acceptedIDs)
	assert.Empty(t, f.executor.declinedIDs)
	f.executor.mu.Unlock()
}

func TestHandleOffer_ValidOffer_PropagatesTransportType(t *testing.T) {
	t.Parallel()

	var capturedCtx context.Context

	eng := engine.New()
	eng.Use(func(next engine.Handler) engine.Handler {
		return func(ctx *engine.TradeContext) error {
			capturedCtx = ctx.Context
			ctx.Accept(reason.AcceptCorrectValue)
			return nil
		}
	})

	f := newTestFixture(t, eng)

	offer := &trading.TradeOffer{
		ID:           777,
		OtherSteamID: id.ID(76561198000000777),
	}

	f.proc.handleOffer(t.Context(), offer)

	require.NotNil(t, capturedCtx, "expected non-nil context in middleware")

	transport, ok := protocol.GetTransportType(capturedCtx)
	assert.True(t, ok)
	assert.Equal(t, protocol.TransportWebAPI, transport)
}

func TestEnqueue_DuplicateOffers_ProcessesOnlyOnce(t *testing.T) {
	t.Parallel()

	eng := engine.New()
	eng.Use(func(next engine.Handler) engine.Handler {
		return func(ctx *engine.TradeContext) error {
			ctx.Accept(reason.AcceptDonation)
			return nil
		}
	})

	f := newTestFixture(t, eng)
	go f.proc.Run(t.Context())

	offer := &trading.TradeOffer{
		ID:           12345,
		OtherSteamID: id.ID(76561198000000001),
		ItemsToGive: []*trading.Item{
			{AssetID: 100, SKU: "5021;6"},
		},
	}

	f.proc.Enqueue(offer)
	f.proc.Enqueue(offer)
	f.proc.Enqueue(offer)

	ctx, cancel := context.WithTimeout(t.Context(), 500*time.Millisecond)
	t.Cleanup(cancel)

	select {
	case <-f.executor.callsChan:
	case <-ctx.Done():
		t.Fatal("timeout waiting for executor call")
	}

	f.executor.mu.Lock()
	assert.Len(t, f.executor.acceptedIDs, 1, "offer should be processed only once")
	f.executor.mu.Unlock()
}

func TestEnqueue_QueueIsFull_DropsExcessOffers(t *testing.T) {
	t.Parallel()

	eng := engine.New()
	f := newTestFixture(t, eng)

	for i := range 150 {
		offerID := uint64(i + 1)
		f.proc.Enqueue(&trading.TradeOffer{
			ID:           offerID,
			OtherSteamID: id.ID(76561198000000000 + offerID),
		})
	}

	assert.Equal(t, 100, len(f.proc.queue), "queue should be filled to its capacity of 100")
}

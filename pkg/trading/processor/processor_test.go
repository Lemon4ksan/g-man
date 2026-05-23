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

	"github.com/stretchr/testify/assert"

	"github.com/lemon4ksan/g-man/pkg/log"
	"github.com/lemon4ksan/g-man/pkg/steam/id"
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
}

func (m *mockExecutor) AcceptOffer(ctx context.Context, id uint64) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.acceptErr != nil {
		return m.acceptErr
	}

	m.acceptedIDs = append(m.acceptedIDs, id)

	return nil
}

func (m *mockExecutor) DeclineOffer(ctx context.Context, id uint64) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.declineErr != nil {
		return m.declineErr
	}

	m.declinedIDs = append(m.declinedIDs, id)

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

func TestProcessor_SequentialExecution(t *testing.T) {
	logger := log.New(log.DefaultConfig(log.LevelError))
	executor := &mockExecutor{}

	// Setup Chat Providers
	reviewChat := &mockReviewChat{}
	notifChat := &mockNotifChat{reviewChat: reviewChat}

	cfg := &mockConfigProvider{}
	notifMgr := notifications.NewManager(notifChat, cfg, logger)

	// Setup Reviewer
	schema := &mockSchemaProvider{}
	reviewer := review.New(schema, reviewChat, logger)

	// Setup Engine that accepts
	eng := engine.New()
	eng.Use(func(next engine.Handler) engine.Handler {
		return func(ctx *engine.TradeContext) error {
			ctx.Accept(reason.AcceptDonation)
			return nil
		}
	})

	proc := New(executor, eng, notifMgr, reviewer, logger)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	proc.Start(ctx)

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

	proc.Enqueue(offer1)
	proc.Enqueue(offer2)

	// Wait for processing
	time.Sleep(100 * time.Millisecond)

	executor.mu.Lock()
	assert.ElementsMatch(t, []uint64{1, 2}, executor.acceptedIDs)
	executor.mu.Unlock()

	reviewChat.mu.Lock()
	assert.Len(t, reviewChat.messages, 2) // Notifications sent
	reviewChat.mu.Unlock()
}

func TestProcessor_ItemLockingAndBusySkip(t *testing.T) {
	logger := log.New(log.DefaultConfig(log.LevelError))
	executor := &mockExecutor{}

	reviewChat := &mockReviewChat{}
	notifChat := &mockNotifChat{reviewChat: reviewChat}
	cfg := &mockConfigProvider{}
	notifMgr := notifications.NewManager(notifChat, cfg, logger)
	schema := &mockSchemaProvider{}
	reviewer := review.New(schema, reviewChat, logger)

	eng := engine.New()
	eng.Use(func(next engine.Handler) engine.Handler {
		return func(ctx *engine.TradeContext) error {
			ctx.Accept(reason.AcceptDonation)
			return nil
		}
	})

	proc := New(executor, eng, notifMgr, reviewer, logger)

	offer := &trading.TradeOffer{
		ID:           11,
		OtherSteamID: id.ID(76561198000000002),
		ItemsToGive: []*trading.Item{
			{AssetID: 500, SKU: "5021;6"},
		},
	}

	// Manually set item 500 as busy!
	proc.mu.Lock()
	proc.busyItems[500] = 10
	proc.mu.Unlock()

	// Call handleOffer directly. It should return early because item 500 is busy!
	proc.handleOffer(context.Background(), offer)

	executor.mu.Lock()
	assert.Empty(t, executor.acceptedIDs)
	assert.Empty(t, executor.declinedIDs)
	executor.mu.Unlock()
}

func TestProcessor_ExecuteVerdicts(t *testing.T) {
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
			name:         "Accept Verdict",
			action:       trading.ActionAccept,
			reason:       reason.AcceptDonation,
			expectAccept: true,
			expectNotif:  true,
		},
		{
			name:          "Decline Verdict",
			action:        trading.ActionDecline,
			reason:        reason.DeclineBlacklisted,
			expectDecline: true,
			expectNotif:   true,
			expectReview:  true,
		},
		{
			name:         "Review Verdict",
			action:       trading.ActionReview,
			reason:       reason.ReviewOverstocked,
			expectReview: true,
			expectNotif:  false,
		},
		{
			name:   "Ignore Verdict",
			action: trading.ActionIgnore,
			reason: reason.ReviewInvalidItems,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logger := log.New(log.DefaultConfig(log.LevelError))
			executor := &mockExecutor{}

			reviewChat := &mockReviewChat{}
			notifChat := &mockNotifChat{reviewChat: reviewChat}
			cfg := &mockConfigProvider{}
			notifMgr := notifications.NewManager(notifChat, cfg, logger)
			schema := &mockSchemaProvider{}
			reviewer := review.New(schema, reviewChat, logger)

			eng := engine.New()
			eng.Use(func(next engine.Handler) engine.Handler {
				return func(ctx *engine.TradeContext) error {
					ctx.Verdict.Action = tt.action
					ctx.Verdict.Reason = tt.reason
					return nil
				}
			})

			proc := New(executor, eng, notifMgr, reviewer, logger)

			offer := &trading.TradeOffer{
				ID:           999,
				OtherSteamID: id.ID(76561198000000999),
				ItemsToGive: []*trading.Item{
					{AssetID: 999, SKU: "5021;6"},
				},
			}

			proc.handleOffer(context.Background(), offer)

			executor.mu.Lock()
			if tt.expectAccept {
				assert.Equal(t, []uint64{999}, executor.acceptedIDs)
			} else {
				assert.Empty(t, executor.acceptedIDs)
			}

			if tt.expectDecline {
				assert.Equal(t, []uint64{999}, executor.declinedIDs)
			} else {
				assert.Empty(t, executor.declinedIDs)
			}

			executor.mu.Unlock()

			reviewChat.mu.Lock()
			if tt.expectNotif {
				assert.NotEmpty(t, reviewChat.messages)
			} else {
				assert.Empty(t, reviewChat.messages)
			}

			if tt.expectReview {
				assert.NotEmpty(t, reviewChat.adminMessages)
			} else {
				assert.Empty(t, reviewChat.adminMessages)
			}

			reviewChat.mu.Unlock()
		})
	}
}

func TestProcessor_ExecutorErrorHandling(t *testing.T) {
	logger := log.New(log.DefaultConfig(log.LevelError))
	executor := &mockExecutor{
		acceptErr: errors.New("steam accepted failed connection"),
	}

	reviewChat := &mockReviewChat{}
	notifChat := &mockNotifChat{reviewChat: reviewChat}
	cfg := &mockConfigProvider{}
	notifMgr := notifications.NewManager(notifChat, cfg, logger)
	schema := &mockSchemaProvider{}
	reviewer := review.New(schema, reviewChat, logger)

	eng := engine.New()
	eng.Use(func(next engine.Handler) engine.Handler {
		return func(ctx *engine.TradeContext) error {
			ctx.Accept(reason.AcceptDonation)
			return nil
		}
	})

	proc := New(executor, eng, notifMgr, reviewer, logger)

	offer := &trading.TradeOffer{
		ID:           888,
		OtherSteamID: id.ID(76561198000000888),
	}

	// Should handle the executor error gracefully and not panic
	assert.NotPanics(t, func() {
		proc.handleOffer(context.Background(), offer)
	})
}

func TestProcessor_DeclineExecutorErrorHandling(t *testing.T) {
	logger := log.New(log.DefaultConfig(log.LevelError))
	executor := &mockExecutor{
		declineErr: errors.New("steam decline failed connection"),
	}

	reviewChat := &mockReviewChat{}
	notifChat := &mockNotifChat{reviewChat: reviewChat}
	cfg := &mockConfigProvider{}
	notifMgr := notifications.NewManager(notifChat, cfg, logger)
	schema := &mockSchemaProvider{}
	reviewer := review.New(schema, reviewChat, logger)

	eng := engine.New()
	eng.Use(func(next engine.Handler) engine.Handler {
		return func(ctx *engine.TradeContext) error {
			ctx.Decline(reason.DeclineBlacklisted)
			return nil
		}
	})

	proc := New(executor, eng, notifMgr, reviewer, logger)

	offer := &trading.TradeOffer{
		ID:           777,
		OtherSteamID: id.ID(76561198000000777),
	}

	// Should handle the executor error gracefully and not panic, and not send notifications
	assert.NotPanics(t, func() {
		proc.handleOffer(context.Background(), offer)
	})

	reviewChat.mu.Lock()
	assert.Empty(t, reviewChat.messages)
	assert.Empty(t, reviewChat.adminMessages)
	reviewChat.mu.Unlock()
}

func TestProcessor_EngineErrorHandling(t *testing.T) {
	logger := log.New(log.DefaultConfig(log.LevelError))
	executor := &mockExecutor{}

	reviewChat := &mockReviewChat{}
	notifChat := &mockNotifChat{reviewChat: reviewChat}
	cfg := &mockConfigProvider{}
	notifMgr := notifications.NewManager(notifChat, cfg, logger)
	schema := &mockSchemaProvider{}
	reviewer := review.New(schema, reviewChat, logger)

	eng := engine.New()
	eng.Use(func(next engine.Handler) engine.Handler {
		return func(ctx *engine.TradeContext) error {
			return errors.New("some middleware processing error")
		}
	})

	proc := New(executor, eng, notifMgr, reviewer, logger)

	offer := &trading.TradeOffer{
		ID:           666,
		OtherSteamID: id.ID(76561198000000666),
	}

	// Should handle the engine error gracefully and not panic
	assert.NotPanics(t, func() {
		proc.handleOffer(context.Background(), offer)
	})

	executor.mu.Lock()
	assert.Empty(t, executor.acceptedIDs)
	assert.Empty(t, executor.declinedIDs)
	executor.mu.Unlock()
}

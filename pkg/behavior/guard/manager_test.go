// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package guard

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/lemon4ksan/miyako/bus"
	"github.com/lemon4ksan/miyako/generic"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"

	"github.com/lemon4ksan/g-man/pkg/behavior"
	"github.com/lemon4ksan/g-man/pkg/log"
	"github.com/lemon4ksan/g-man/pkg/steam/auth"
	"github.com/lemon4ksan/g-man/pkg/steam/guard"
)

type mockGuardianProvider struct {
	mock.Mock
}

func (m *mockGuardianProvider) FetchConfirmations(ctx context.Context) ([]*guard.Confirmation, error) {
	args := m.Called(ctx)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}

	return args.Get(0).([]*guard.Confirmation), args.Error(1)
}

func (m *mockGuardianProvider) AcceptMultiple(ctx context.Context, confs []*guard.Confirmation) error {
	args := m.Called(ctx, confs)
	return args.Error(0)
}

func TestConfig_Defaults(t *testing.T) {
	t.Parallel()

	t.Run("default_guard_config", func(t *testing.T) {
		t.Parallel()

		cfg := DefaultGuardConfig("shared", "identity", "android:123")
		assert.Equal(t, "shared", cfg.SharedSecret)
		assert.Equal(t, "identity", cfg.IdentitySecret)
		assert.Equal(t, "android:123", cfg.DeviceID)
	})
}

func TestManager_Run(t *testing.T) {
	t.Parallel()

	t.Run("auto_accept_registration", func(t *testing.T) {
		t.Parallel()

		bBus := bus.New()
		logger := log.Discard
		orch := behavior.NewOrchestrator(bBus, logger)
		provider := new(mockGuardianProvider)

		AutoAccept(orch, provider, Config{})
		assert.Equal(t, 1, orch.Count())

		m := New(provider, logger, bBus, Config{})
		assert.Equal(t, BehaviorName, m.Name())
	})

	t.Run("poll_on_start_fetches_and_accepts", func(t *testing.T) {
		t.Parallel()

		provider := new(mockGuardianProvider)
		eventBus := bus.New()
		cfg := Config{
			AutoAcceptTypes: generic.NewSet(guard.ConfTypeTrade),
			PollOnStart:     true,
		}

		confs := []*guard.Confirmation{
			{ID: 1, Type: guard.ConfTypeTrade},
			{ID: 2, Type: guard.ConfTypeLogin}, // Should be skipped
		}

		provider.On("FetchConfirmations", mock.Anything).Return(confs, nil).Once()
		provider.On("AcceptMultiple", mock.Anything, []*guard.Confirmation{confs[0]}).Return(nil).Once()

		m := New(provider, log.Discard, eventBus, cfg)

		ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
		defer cancel()

		err := m.Run(ctx)
		assert.ErrorIs(t, err, context.DeadlineExceeded)
		provider.AssertExpectations(t)
	})

	t.Run("triggers_on_steam_guard_required_event", func(t *testing.T) {
		t.Parallel()

		provider := new(mockGuardianProvider)
		eventBus := bus.New()
		cfg := Config{
			AutoAcceptTypes: generic.NewSet(guard.ConfTypeLogin),
		}

		confs := []*guard.Confirmation{
			{ID: 3, Type: guard.ConfTypeLogin},
		}

		fetched := make(chan struct{})
		provider.On("FetchConfirmations", mock.Anything).Return(confs, nil).Run(func(args mock.Arguments) {
			close(fetched)
		}).Once()
		provider.On("AcceptMultiple", mock.Anything, confs).Return(nil).Once()

		m := New(provider, log.Discard, eventBus, cfg)

		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()

		go func() {
			time.Sleep(20 * time.Millisecond)
			eventBus.Publish(&auth.SteamGuardRequiredEvent{IsAppConfirm: true})

			select {
			case <-fetched:
			case <-time.After(1 * time.Second):
			}

			cancel()
		}()

		err := m.Run(ctx)
		assert.ErrorIs(t, err, context.Canceled)
		provider.AssertExpectations(t)
	})

	t.Run("triggers_on_confirmation_required_event", func(t *testing.T) {
		t.Parallel()

		provider := new(mockGuardianProvider)
		eventBus := bus.New()
		cfg := Config{
			AutoAcceptTypes: generic.NewSet(guard.ConfTypeTrade),
		}

		confs := []*guard.Confirmation{
			{ID: 4, Type: guard.ConfTypeTrade},
		}

		fetched := make(chan struct{})
		provider.On("FetchConfirmations", mock.Anything).Return(confs, nil).Run(func(args mock.Arguments) {
			close(fetched)
		}).Once()
		provider.On("AcceptMultiple", mock.Anything, confs).Return(nil).Once()

		m := New(provider, log.Discard, eventBus, cfg)

		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()

		go func() {
			time.Sleep(20 * time.Millisecond)
			eventBus.Publish(&guard.ConfirmationRequiredEvent{IsAppConfirm: true, TradeOfferID: "123"})

			select {
			case <-fetched:
			case <-time.After(1 * time.Second):
			}

			cancel()
		}()

		err := m.Run(ctx)
		assert.ErrorIs(t, err, context.Canceled)
		provider.AssertExpectations(t)
	})

	t.Run("filters_out_types_correctly", func(t *testing.T) {
		t.Parallel()

		provider := new(mockGuardianProvider)
		eventBus := bus.New()
		cfg := Config{
			AutoAcceptTypes: generic.NewSet(guard.ConfTypeMarket),
		}

		confs := []*guard.Confirmation{
			{ID: 5, Type: guard.ConfTypeTrade},
			{ID: 6, Type: guard.ConfTypeMarket},
		}

		fetched := make(chan struct{})
		provider.On("FetchConfirmations", mock.Anything).Return(confs, nil).Run(func(args mock.Arguments) {
			close(fetched)
		}).Once()
		provider.On("AcceptMultiple", mock.Anything, []*guard.Confirmation{confs[1]}).Return(nil).Once()

		m := New(provider, log.Discard, eventBus, cfg)

		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()

		go func() {
			time.Sleep(20 * time.Millisecond)
			eventBus.Publish(&auth.SteamGuardRequiredEvent{IsAppConfirm: true})

			select {
			case <-fetched:
			case <-time.After(1 * time.Second):
			}

			cancel()
		}()

		_ = m.Run(ctx)

		provider.AssertExpectations(t)
	})

	t.Run("handles_empty_fetch", func(t *testing.T) {
		t.Parallel()

		provider := new(mockGuardianProvider)
		eventBus := bus.New()
		cfg := Config{PollOnStart: true}

		provider.On("FetchConfirmations", mock.Anything).Return([]*guard.Confirmation{}, nil).Once()

		m := New(provider, log.Discard, eventBus, cfg)

		ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
		defer cancel()

		_ = m.Run(ctx)

		provider.AssertExpectations(t)
	})

	t.Run("handles_fetch_error_gracefully", func(t *testing.T) {
		t.Parallel()

		provider := new(mockGuardianProvider)
		eventBus := bus.New()
		cfg := Config{PollOnStart: true}

		provider.On("FetchConfirmations", mock.Anything).Return(nil, errors.New("network error")).Once()

		m := New(provider, log.Discard, eventBus, cfg)

		ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
		defer cancel()

		_ = m.Run(ctx)

		provider.AssertExpectations(t)
	})

	t.Run("handles_accept_multiple_error_gracefully", func(t *testing.T) {
		t.Parallel()

		provider := new(mockGuardianProvider)
		eventBus := bus.New()
		cfg := Config{
			AutoAcceptTypes: generic.NewSet(guard.ConfTypeTrade),
			PollOnStart:     true,
		}

		confs := []*guard.Confirmation{{ID: 7, Type: guard.ConfTypeTrade}}
		provider.On("FetchConfirmations", mock.Anything).Return(confs, nil).Once()
		provider.On("AcceptMultiple", mock.Anything, mock.Anything).Return(errors.New("steam error")).Once()

		m := New(provider, log.Discard, eventBus, cfg)

		ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
		defer cancel()

		_ = m.Run(ctx)

		provider.AssertExpectations(t)
	})

	t.Run("ignore_non_app_confirm_events", func(t *testing.T) {
		t.Parallel()

		provider := new(mockGuardianProvider)
		eventBus := bus.New()
		m := New(provider, log.Discard, eventBus, Config{})

		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()

		go func() {
			time.Sleep(10 * time.Millisecond)
			// Publish non-app-confirm events (should be ignored, no FetchConfirmations called)
			eventBus.Publish(&auth.SteamGuardRequiredEvent{IsAppConfirm: false})
			eventBus.Publish(&guard.ConfirmationRequiredEvent{IsAppConfirm: false})
			time.Sleep(10 * time.Millisecond)
			cancel()
		}()

		err := m.Run(ctx)
		assert.ErrorIs(t, err, context.Canceled)
		provider.AssertExpectations(t) // no mock calls expected
	})

	t.Run("empty_to_accept_filter_exit", func(t *testing.T) {
		t.Parallel()

		provider := new(mockGuardianProvider)
		eventBus := bus.New()
		cfg := Config{
			AutoAcceptTypes: generic.NewSet(guard.ConfTypeTrade),
		}

		confs := []*guard.Confirmation{{ID: 1, Type: guard.ConfTypeLogin}}

		m := New(provider, log.Discard, eventBus, cfg)

		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()

		fetched := make(chan struct{})
		provider.On("FetchConfirmations", mock.Anything).Return(confs, nil).Run(func(args mock.Arguments) {
			close(fetched)
		}).Once()

		go func() {
			time.Sleep(20 * time.Millisecond)
			eventBus.Publish(&auth.SteamGuardRequiredEvent{IsAppConfirm: true})

			select {
			case <-fetched:
			case <-time.After(1 * time.Second):
			}

			cancel()
		}()

		_ = m.Run(ctx)

		provider.AssertExpectations(t)
	})
}

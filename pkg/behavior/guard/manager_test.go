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

func TestManager_Run(t *testing.T) {
	t.Run("PollOnStart fetches and accepts", func(t *testing.T) {
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

		ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
		defer cancel()

		err := m.Run(ctx)
		assert.ErrorIs(t, err, context.DeadlineExceeded)
		provider.AssertExpectations(t)
	})

	t.Run("Triggers on SteamGuardRequiredEvent", func(t *testing.T) {
		provider := new(mockGuardianProvider)
		eventBus := bus.New()
		cfg := Config{
			AutoAcceptTypes: generic.NewSet(guard.ConfTypeLogin),
		}

		confs := []*guard.Confirmation{
			{ID: 3, Type: guard.ConfTypeLogin},
		}

		provider.On("FetchConfirmations", mock.Anything).Return(confs, nil).Once()
		provider.On("AcceptMultiple", mock.Anything, confs).Return(nil).Once()

		m := New(provider, log.Discard, eventBus, cfg)

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		go func() {
			// Give the behavior a moment to start and subscribe
			time.Sleep(20 * time.Millisecond)
			eventBus.Publish(&auth.SteamGuardRequiredEvent{IsAppConfirm: true})
			time.Sleep(20 * time.Millisecond)
			cancel()
		}()

		err := m.Run(ctx)
		assert.ErrorIs(t, err, context.Canceled)
		provider.AssertExpectations(t)
	})

	t.Run("Triggers on ConfirmationRequiredEvent", func(t *testing.T) {
		provider := new(mockGuardianProvider)
		eventBus := bus.New()
		cfg := Config{
			AutoAcceptTypes: generic.NewSet(guard.ConfTypeTrade),
		}

		confs := []*guard.Confirmation{
			{ID: 4, Type: guard.ConfTypeTrade},
		}

		provider.On("FetchConfirmations", mock.Anything).Return(confs, nil).Once()
		provider.On("AcceptMultiple", mock.Anything, confs).Return(nil).Once()

		m := New(provider, log.Discard, eventBus, cfg)

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		go func() {
			time.Sleep(20 * time.Millisecond)
			eventBus.Publish(&guard.ConfirmationRequiredEvent{IsAppConfirm: true, TradeOfferID: "123"})
			time.Sleep(20 * time.Millisecond)
			cancel()
		}()

		err := m.Run(ctx)
		assert.ErrorIs(t, err, context.Canceled)
		provider.AssertExpectations(t)
	})

	t.Run("Filters out types correctly", func(t *testing.T) {
		provider := new(mockGuardianProvider)
		eventBus := bus.New()
		cfg := Config{
			AutoAcceptTypes: generic.NewSet(guard.ConfTypeMarket),
		}

		confs := []*guard.Confirmation{
			{ID: 5, Type: guard.ConfTypeTrade},
			{ID: 6, Type: guard.ConfTypeMarket},
		}

		// It should only accept ID 6
		provider.On("FetchConfirmations", mock.Anything).Return(confs, nil).Once()
		provider.On("AcceptMultiple", mock.Anything, []*guard.Confirmation{confs[1]}).Return(nil).Once()

		m := New(provider, log.Discard, eventBus, cfg)

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		go func() {
			time.Sleep(20 * time.Millisecond)
			eventBus.Publish(&auth.SteamGuardRequiredEvent{IsAppConfirm: true})
			time.Sleep(20 * time.Millisecond)
			cancel()
		}()

		_ = m.Run(ctx)

		provider.AssertExpectations(t)
	})

	t.Run("Handles empty fetch", func(t *testing.T) {
		provider := new(mockGuardianProvider)
		eventBus := bus.New()
		cfg := Config{PollOnStart: true}

		provider.On("FetchConfirmations", mock.Anything).Return([]*guard.Confirmation{}, nil).Once()
		// AcceptMultiple should NOT be called

		m := New(provider, log.Discard, eventBus, cfg)

		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
		defer cancel()

		_ = m.Run(ctx)

		provider.AssertExpectations(t)
	})

	t.Run("Handles Fetch error gracefully", func(t *testing.T) {
		provider := new(mockGuardianProvider)
		eventBus := bus.New()
		cfg := Config{PollOnStart: true}

		provider.On("FetchConfirmations", mock.Anything).Return(nil, errors.New("network error")).Once()

		m := New(provider, log.Discard, eventBus, cfg)

		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
		defer cancel()

		_ = m.Run(ctx)

		provider.AssertExpectations(t)
	})

	t.Run("Handles AcceptMultiple error gracefully", func(t *testing.T) {
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

		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
		defer cancel()

		_ = m.Run(ctx)

		provider.AssertExpectations(t)
	})
}

// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package session

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"

	"github.com/lemon4ksan/g-man/pkg/bus"
	"github.com/lemon4ksan/g-man/pkg/log"
)

type mockSessionProvider struct {
	mock.Mock
}

func (m *mockSessionProvider) IsAuthenticated() bool {
	args := m.Called()
	return args.Bool(0)
}

func (m *mockSessionProvider) Verify(ctx context.Context) (bool, error) {
	args := m.Called(ctx)
	return args.Bool(0), args.Error(1)
}

func (m *mockSessionProvider) Refresh(ctx context.Context) error {
	args := m.Called(ctx)
	return args.Error(0)
}

func TestManager_Run(t *testing.T) {
	t.Run("Session Not Authenticated - Skips", func(t *testing.T) {
		provider := new(mockSessionProvider)
		eventBus := bus.New()
		cfg := Config{
			Interval: 10 * time.Millisecond,
		}

		provider.On("IsAuthenticated").Return(false)

		m := New(provider, log.Discard, eventBus, cfg)

		ctx, cancel := context.WithTimeout(context.Background(), 25*time.Millisecond)
		defer cancel()

		err := m.Run(ctx)
		assert.ErrorIs(t, err, context.DeadlineExceeded)

		provider.AssertExpectations(t)
	})

	t.Run("Session Alive - Does Not Refresh", func(t *testing.T) {
		provider := new(mockSessionProvider)
		eventBus := bus.New()
		cfg := Config{
			Interval: 10 * time.Millisecond,
		}

		provider.On("IsAuthenticated").Return(true)
		provider.On("Verify", mock.Anything).Return(true, nil)

		m := New(provider, log.Discard, eventBus, cfg)

		ctx, cancel := context.WithTimeout(context.Background(), 25*time.Millisecond)
		defer cancel()

		err := m.Run(ctx)
		assert.ErrorIs(t, err, context.DeadlineExceeded)

		provider.AssertExpectations(t)
	})

	t.Run("Session Expired - Triggers Refresh", func(t *testing.T) {
		provider := new(mockSessionProvider)
		eventBus := bus.New()
		cfg := Config{
			Interval: 10 * time.Millisecond,
		}

		provider.On("IsAuthenticated").Return(true)
		provider.On("Verify", mock.Anything).Return(false, nil).Once()
		provider.On("Verify", mock.Anything).Return(true, nil)
		provider.On("Refresh", mock.Anything).Return(nil).Once()

		m := New(provider, log.Discard, eventBus, cfg)

		ctx, cancel := context.WithTimeout(context.Background(), 25*time.Millisecond)
		defer cancel()

		err := m.Run(ctx)
		assert.ErrorIs(t, err, context.DeadlineExceeded)

		provider.AssertExpectations(t)
	})

	t.Run("Session Verify Fails - Triggers Refresh", func(t *testing.T) {
		provider := new(mockSessionProvider)
		eventBus := bus.New()
		cfg := Config{
			Interval: 10 * time.Millisecond,
		}

		provider.On("IsAuthenticated").Return(true)
		provider.On("Verify", mock.Anything).Return(false, errors.New("network error")).Once()
		provider.On("Verify", mock.Anything).Return(true, nil)
		provider.On("Refresh", mock.Anything).Return(nil).Once()

		m := New(provider, log.Discard, eventBus, cfg)

		ctx, cancel := context.WithTimeout(context.Background(), 25*time.Millisecond)
		defer cancel()

		err := m.Run(ctx)
		assert.ErrorIs(t, err, context.DeadlineExceeded)

		provider.AssertExpectations(t)
	})
}

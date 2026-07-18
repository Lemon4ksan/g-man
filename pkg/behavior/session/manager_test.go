// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package session

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/lemon4ksan/miyako/bus"
	"github.com/lemon4ksan/miyako/log"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"

	"github.com/lemon4ksan/g-man/pkg/behavior"
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

func TestKeepAlive(t *testing.T) {
	t.Parallel()

	t.Run("keepalive_and_name", func(t *testing.T) {
		t.Parallel()

		bBus := bus.New()
		logger := log.Discard
		orch := behavior.NewOrchestrator(bBus, logger)
		provider := new(mockSessionProvider)

		orch.Register(New(provider, logger, bBus, Config{}))
		assert.Equal(t, 1, orch.Count())

		m := New(provider, logger, bBus, Config{})
		assert.Equal(t, BehaviorName, m.Name())
	})

	t.Run("default_interval", func(t *testing.T) {
		t.Parallel()

		m := New(&mockSessionProvider{}, log.Discard, bus.New(), Config{})
		assert.Equal(t, 5*time.Minute, m.config.Interval)
	})
}

func TestManager_Run(t *testing.T) {
	t.Parallel()

	t.Run("session_not_authenticated_skips", func(t *testing.T) {
		t.Parallel()

		provider := new(mockSessionProvider)
		eventBus := bus.New()
		cfg := Config{
			Interval: 1 * time.Millisecond,
		}

		provider.On("IsAuthenticated").Return(false)

		m := New(provider, log.Discard, eventBus, cfg)

		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()

		// Start checking loop asynchronously
		go func() {
			_ = m.Run(ctx)
		}()

		// Give it a brief moment to tick and skip
		time.Sleep(15 * time.Millisecond)
		cancel()

		provider.AssertExpectations(t)
	})

	t.Run("session_alive_does_not_refresh", func(t *testing.T) {
		t.Parallel()

		provider := new(mockSessionProvider)
		eventBus := bus.New()
		cfg := Config{
			Interval: 1 * time.Millisecond,
		}

		verified := make(chan struct{})

		provider.On("IsAuthenticated").Return(true)
		provider.On("Verify", mock.Anything).Return(true, nil).Run(func(args mock.Arguments) {
			select {
			case verified <- struct{}{}:
			default:
			}
		})

		m := New(provider, log.Discard, eventBus, cfg)

		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()

		go func() {
			_ = m.Run(ctx)
		}()

		select {
		case <-verified:
			// Success!
		case <-time.After(1 * time.Second):
			t.Fatal("timeout waiting for verification")
		}

		cancel()

		provider.AssertExpectations(t)
	})

	t.Run("session_expired_triggers_refresh", func(t *testing.T) {
		t.Parallel()

		provider := new(mockSessionProvider)
		eventBus := bus.New()
		cfg := Config{
			Interval: 1 * time.Millisecond,
		}

		provider.On("IsAuthenticated").Return(true)
		provider.On("Verify", mock.Anything).Return(false, nil)

		refreshed := make(chan struct{}, 1)
		provider.On("Refresh", mock.Anything).Return(nil).Run(func(args mock.Arguments) {
			select {
			case refreshed <- struct{}{}:
			default:
			}
		})

		m := New(provider, log.Discard, eventBus, cfg)

		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()

		go func() {
			_ = m.Run(ctx)
		}()

		select {
		case <-refreshed:
			cancel()
		case <-time.After(1 * time.Second):
			t.Fatal("timeout waiting for session refresh")
		}

		provider.AssertExpectations(t)
	})

	t.Run("session_verify_fails_triggers_refresh", func(t *testing.T) {
		t.Parallel()

		provider := new(mockSessionProvider)
		eventBus := bus.New()
		cfg := Config{
			Interval: 1 * time.Millisecond,
		}

		provider.On("IsAuthenticated").Return(true)
		provider.On("Verify", mock.Anything).Return(false, errors.New("network error"))

		refreshed := make(chan struct{}, 1)
		provider.On("Refresh", mock.Anything).Return(nil).Run(func(args mock.Arguments) {
			select {
			case refreshed <- struct{}{}:
			default:
			}
		})

		m := New(provider, log.Discard, eventBus, cfg)

		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()

		go func() {
			_ = m.Run(ctx)
		}()

		select {
		case <-refreshed:
			cancel()
		case <-time.After(1 * time.Second):
			t.Fatal("timeout waiting for session refresh")
		}

		provider.AssertExpectations(t)
	})

	t.Run("session_expired_refresh_fails", func(t *testing.T) {
		t.Parallel()

		provider := new(mockSessionProvider)
		eventBus := bus.New()
		cfg := Config{
			Interval: 1 * time.Millisecond,
		}

		provider.On("IsAuthenticated").Return(true)
		provider.On("Verify", mock.Anything).Return(false, nil)

		refreshed := make(chan struct{}, 1)
		provider.On("Refresh", mock.Anything).Return(errors.New("refresh fail")).Run(func(args mock.Arguments) {
			select {
			case refreshed <- struct{}{}:
			default:
			}
		})

		m := New(provider, log.Discard, eventBus, cfg)

		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()

		go func() {
			_ = m.Run(ctx)
		}()

		select {
		case <-refreshed:
			cancel()
		case <-time.After(1 * time.Second):
			t.Fatal("timeout waiting for session refresh")
		}

		provider.AssertExpectations(t)
	})
}

// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package achievements

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/lemon4ksan/miyako/bus"
	"github.com/lemon4ksan/miyako/log"
	"github.com/stretchr/testify/assert"

	"github.com/lemon4ksan/g-man/pkg/behavior"
)

type mockProvider struct {
	mu           sync.Mutex
	achievements map[uint32]bool
	playedGames  []uint32
	awarded      chan uint32
	getErr       error
	getCanceled  context.CancelFunc
}

func (m *mockProvider) AwardAchievement(ctx context.Context, id uint32) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.achievements[id] = true
	if m.awarded != nil {
		select {
		case m.awarded <- id:
		default:
		}
	}

	return nil
}

func (m *mockProvider) GetCurrentAchievements(ctx context.Context) (map[uint32]bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.getErr != nil {
		if m.getCanceled != nil {
			m.getCanceled()
		}

		return nil, m.getErr
	}

	return m.achievements, nil
}

func (m *mockProvider) PlayGames(ctx context.Context, appIDs []uint32) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.playedGames = appIDs

	return nil
}

func TestAchievementManager_Lifecycle(t *testing.T) {
	t.Parallel()

	t.Run("simulate_and_name", func(t *testing.T) {
		t.Parallel()

		bBus := bus.New()
		logger := log.Discard
		orch := behavior.NewOrchestrator(bBus, logger)

		provider := &mockProvider{
			achievements: make(map[uint32]bool),
		}
		config := Config{
			AppID: 440,
		}

		Simulate(orch, provider, config)
		assert.Equal(t, 1, orch.Count())

		mgr := New(provider, config, logger)
		assert.Equal(t, BehaviorName, mgr.Name())
	})
}

func TestAchievementManager_Unlock(t *testing.T) {
	t.Parallel()

	t.Run("success_unlock", func(t *testing.T) {
		t.Parallel()

		awardedChan := make(chan uint32, 1)
		provider := &mockProvider{
			achievements: make(map[uint32]bool),
			awarded:      awardedChan,
		}

		config := Config{
			AppID:            440,
			TotalCount:       10,
			MinTargetPercent: 1.0, // Force 100% for test
			MaxTargetPercent: 1.0,
			UnlockChance:     1.0, // Force unlock
			CheckInterval:    10 * time.Millisecond,
			AchievementPool:  [][]uint32{{1, 5}},
		}

		mgr := New(provider, config, log.Discard)

		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()

		go func() {
			_ = mgr.Run(ctx)
		}()

		// Ожидание разблокировки без time.Sleep
		select {
		case <-awardedChan:
			// Успешно
		case <-time.After(1 * time.Second):
			t.Fatal("timeout waiting for achievement unlock")
		}

		provider.mu.Lock()
		count := len(provider.achievements)
		provider.mu.Unlock()

		assert.Greater(t, count, 0, "expected at least one achievement to be unlocked")
	})

	t.Run("zero_chances", func(t *testing.T) {
		t.Parallel()

		provider := &mockProvider{
			achievements: make(map[uint32]bool),
		}
		config := Config{
			AppID:            440,
			TotalCount:       10,
			MinTargetPercent: 1.0,
			MaxTargetPercent: 1.0,
			UnlockChance:     0.0, // never unlock
			BreakChance:      0.0, // never break
		}
		mgr := New(provider, config, log.Discard)

		ctx, cancel := context.WithCancel(t.Context())
		cancel() // выход

		err := mgr.Run(ctx)
		assert.ErrorIs(t, err, context.Canceled)
	})
}

func TestAchievementManager_Break(t *testing.T) {
	t.Parallel()

	t.Run("simulate_break", func(t *testing.T) {
		t.Parallel()

		provider := &mockProvider{
			achievements: make(map[uint32]bool),
			playedGames:  []uint32{440},
		}

		config := Config{
			AppID:         440,
			BreakChance:   1.0, // Force break
			CheckInterval: 10 * time.Millisecond,
		}

		mgr := New(provider, config, log.Discard)

		ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
		defer cancel()

		mgr.simulateBreak(ctx)

		provider.mu.Lock()
		isPlaying := len(provider.playedGames) > 0
		provider.mu.Unlock()

		assert.False(t, isPlaying, "expected to stop playing during break")
	})
}

func TestAchievementManager_Run_Errors(t *testing.T) {
	t.Parallel()

	t.Run("initial_delay_canceled", func(t *testing.T) {
		t.Parallel()

		provider := &mockProvider{
			achievements: make(map[uint32]bool),
		}
		config := Config{
			AppID:        440,
			InitialDelay: 5 * time.Second, // long delay
		}
		mgr := New(provider, config, log.Discard)

		ctx, cancel := context.WithCancel(t.Context())
		cancel() // Cancel context immediately

		err := mgr.Run(ctx)
		assert.ErrorIs(t, err, context.Canceled)
	})

	t.Run("get_achievements_error_canceled", func(t *testing.T) {
		t.Parallel()

		provider := &mockProvider{
			achievements: make(map[uint32]bool),
			getErr:       errors.New("steam api error"),
		}
		config := Config{
			AppID: 440,
		}
		mgr := New(provider, config, log.Discard)

		ctx, cancel := context.WithCancel(t.Context())
		provider.getCanceled = cancel

		err := mgr.Run(ctx)
		assert.ErrorIs(t, err, context.Canceled)
	})
}

func TestAchievementManager_Unlock_Errors(t *testing.T) {
	t.Parallel()

	t.Run("unlock_random_empty_pool", func(t *testing.T) {
		t.Parallel()

		mgr := New(&mockProvider{}, Config{}, log.Discard)
		assert.NotPanics(t, func() {
			mgr.unlockRandom(t.Context(), nil)
		})
	})

	t.Run("unlock_random_invalid_range_size", func(t *testing.T) {
		t.Parallel()

		mgr := New(&mockProvider{}, Config{
			AchievementPool: [][]uint32{{1}}, // only 1 element
		}, log.Discard)
		assert.NotPanics(t, func() {
			mgr.unlockRandom(t.Context(), nil)
		})
	})

	t.Run("unlock_random_already_unlocked", func(t *testing.T) {
		t.Parallel()

		provider := &mockProvider{
			achievements: map[uint32]bool{1: true},
		}
		mgr := New(provider, Config{
			AchievementPool: [][]uint32{{1, 1}}, // range [1, 1]
		}, log.Discard)

		assert.NotPanics(t, func() {
			mgr.unlockRandom(t.Context(), map[uint32]bool{1: true})
		})
	})
}

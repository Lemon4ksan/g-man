// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package achievements

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/lemon4ksan/g-man/pkg/log"
)

type mockProvider struct {
	mu           sync.Mutex
	achievements map[uint32]bool
	playedGames  []uint32
}

func (m *mockProvider) AwardAchievement(ctx context.Context, id uint32) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.achievements[id] = true

	return nil
}

func (m *mockProvider) GetCurrentAchievements(ctx context.Context) (map[uint32]bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.achievements, nil
}

func (m *mockProvider) PlayGames(ctx context.Context, appIDs []uint32) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.playedGames = appIDs

	return nil
}

func (m *mockProvider) GetLogger() log.Logger {
	return log.Discard
}

func TestAchievementManager_Unlock(t *testing.T) {
	provider := &mockProvider{
		achievements: make(map[uint32]bool),
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

	mgr := NewManager(provider, config, log.Discard)

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	go mgr.Run(ctx)

	// Wait for some unlocks
	time.Sleep(50 * time.Millisecond)

	provider.mu.Lock()
	count := len(provider.achievements)
	provider.mu.Unlock()

	if count == 0 {
		t.Error("Expected at least one achievement to be unlocked")
	}
}

func TestAchievementManager_Break(t *testing.T) {
	provider := &mockProvider{
		achievements: make(map[uint32]bool),
		playedGames:  []uint32{440},
	}

	config := Config{
		AppID:         440,
		BreakChance:   1.0, // Force break
		CheckInterval: 10 * time.Millisecond,
	}

	mgr := NewManager(provider, config, log.Discard)

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	// We'll test simulateBreak directly since it has a long sleep inside
	// or we can mock the break duration?
	// For now let's just check if it calls PlayGames(empty)

	mgr.simulateBreak(ctx)

	provider.mu.Lock()
	isPlaying := len(provider.playedGames) > 0
	provider.mu.Unlock()

	if isPlaying {
		t.Error("Expected to stop playing during break")
	}
}

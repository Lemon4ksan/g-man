// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package achievements provides the universal achievement manager behavior for the orchestrator.
package achievements

import (
	"context"
	"math/rand"
	"time"

	"github.com/lemon4ksan/miyako/generic"

	"github.com/lemon4ksan/g-man/pkg/behavior"
	"github.com/lemon4ksan/g-man/pkg/log"
)

// BehaviorName is the name of the behavior.
const BehaviorName = "achievements"

// Simulate registers the achievement manager behavior with the orchestrator.
func Simulate(orch *behavior.Orchestrator, provider Provider, cfg Config) {
	orch.Register(New(provider, cfg, orch.Logger()))
}

// Provider describes the interface for interaction with the game for the manager.
type Provider interface {
	AwardAchievement(ctx context.Context, id uint32) error
	GetCurrentAchievements(ctx context.Context) (map[uint32]bool, error)
	PlayGames(ctx context.Context, appIDs []uint32) error
}

// Config defines strategy config for achievement manager.
type Config struct {
	AppID            uint32
	TotalCount       int
	MinTargetPercent float64
	MaxTargetPercent float64
	UnlockChance     float32
	BreakChance      float32
	CheckInterval    time.Duration
	InitialDelay     time.Duration
	AchievementPool  [][]uint32 // Ranges of achievement IDs [start, end].
}

// Manager implements the universal strategy for obtaining achievements.
type Manager struct {
	provider Provider
	config   Config
	rng      *rand.Rand
	logger   log.Logger
}

// New creates a new achievement behavior.
func New(provider Provider, config Config, logger log.Logger) *Manager {
	return &Manager{
		provider: provider,
		config:   config,
		rng:      rand.New(rand.NewSource(time.Now().UnixNano())), // #nosec G404
		logger:   logger,
	}
}

// Name returns the name of the behavior.
func (m *Manager) Name() string {
	return BehaviorName
}

// Run implements the universal strategy for obtaining achievements.
func (m *Manager) Run(ctx context.Context) error {
	logger := m.logger.With(log.Uint32("app_id", m.config.AppID))
	logger.Info("Achievement Manager started")

	interval := generic.Coalesce(m.config.CheckInterval, 24*time.Hour)

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	// Wait a brief moment for games played status to propagate to Steam CM if configured
	if m.config.InitialDelay > 0 {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(m.config.InitialDelay):
		}
	}

	targetCount := int(
		float64(
			m.config.TotalCount,
		) * (m.config.MinTargetPercent + m.rng.Float64()*(m.config.MaxTargetPercent-m.config.MinTargetPercent)),
	)

	for {
		unlocked, err := m.provider.GetCurrentAchievements(ctx)
		if err != nil {
			logger.Error("Failed to fetch progress", log.Err(err))
			// Retry in 1 minute on failure instead of waiting for the full ticker
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(1 * time.Minute):
				continue
			}
		} else {
			currentCount := len(unlocked)
			logger.Info("Progress status", log.Int("current", currentCount), log.Int("target", targetCount))

			if currentCount < targetCount {
				if m.rng.Float32() < m.config.UnlockChance {
					m.unlockRandom(ctx, unlocked)
				}
			}
		}

		if m.rng.Float32() < m.config.BreakChance {
			m.simulateBreak(ctx)
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

func (m *Manager) unlockRandom(ctx context.Context, unlocked map[uint32]bool) {
	if len(m.config.AchievementPool) == 0 {
		return
	}

	r := m.config.AchievementPool[m.rng.Intn(len(m.config.AchievementPool))]
	id := r[0] + uint32(m.rng.Intn(int(r[1]-r[0]+1)))

	if unlocked[id] {
		return
	}

	m.logger.Info("Strategy: Unlocking achievement", log.Uint32("id", id))
	_ = m.provider.AwardAchievement(ctx, id)
}

func (m *Manager) simulateBreak(ctx context.Context) {
	duration := time.Duration(2+m.rng.Intn(4)) * time.Hour
	m.logger.Info("Strategy: Taking a break", log.Duration("duration", duration))

	_ = m.provider.PlayGames(ctx, []uint32{}) // Stop playing

	select {
	case <-ctx.Done():
	case <-time.After(duration):
		_ = m.provider.PlayGames(ctx, []uint32{m.config.AppID}) // Resume
	}
}

// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"context"
	"math/rand"
	"time"

	"github.com/lemon4ksan/miyako/log"

	"github.com/lemon4ksan/g-man/pkg/steam"
	"github.com/lemon4ksan/g-man/pkg/steam/sys/apps"
)

// HumanMimicryBehavior describes the account hour-boosting behavior model
type HumanMimicryBehavior struct {
	client *steam.Client
	logger log.Logger
	rng    *rand.Rand

	gamePool    []uint32
	startHour   int // Start of activity (e.g., 9 AM)
	endHour     int // End of activity (e.g., 11 PM)
	activeState bool
}

func NewHumanMimicryBehavior(
	client *steam.Client,
	gamePool []uint32,
	startHour, endHour int,
	logger log.Logger,
) *HumanMimicryBehavior {
	return &HumanMimicryBehavior{
		client:    client,
		logger:    logger.With(log.Module("mimicry")),
		rng:       rand.New(rand.NewSource(time.Now().UnixNano())), // #nosec G404
		gamePool:  gamePool,
		startHour: startHour,
		endHour:   endHour,
	}
}

func (h *HumanMimicryBehavior) Name() string {
	return "human_mimicry"
}

// Run contains the main lifecycle of the behavior
func (h *HumanMimicryBehavior) Run(ctx context.Context) error {
	h.logger.Info("Human Mimicry behavior started",
		log.Int("active_hours", h.startHour),
		log.Int("inactive_hours", h.endHour),
	)

	// Check status every 5 minutes
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	// Initialization step
	h.evaluateState(ctx)

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			h.evaluateState(ctx)
		}
	}
}

// evaluateState evaluates the time of day and determines activity
func (h *HumanMimicryBehavior) evaluateState(ctx context.Context) {
	now := time.Now()
	currentHour := now.Hour()

	appsMgr := apps.From(h.client)
	if appsMgr == nil {
		h.logger.Warn("Apps manager module is not ready")
		return
	}

	// Check if current time falls within the active window
	isWorkTime := false
	if h.startHour < h.endHour {
		isWorkTime = currentHour >= h.startHour && currentHour < h.endHour
	} else {
		// Nighttime activity spanning across midnight
		isWorkTime = currentHour >= h.startHour || currentHour < h.endHour
	}

	if isWorkTime {
		if !h.activeState {
			h.activeState = true
			h.logger.Info("Entering daily active state. Simulating gameplay...")

			// Simulate a random delay before "launching" games (10 to 120 seconds)
			h.randomSleep(ctx, 10, 120)

			// Select up to 3 random games from the available pool for idling
			selectedGames := h.selectRandomGames(3)
			h.logger.Info("Starting game idling session", log.Any("game_ids", selectedGames))

			if err := appsMgr.PlayGames(ctx, selectedGames, false); err != nil {
				h.logger.Error("Failed to update playing status", log.Err(err))
			}
		} else if h.rng.Float32() < 0.15 {
			h.logger.Info("Simulating game change session...")
			h.randomSleep(ctx, 15, 60)

			selectedGames := h.selectRandomGames(2)
			_ = appsMgr.PlayGames(ctx, selectedGames, false)
		}
	} else if h.activeState {
		h.activeState = false
		h.logger.Info("Entering nightly sleep state. Shutting down games...")

		// Simulate a random delay before exiting the game before sleep (1 to 15 minutes)
		h.randomSleep(ctx, 60, 900)

		if err := appsMgr.StopPlaying(ctx); err != nil {
			h.logger.Error("Failed to stop playing status", log.Err(err))
		}
	}
}

func (h *HumanMimicryBehavior) selectRandomGames(maxCount int) []uint32 {
	if len(h.gamePool) == 0 {
		return nil
	}

	// Shuffle a copy of the pool
	shuffled := make([]uint32, len(h.gamePool))
	copy(shuffled, h.gamePool)
	h.rng.Shuffle(len(shuffled), func(i, j int) {
		shuffled[i], shuffled[j] = shuffled[j], shuffled[i]
	})

	count := min(h.rng.Intn(maxCount)+1, len(shuffled))

	return shuffled[:count]
}

func (h *HumanMimicryBehavior) randomSleep(ctx context.Context, minSec, maxSec int) {
	duration := time.Duration(h.rng.Intn(maxSec-minSec)+minSec) * time.Second

	select {
	case <-ctx.Done():
	case <-time.After(duration):
	}
}

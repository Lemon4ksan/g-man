// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package crafting

import (
	"context"
	"time"

	"github.com/lemon4ksan/g-man/pkg/log"
	"github.com/lemon4ksan/g-man/pkg/tf2/schema"
)

// Automator is a high-level orchestrator that monitors the state of your
// backpack and automatically maintains metal reserves and weapon recrafting.
type Automator struct {
	manager *Manager
	logger  log.Logger

	minScrap int
	minRec   int
	maxScrap int
	maxRec   int
}

func NewAutomator(mgr *Manager, logger log.Logger) *Automator {
	return &Automator{
		manager:  mgr,
		logger:   logger,
		minScrap: 3,
		minRec:   3,
		maxScrap: 9,
		maxRec:   9,
	}
}

// Tick performs one check and one action (if needed).
// It is recommended to call it every few minutes or after each trade.
func (a *Automator) Tick(ctx context.Context) error {
	cache := a.manager.tf2.Cache()

	scrapCount := cache.GetMetalCount(DefIndexScrap)
	refCount := cache.GetMetalCount(DefIndexRefined)
	recCount := cache.GetMetalCount(DefIndexReclaimed)

	if scrapCount < a.minScrap && recCount > 0 {
		a.logger.Info("Scrap supply low, smelting Reclaimed")
		_, err := a.manager.SmeltMetal(ctx, DefIndexReclaimed)

		return err
	}

	if recCount < a.minRec && refCount > 0 {
		a.logger.Info("Reclaimed supply low, smelting Refined")
		_, err := a.manager.SmeltMetal(ctx, DefIndexRefined)

		return err
	}

	if scrapCount > a.maxScrap {
		a.logger.Info("Too much Scrap, combining into Reclaimed")
		_, err := a.manager.CombineMetal(ctx, DefIndexScrap)

		return err
	}

	if recCount > a.maxRec {
		a.logger.Info("Too much Reclaimed, combining into Refined")
		_, err := a.manager.CombineMetal(ctx, DefIndexReclaimed)

		return err
	}

	return nil
}

// CraftExcessWeapons finds duplicate weapons and crafts them into metal.
func (a *Automator) CleanInventory(ctx context.Context, s *schema.Schema) error {
	classes := []string{"Scout", "Soldier", "Pyro", "Demoman", "Heavy", "Engineer", "Medic", "Sniper", "Spy"}

	for _, class := range classes {
		for {
			weapons := a.manager.tf2.Cache().FindWeaponsByClass(s, class)
			if len(weapons) < 2 {
				break
			}

			a.logger.Info("Cleaning inventory: smelting class weapons", log.String("class", class))

			_, err := a.manager.SmeltClassWeapons(ctx, s, class)
			if err != nil {
				a.logger.Error("Failed to smelt class weapons", log.Err(err))
				break
			}

			time.Sleep(500 * time.Millisecond)
		}
	}

	_, err := a.manager.CondenseMetal(ctx)

	return err
}

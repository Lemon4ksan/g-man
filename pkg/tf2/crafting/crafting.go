// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package crafting

import (
	"context"
	"fmt"
	"time"

	"github.com/lemon4ksan/g-man/pkg/tf2"
	"github.com/lemon4ksan/g-man/pkg/tf2/schema"
)

// DefIndex of metals in TF2
const (
	DefIndexScrap     uint32 = 5000 // Scrap Metal
	DefIndexReclaimed uint32 = 5001 // Reclaimed Metal
	DefIndexRefined   uint32 = 5002 // Refined Metal
)

// IDs of basic crafting recipes in TF2 (Blueprints)
const (
	RecipeSmeltWeapons       int16 = 3   // 2 weapons of the same class -> 1 Scrap
	RecipeCombineScrap       int16 = 4   // 3 Scrap -> 1 Reclaimed
	RecipeCombineReclaimed   int16 = 5   // 3 Reclaimed -> 1 Refined
	RecipeSmeltReclaimed     int16 = 22  // 1 Reclaimed -> 3 Scrap
	RecipeSmeltRefined       int16 = 23  // 1 Refined -> 3 Reclaimed
	RecipeRebuildHeadgear    int16 = 8   // 2 hats -> 1 random hat
	RecipeFabricateToken     int16 = 6   // 3 weapons -> 1 class token
	RecipeFabricateSlotToken int16 = 7   // 3 weapons -> 1 slot token
	RecipeCustomDynamic      int16 = 200 // Used for Killstreak Fabricators / Chemistry Sets
)

type Manager struct {
	tf2 *tf2.TF2
}

func NewManager(tf2 *tf2.TF2) *Manager {
	return &Manager{tf2: tf2}
}

// CombineMetal automatically converts 3 units of low-grade metal into 1 high-grade metal.
// For example: 3 Scrap -> 1 Reclaimed. Returns the ID of the created metal.
func (cm *Manager) CombineMetal(ctx context.Context, metalDefIndex uint32) ([]uint64, error) {
	items := cm.tf2.Cache().FindCraftableItems(metalDefIndex, 3)
	if len(items) < 3 {
		return nil, fmt.Errorf("craft: not enough metal with defindex %d (need 3, got %d)", metalDefIndex, len(items))
	}

	var recipe int16
	switch metalDefIndex {
	case DefIndexScrap:
		recipe = RecipeCombineScrap
	case DefIndexReclaimed:
		recipe = RecipeCombineReclaimed
	default:
		return nil, fmt.Errorf("craft: invalid metal defindex for combination: %d", metalDefIndex)
	}

	return cm.tf2.Craft(ctx, items, recipe)
}

// SmeltMetal "smelts" 1 high-grade metal into 3 low-grade metals.
// For example: 1 Refined -> 3 Reclaimed. Returns the IDs of the created metals.
func (cm *Manager) SmeltMetal(ctx context.Context, metalDefIndex uint32) ([]uint64, error) {
	items := cm.tf2.Cache().FindCraftableItems(metalDefIndex, 1)
	if len(items) == 0 {
		return nil, fmt.Errorf("craft: no metal found with defindex %d", metalDefIndex)
	}

	var recipe int16
	switch metalDefIndex {
	case DefIndexReclaimed:
		recipe = RecipeSmeltReclaimed
	case DefIndexRefined:
		recipe = RecipeSmeltRefined
	default:
		return nil, fmt.Errorf("craft: invalid metal defindex for smelting: %d", metalDefIndex)
	}

	return cm.tf2.Craft(ctx, items, recipe)
}

// SmeltWeapons crafts two weapons of the same class into one Scrap Metal.
// Note: Both weapons must be of the same class in TF2, otherwise the GC will reject the request.
func (cm *Manager) SmeltWeapons(ctx context.Context, weaponID1, weaponID2 uint64) ([]uint64, error) {
	return cm.tf2.Craft(ctx, []uint64{weaponID1, weaponID2}, RecipeSmeltWeapons)
}

// CondenseMetal automatically scans your inventory and "compresses" all available metal:
// All Scrap items (3 each) are converted to Reclaimed, and all Reclaimed items (3 each) are converted to Refined.
// Returns the number of successful crafting operations.
func (cm *Manager) CondenseMetal(ctx context.Context) (int, error) {
	crafts := 0

	for cm.tf2.Cache().GetMetalCount(DefIndexScrap) >= 3 {
		if _, err := cm.CombineMetal(ctx, DefIndexScrap); err != nil {
			return crafts, fmt.Errorf("condense scrap failed after %d crafts: %w", crafts, err)
		}
		crafts++
		time.Sleep(300 * time.Millisecond)
	}

	for cm.tf2.Cache().GetMetalCount(DefIndexReclaimed) >= 3 {
		if _, err := cm.CombineMetal(ctx, DefIndexReclaimed); err != nil {
			return crafts, fmt.Errorf("condense reclaimed failed after %d crafts: %w", crafts, err)
		}
		crafts++
		time.Sleep(300 * time.Millisecond)
	}

	return crafts, nil
}

// MakeChange will smelt higher-grade metal until the target is reached.
// Example: MakeChange(ctx, DefIndexScrap, 1) - if there is no scrap, it will smelt 1 Rec.
func (cm *Manager) MakeChange(ctx context.Context, targetDefIndex uint32, targetCount int) error {
	for cm.tf2.Cache().GetMetalCount(targetDefIndex) < targetCount {
		switch targetDefIndex {
		case DefIndexScrap:
			if cm.tf2.Cache().GetMetalCount(DefIndexReclaimed) > 0 {
				if _, err := cm.SmeltMetal(ctx, DefIndexReclaimed); err != nil {
					return err
				}
			} else {
				if err := cm.MakeChange(ctx, DefIndexReclaimed, 1); err != nil {
					return err
				}
			}
		case DefIndexReclaimed:
			if cm.tf2.Cache().GetMetalCount(DefIndexRefined) > 0 {
				if _, err := cm.SmeltMetal(ctx, DefIndexRefined); err != nil {
					return err
				}
			} else {
				return fmt.Errorf("make_change: no refined metal left to smelt")
			}
		default:
			return fmt.Errorf("make_change: cannot smelt this item type")
		}

		time.Sleep(500 * time.Millisecond)
	}
	return nil
}

func (cm *Manager) SmeltClassWeapons(ctx context.Context, s *schema.Schema, class string) ([]uint64, error) {
	weapons := cm.tf2.Cache().FindWeaponsByClass(s, class)

	if len(weapons) < 2 {
		return nil, fmt.Errorf("not enough weapons for class %s", class)
	}

	itemsToCraft := []uint64{weapons[0].ID, weapons[1].ID}

	return cm.tf2.Craft(ctx, itemsToCraft, RecipeSmeltWeapons)
}

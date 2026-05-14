// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package crafting

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/lemon4ksan/g-man/pkg/tf2"
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

// InventoryProvider provides access to bot items and currency state.
type InventoryProvider interface {
	FindCraftableItems(defIndex uint32, count int) []uint64
	FindWeaponsByClass(class string) []*tf2.Item
	GetMetalCount(defIndex uint32) int
}

// GCProvider sends craft commands to Team Fortress 2 Game Coordinator.
type GCProvider interface {
	Craft(ctx context.Context, items []uint64, recipe int16) ([]uint64, error)
}

// Manager handles the crafting of items in TF2.
type Manager struct {
	inv InventoryProvider
	gc  GCProvider
}

// NewManager creates a new crafting manager.
func NewManager(inv InventoryProvider, gc GCProvider) *Manager {
	return &Manager{
		inv: inv,
		gc:  gc,
	}
}

// CombineMetal automatically converts 3 units of low-grade metal into 1 high-grade metal.
// For example: 3 Scrap -> 1 Reclaimed. Returns the ID of the created metal.
func (cm *Manager) CombineMetal(ctx context.Context, metalDefIndex uint32) ([]uint64, error) {
	items := cm.inv.FindCraftableItems(metalDefIndex, 3)
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

	return cm.gc.Craft(ctx, items, recipe)
}

// SmeltMetal "smelts" 1 high-grade metal into 3 low-grade metals.
// For example: 1 Refined -> 3 Reclaimed. Returns the IDs of the created metals.
func (cm *Manager) SmeltMetal(ctx context.Context, metalDefIndex uint32) ([]uint64, error) {
	items := cm.inv.FindCraftableItems(metalDefIndex, 1)
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

	return cm.gc.Craft(ctx, items, recipe)
}

// SmeltWeapons crafts two weapons of the same class into one Scrap Metal.
func (cm *Manager) SmeltWeapons(ctx context.Context, weaponID1, weaponID2 uint64) ([]uint64, error) {
	return cm.gc.Craft(ctx, []uint64{weaponID1, weaponID2}, RecipeSmeltWeapons)
}

// CondenseMetal automatically scans your inventory and "compresses" all available metal.
func (cm *Manager) CondenseMetal(ctx context.Context) (int, error) {
	crafts := 0

	for cm.inv.GetMetalCount(DefIndexScrap) >= 3 {
		if _, err := cm.CombineMetal(ctx, DefIndexScrap); err != nil {
			return crafts, fmt.Errorf("condense scrap failed after %d crafts: %w", crafts, err)
		}

		crafts++

		time.Sleep(300 * time.Millisecond)
	}

	for cm.inv.GetMetalCount(DefIndexReclaimed) >= 3 {
		if _, err := cm.CombineMetal(ctx, DefIndexReclaimed); err != nil {
			return crafts, fmt.Errorf("condense reclaimed failed after %d crafts: %w", crafts, err)
		}

		crafts++

		time.Sleep(300 * time.Millisecond)
	}

	return crafts, nil
}

// MakeChange will smelt higher-grade metal until the target is reached.
func (cm *Manager) MakeChange(ctx context.Context, targetDefIndex uint32, targetCount int) error {
	for cm.inv.GetMetalCount(targetDefIndex) < targetCount {
		switch targetDefIndex {
		case DefIndexScrap:
			if cm.inv.GetMetalCount(DefIndexReclaimed) > 0 {
				if _, err := cm.SmeltMetal(ctx, DefIndexReclaimed); err != nil {
					return err
				}
			} else {
				if err := cm.MakeChange(ctx, DefIndexReclaimed, 1); err != nil {
					return err
				}
			}

		case DefIndexReclaimed:
			if cm.inv.GetMetalCount(DefIndexRefined) > 0 {
				if _, err := cm.SmeltMetal(ctx, DefIndexRefined); err != nil {
					return err
				}
			} else {
				return errors.New("make_change: no refined metal left to smelt")
			}

		default:
			return errors.New("make_change: cannot smelt this item type")
		}

		time.Sleep(500 * time.Millisecond)
	}

	return nil
}

// SmeltClassWeapons finds two weapons of the same class and smelts them into scrap metal.
func (cm *Manager) SmeltClassWeapons(ctx context.Context, class string) ([]uint64, error) {
	weapons := cm.inv.FindWeaponsByClass(class)

	if len(weapons) < 2 {
		return nil, fmt.Errorf("not enough weapons for class %s", class)
	}

	itemsToCraft := []uint64{weapons[0].ID, weapons[1].ID}

	return cm.gc.Craft(ctx, itemsToCraft, RecipeSmeltWeapons)
}

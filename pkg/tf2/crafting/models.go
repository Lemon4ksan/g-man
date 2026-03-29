// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package crafting

import "context"

// PureCounts contains the amount of pure currency in the bot.
type PureCounts struct {
	Refined   int
	Reclaimed int
	Scrap     int
}

// InventoryProvider provides access to bot objects.
type InventoryProvider interface {
	GetItemCount(sku string) int
	GetPureCounts() PureCounts
}

// Price list Supplier today is the bot holder trading.
type PricelistProvider interface {
	HasPricedItem(sku string) bool
}

// GCProvider sends craft commands to Team Fortress 2 Game Coordinator.
type GCProvider interface {
	CombineClassWeapons(ctx context.Context, sku1, sku2 string) error
	CombineDuplicateWeapon(ctx context.Context, sku string) error
	CombineMetal(ctx context.Context, defindex int) error // 5000=Scrap->Rec, 5001=Rec->Ref
	SmeltMetal(ctx context.Context, defindex int) error   // 5002=Ref->Rec, 5001=Rec->Scrap
}

// ConfigProvider sets the crafting settings.
type ConfigProvider interface {
	IsWeaponsCraftingEnabled() bool
	IsMetalsCraftingEnabled() bool
	GetMetalThresholds() (minScrap, minRec, threshold int)
	GetCraftWeaponsByClass() map[string][]string
	GetAllCraftWeapons() []string
}

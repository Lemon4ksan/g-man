// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Crafting module provides abstractions for tf2 item crafting calculations.
package crafting

import (
	"context"
	"time"

	"github.com/lemon4ksan/g-man/pkg/log"
)

// Service is responsible for the automatic creation of weapons and metal smelting.
type Service struct {
	inv    InventoryProvider
	price  PricelistProvider
	gc     GCProvider
	cfg    ConfigProvider
	logger log.Logger
}

// NewService creates a new instance of the crafting service.
func NewService(inv InventoryProvider, price PricelistProvider, gc GCProvider, cfg ConfigProvider, logger log.Logger) *Service {
	return &Service{
		inv:    inv,
		price:  price,
		gc:     gc,
		cfg:    cfg,
		logger: logger,
	}
}

// KeepMetalSupply automatically forges and smelts metal to maintain limits.
func (s *Service) KeepMetalSupply(ctx context.Context) error {
	if !s.cfg.IsMetalsCraftingEnabled() {
		return nil
	}

	pure := s.inv.GetPureCounts()

	if pure.Refined <= 0 && pure.Reclaimed <= 3 && pure.Scrap <= 3 {
		return nil
	}

	minScrap, minRec, threshold := s.cfg.GetMetalThresholds()
	maxRec := minRec + threshold
	maxScrap := minScrap + threshold

	var smeltRefined, smeltReclaimed, combineScrap, combineReclaimed int

	if pure.Scrap > maxScrap {
		combineScrap = (pure.Scrap - maxScrap + 2) / 3
	} else if pure.Scrap < minScrap {
		smeltReclaimed = (minScrap - pure.Scrap + 2) / 3
	}

	projectedReclaimed := pure.Reclaimed + combineScrap - smeltReclaimed

	if projectedReclaimed > maxRec {
		if smeltReclaimed == 0 {
			combineReclaimed = (projectedReclaimed - maxRec + 2) / 3
		}
	} else if projectedReclaimed < minRec {
		if combineScrap == 0 {
			smeltRefined = (minRec - projectedReclaimed + 2) / 3
		}
	}

	for range combineScrap {
		s.logger.Debug("Combining 3 Scrap -> 1 Reclaimed")
		_ = s.gc.CombineMetal(ctx, 5000)
		if err := s.delay(ctx); err != nil {
			return err
		}
	}

	for range combineReclaimed {
		s.logger.Debug("Combining 3 Reclaimed -> 1 Refined")
		_ = s.gc.CombineMetal(ctx, 5001)
		if err := s.delay(ctx); err != nil {
			return err
		}
	}

	for range smeltRefined {
		s.logger.Debug("Smelting 1 Refined -> 3 Reclaimed")
		_ = s.gc.SmeltMetal(ctx, 5002)
		if err := s.delay(ctx); err != nil {
			return err
		}
	}

	for range smeltReclaimed {
		s.logger.Debug("Smelting 1 Reclaimed -> 3 Scrap")
		_ = s.gc.SmeltMetal(ctx, 5001)
		if err := s.delay(ctx); err != nil {
			return err
		}
	}

	return nil
}

// CraftClassWeapons crafts two different weapons of the same class into scrap.
func (s *Service) CraftClassWeapons(ctx context.Context) error {
	if !s.cfg.IsWeaponsCraftingEnabled() {
		return nil
	}

	for class, weapons := range s.cfg.GetCraftWeaponsByClass() {
		if err := s.craftEachClassWeapons(ctx, class, weapons); err != nil {
			return err
		}
	}

	return nil
}

func (s *Service) CraftDuplicateWeapons(ctx context.Context) error {
	if !s.cfg.IsWeaponsCraftingEnabled() {
		return nil
	}

	allWeapons := s.cfg.GetAllCraftWeapons()

	for _, sku := range allWeapons {
		count := s.inv.GetItemCount(sku)

		if count < 2 || s.price.HasPricedItem(sku) {
			continue
		}

		combinations := count / 2

		for range combinations {
			s.logger.Debug("Crafting duplicate weapon", log.String("sku", sku))

			if err := s.gc.CombineDuplicateWeapon(ctx, sku); err != nil {
				return err
			}

			if err := s.delay(ctx); err != nil {
				return err
			}
		}
	}

	return nil
}

func (s *Service) delay(ctx context.Context) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(200 * time.Millisecond):
		return nil
	}
}

func (s *Service) craftEachClassWeapons(ctx context.Context, className string, weapons []string) error {
	count := len(weapons)

	for i := range count {
		wep1 := weapons[i]

		if s.inv.GetItemCount(wep1) != 1 || s.price.HasPricedItem(wep1) {
			continue
		}

		for j := range count {
			if i == j {
				continue
			}

			wep2 := weapons[j]

			if s.inv.GetItemCount(wep2) != 1 || s.price.HasPricedItem(wep2) {
				continue
			}

			s.logger.Debug("Crafting class weapons",
				log.String("class_name", className),
				log.String("weapon_1", wep1),
				log.String("weapon_2", wep2),
			)

			if err := s.gc.CombineClassWeapons(ctx, wep1, wep2); err != nil {
				return err
			}

			if err := s.delay(ctx); err != nil {
				return err
			}

			return nil
		}
	}
	return nil
}

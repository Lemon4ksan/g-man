// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package crafting

import (
	"context"
	"errors"
	"fmt"

	"github.com/lemon4ksan/g-man/pkg/log"
	"github.com/lemon4ksan/g-man/pkg/tf2/currency"
)

var ErrNotEnoughChange = errors.New("tf2econ: not enough pure metal to make exact change")

type AssetFetcher interface {
	GetAssetIDs(sku string) []uint64
	GetPureStock() currency.PureStock
}

type MetalManager struct {
	fetcher AssetFetcher
	logger  log.Logger
	craft   *Manager
}

func NewMetalManager(fetcher AssetFetcher, craft *Manager, logger log.Logger) *MetalManager {
	return &MetalManager{fetcher: fetcher, craft: craft, logger: logger}
}

// SelectMetal selects a metal for exchange.
// If there is no exact exchange, it tries to craft it.
func (m *MetalManager) SelectMetal(ctx context.Context, needed currency.Scrap) ([]uint64, error) {
	if needed <= 0 {
		return nil, nil
	}

	selected, remaining := m.greedySelect(int(needed))
	if remaining > 0 {
		if err := m.craft.MakeChange(ctx, DefIndexScrap, remaining); err != nil {
			return nil, err
		}

		selected, remaining = m.greedySelect(int(needed))
	}

	if remaining > 0 {
		return nil, fmt.Errorf("not enough metal: missing %d scrap", remaining)
	}

	return selected, nil
}

// SelectChange collects an array of AssetIDs whose sum is exactly equal to amountScrap.
// Uses a greedy algorithm, trying to take large denominations (Ref) first,
// but automatically "exchanges" them for smaller ones if there are no large ones
// (for example, using 3 Rec instead of 1 Ref).
func (m *MetalManager) SelectChange(amount currency.Scrap) ([]uint64, error) {
	if amount <= 0 {
		return nil, nil
	}

	availableRef := m.fetcher.GetAssetIDs(currency.SKURefined)
	availableRec := m.fetcher.GetAssetIDs(currency.SKUReclaimed)
	availableScrap := m.fetcher.GetAssetIDs(currency.SKUScrap)

	var selected []uint64

	needed := amount

	for needed >= 9 && len(availableRef) > 0 {
		selected = append(selected, availableRef[0])
		availableRef = availableRef[1:]
		needed -= 9
	}

	for needed >= 3 && len(availableRec) > 0 {
		selected = append(selected, availableRec[0])
		availableRec = availableRec[1:]
		needed -= 3
	}

	for needed >= 1 && len(availableScrap) > 0 {
		selected = append(selected, availableScrap[0])
		availableScrap = availableScrap[1:]
		needed -= 1
	}

	if needed > 0 {
		return nil, ErrNotEnoughChange
	}

	return selected, nil
}

func (m *MetalManager) SelectKeysAndMetal(keys int, metal currency.Scrap) ([]uint64, error) {
	var selected []uint64

	if keys > 0 {
		availableKeys := m.fetcher.GetAssetIDs(currency.SKUKey)
		if len(availableKeys) < keys {
			return nil, errors.New("tf2econ: not enough keys in inventory")
		}

		selected = append(selected, availableKeys[:keys]...)
	}

	if metal > 0 {
		metalIDs, err := m.SelectChange(metal)
		if err != nil {
			return nil, err
		}

		selected = append(selected, metalIDs...)
	}

	return selected, nil
}

// SelectFinalInventory finds keys and metal for the offer.
func (m *MetalManager) SelectFinalInventory(ctx context.Context, keys int, metal currency.Scrap) ([]uint64, error) {
	var total []uint64

	if keys > 0 {
		availableKeys := m.fetcher.GetAssetIDs(currency.SKUKey)
		if len(availableKeys) < keys {
			return nil, fmt.Errorf("not enough keys: need %d, have %d", keys, len(availableKeys))
		}

		total = append(total, availableKeys[:keys]...)
	}

	if metal > 0 {
		metalIDs, err := m.SelectMetal(ctx, metal)
		if err != nil {
			return nil, err
		}

		total = append(total, metalIDs...)
	}

	return total, nil
}

func (m *MetalManager) greedySelect(needed int) (selected []uint64, remaining int) {
	ref := m.fetcher.GetAssetIDs(currency.SKURefined)
	rec := m.fetcher.GetAssetIDs(currency.SKUReclaimed)
	scrap := m.fetcher.GetAssetIDs(currency.SKUScrap)

	current := needed

	for current >= 9 && len(ref) > 0 {
		selected = append(selected, ref[0])
		ref = ref[1:]
		current -= 9
	}

	for current >= 3 && len(rec) > 0 {
		selected = append(selected, rec[0])
		rec = rec[1:]
		current -= 3
	}

	for current >= 1 && len(scrap) > 0 {
		selected = append(selected, scrap[0])
		scrap = scrap[1:]
		current -= 1
	}

	return selected, current
}

// TryToSmeltForChange checks whether the required amount can be collected from the current metal.
// If an exact exchange is impossible (for example, 1 scrap is needed, but only Refined is available),
// the method initiates the smelting chain via the CraftingManager.
func (m *MetalManager) TryToSmeltForChange(ctx context.Context, needed currency.Scrap) error {
	stock := m.fetcher.GetPureStock()
	totalValue := stock.TotalScrap()

	if totalValue < needed {
		return fmt.Errorf("tf2econ: insufficient total metal value (have %d, need %d)", totalValue, needed)
	}

	_, remaining := m.greedySelect(int(needed))
	if remaining == 0 {
		return nil
	}

	m.logger.Info("Attempting to break metal for exact change",
		log.Int("needed_scrap", remaining),
		log.Int("total_requested", int(needed)),
	)

	if err := m.craft.MakeChange(ctx, DefIndexScrap, remaining); err != nil {
		return fmt.Errorf("tf2econ: smelting failed: %w", err)
	}

	_, finalRemaining := m.greedySelect(int(needed))
	if finalRemaining > 0 {
		return fmt.Errorf("tf2econ: smelting didn't resolve the change problem, still need %d scrap", finalRemaining)
	}

	return nil
}

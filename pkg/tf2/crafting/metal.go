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

// ErrNotEnoughChange is returned when there is not enough pure metal to make exact change.
var ErrNotEnoughChange = errors.New("tf2econ: not enough pure metal to make exact change")

// AssetFetcher provides access to the asset storage.
type AssetFetcher interface {
	GetAssetIDs(sku string) []uint64
	GetPureStock() currency.PureStock
}

// MetalManager manages the selection and crafting of metal.
type MetalManager struct {
	fetcher AssetFetcher
	logger  log.Logger
	craft   *Manager
}

// NewMetalManager creates a new metal manager.
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
func (m *MetalManager) SelectChange(amount currency.Scrap) ([]uint64, error) {
	selected, remaining := m.greedySelect(int(amount))
	if remaining > 0 {
		return nil, ErrNotEnoughChange
	}

	return selected, nil
}

// SelectKeysAndMetal selects keys and metal for the offer.
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

func (m *MetalManager) greedySelect(needed int) (selected []uint64, remaining int) {
	ref := m.fetcher.GetAssetIDs(currency.SKURefined)
	rec := m.fetcher.GetAssetIDs(currency.SKUReclaimed)
	scrap := m.fetcher.GetAssetIDs(currency.SKUScrap)

	current := needed

	// Helper to pick items until needed amount is covered or we run out of items
	pick := func(items []uint64, value int) {
		for current >= value && len(items) > 0 {
			selected = append(selected, items[0])
			items = items[1:]
			current -= value
		}
	}

	pick(ref, 9)
	pick(rec, 3)
	pick(scrap, 1)

	return selected, current
}

// TryToSmeltForChange checks whether the required amount can be collected from the current metal.
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

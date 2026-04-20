// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package bptf

import (
	"context"
	"fmt"
	"strconv"
	"sync"

	"github.com/lemon4ksan/g-man/pkg/log"
	schema "github.com/lemon4ksan/g-man/pkg/tf2/schema"
	"github.com/lemon4ksan/g-man/pkg/tf2/sku"
)

type PriceManager struct {
	client *Client
	logger log.Logger

	mu    sync.RWMutex
	index map[string]PriceEntry
}

func NewPriceManager(c *Client, l log.Logger) *PriceManager {
	return &PriceManager{
		client: c,
		logger: l.With(log.Module("bptf_prices")),
		index:  make(map[string]PriceEntry),
	}
}

// Update downloads the price list and rebuilds the index.
func (m *PriceManager) Update(ctx context.Context) error {
	m.logger.Debug("Fetching full pricelist from backpack.tf...")

	res, err := m.client.GetPricesV4(ctx, 1, 0)
	if err != nil {
		return fmt.Errorf("bptf update failed: %w", err)
	}

	newIndex := make(map[string]PriceEntry)

	// Quality -> Tradability -> Craftability -> PriceIndex -> PriceEntry.
	for _, itemData := range res.Items {
		// An item can have multiple defindexes (Valve shenanigans)
		// We'll take the first one, as they usually overlap for different styles.
		if len(itemData.Defindexes) == 0 {
			continue
		}

		defindex, _ := strconv.Atoi(itemData.Defindexes[0])

		for quality, tradableMap := range itemData.Prices {
			qInt, _ := strconv.Atoi(quality)

			for tradable, craftableMap := range tradableMap {
				isTradable := tradable == "Tradable"

				for craftable, priceIndexMap := range craftableMap {
					isCraftable := craftable == "Craftable"

					for pIndex, entry := range priceIndexMap {
						sItem := &sku.Item{
							Defindex:  defindex,
							Quality:   qInt,
							Tradable:  isTradable,
							Craftable: isCraftable,
						}

						if pInt, err := strconv.Atoi(pIndex); err == nil && pInt != 0 {
							if qInt == schema.QualityUnusual {
								sItem.Effect = pInt
							} else {
								sItem.Crateseries = pInt
							}
						}

						skuStr := sku.FromObject(sItem)
						newIndex[skuStr] = entry
					}
				}
			}
		}
	}

	m.mu.Lock()
	m.index = newIndex
	m.mu.Unlock()

	m.logger.Info("Bptf index rebuilt", log.Int("unique_skus", len(newIndex)))

	return nil
}

// GetPrice returns the price for the SKU from memory.
func (m *PriceManager) GetPrice(sku string) (PriceEntry, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	entry, ok := m.index[sku]

	return entry, ok
}

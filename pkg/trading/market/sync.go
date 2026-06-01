// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package market

import (
	"context"
	"fmt"
	"sync"

	"github.com/lemon4ksan/g-man/pkg/log"
	"github.com/lemon4ksan/g-man/pkg/trading"
)

// ItemProvider abstracts G-man's game-specific local inventory state.
type ItemProvider interface {
	GetItems() []*trading.Item
	IsItemLocked(assetID uint64) bool
	LockItems(ids []uint64)
	UnlockItems(ids []uint64)
}

// ConfigProvider defines the contract to retrieve Min/Max stock and pricing per adapter.
type ConfigProvider interface {
	GetMaxStock(sku, adapter string) int
	GetMinStock(sku, adapter string) int
	GetTargetPrice(sku, adapter string) (Price, bool)
}

// Synchronizer is the master coordinator managing multiple adapters and multiple
// game-specific inventories (TF2 SOCache, CS2 Web, etc.) simultaneously.
type Synchronizer struct {
	mu          sync.Mutex
	adapters    []Adapter
	inventories map[uint32]ItemProvider // Maps Steam AppID -> Game Inventory Provider
	config      ConfigProvider
	logger      log.Logger

	// allocatedAssets tracks which asset is currently listed on which adapter:
	// assetID -> adapterName
	allocatedAssets map[uint64]string
}

// NewSynchronizer instantiates the master market synchronizer.
func NewSynchronizer(cfg ConfigProvider, logger log.Logger, adapters ...Adapter) *Synchronizer {
	return &Synchronizer{
		adapters:        adapters,
		inventories:     make(map[uint32]ItemProvider),
		config:          cfg,
		logger:          logger.With(log.Module("market_sync")),
		allocatedAssets: make(map[uint64]string),
	}
}

// RegisterInventory links a game-specific inventory provider (e.g. AppID 440 for TF2, 730 for CS2).
func (s *Synchronizer) RegisterInventory(appID uint32, provider ItemProvider) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.inventories[appID] = provider
	s.logger.Info("Registered game inventory provider", log.Uint32("app_id", appID))
}

// ReconcileAll runs the declarative synchronization for all registered adapters.
func (s *Synchronizer) ReconcileAll(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.logger.Info("Starting global market synchronization cycle")

	for _, adapter := range s.adapters {
		if err := s.reconcileAdapter(ctx, adapter); err != nil {
			s.logger.Error("Failed to reconcile adapter", log.String("adapter", adapter.Name()), log.Err(err))
		}
	}

	return nil
}

func (s *Synchronizer) reconcileAdapter(ctx context.Context, adapter Adapter) error {
	adapterName := adapter.Name()
	gameID := uint32(adapter.GameID())

	invProvider, exists := s.inventories[gameID]
	if !exists {
		return fmt.Errorf("no inventory provider registered for game ID %d", gameID)
	}

	remoteListings, err := adapter.GetListings(ctx)
	if err != nil {
		return fmt.Errorf("failed to fetch remote listings: %w", err)
	}

	localItems := invProvider.GetItems()

	var availableItems []*trading.Item
	for _, it := range localItems {
		if it.AppID != gameID {
			continue
		}

		if invProvider.IsItemLocked(it.AssetID) {
			continue
		}

		allocatedTo, isAllocated := s.allocatedAssets[it.AssetID]
		if isAllocated && allocatedTo != adapterName {
			continue // Already listed elsewhere, skip to prevent double-selling
		}

		availableItems = append(availableItems, it)
	}

	listingsBySKU := make(map[string][]Listing)
	for _, l := range remoteListings {
		listingsBySKU[l.SKU] = append(listingsBySKU[l.SKU], l)

		if _, allocated := s.allocatedAssets[l.AssetID]; !allocated {
			s.allocatedAssets[l.AssetID] = adapterName
		}
	}

	itemsBySKU := make(map[string][]*trading.Item)
	for _, it := range availableItems {
		itemsBySKU[it.SKU] = append(itemsBySKU[it.SKU], it)
	}

	allSKUs := make(map[string]bool)
	for sku := range itemsBySKU {
		allSKUs[sku] = true
	}

	for sku := range listingsBySKU {
		allSKUs[sku] = true
	}

	for sku := range allSKUs {
		items := itemsBySKU[sku]
		activeListings := listingsBySKU[sku]

		targetPrice, hasPrice := s.config.GetTargetPrice(sku, adapterName)
		if !hasPrice {
			continue
		}

		minStock := s.config.GetMinStock(sku, adapterName)

		totalStock := len(items)
		allowedToSell := max(totalStock-minStock, 0)
		currentListedCount := len(activeListings)

		// Case A: Over-listed -> Withdraw surplus
		if currentListedCount > allowedToSell {
			toRemove := currentListedCount - allowedToSell

			var idsToDelete []string
			for i := range toRemove {
				idsToDelete = append(idsToDelete, activeListings[i].ID)
				delete(s.allocatedAssets, activeListings[i].AssetID)
			}

			if err := adapter.DeleteListings(ctx, idsToDelete); err != nil {
				s.logger.Error("Failed to delete listings", log.String("adapter", adapterName), log.Err(err))
			}

			activeListings = activeListings[toRemove:]
		}

		// Case B: Under-listed -> List missing items
		if len(activeListings) < allowedToSell {
			needed := allowedToSell - len(activeListings)

			var unlisted []*trading.Item
			for _, it := range items {
				if _, listed := s.allocatedAssets[it.AssetID]; !listed {
					unlisted = append(unlisted, it)
				}
			}

			if needed > len(unlisted) {
				needed = len(unlisted)
			}

			var createRequests []ListingRequest
			for i := range needed {
				targetItem := unlisted[i]
				createRequests = append(createRequests, ListingRequest{
					AssetID: targetItem.AssetID,
					SKU:     targetItem.SKU,
					Price:   targetPrice,
				})
				s.allocatedAssets[targetItem.AssetID] = adapterName
			}

			if len(createRequests) > 0 {
				if err := adapter.CreateListings(ctx, createRequests); err != nil {
					s.logger.Error("Failed to create listings", log.String("adapter", adapterName), log.Err(err))
				}
			}
		}

		// Case C: Price Desynchronization -> Adjust active prices
		var updateRequests []ListingRequest
		for _, l := range activeListings {
			if l.Price != targetPrice {
				updateRequests = append(updateRequests, ListingRequest{
					AssetID: l.AssetID,
					SKU:     l.SKU,
					Price:   targetPrice,
				})
			}
		}

		if len(updateRequests) > 0 {
			if err := adapter.UpdateListings(ctx, updateRequests); err != nil {
				s.logger.Error("Failed to update listings", log.String("adapter", adapterName), log.Err(err))
			}
		}
	}

	return nil
}

// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package inventory

import (
	"context"
	"fmt"
	"reflect"
	"sync"

	"github.com/lemon4ksan/g-man/pkg/log"
	"github.com/lemon4ksan/g-man/pkg/steam/service"
	"github.com/lemon4ksan/g-man/pkg/tf2/schema"
)

// PlayerInventory represents a specific player's TF2 inventory.
// Data is lazy-loaded on the first request.
type PlayerInventory struct {
	steamID uint64
	client  service.Doer
	schema  *schema.Schema
	logger  log.Logger

	dupeCheckers []DupeChecker

	mu      sync.Mutex
	items   []TF2Item
	slots   int
	fetched bool
}

// New creates an inventory for a specific player.
// dupeCheckers is a slice of implementations (e.g. [NewBackpackTFChecker]).
func New(steamID uint64, client service.Doer, dupeCheckers ...DupeChecker) *PlayerInventory {
	return &PlayerInventory{
		steamID:      steamID,
		client:       client,
		logger:       log.Discard,
		dupeCheckers: dupeCheckers,
		items:        make([]TF2Item, 0),
	}
}

func (inv *PlayerInventory) WithLogger(l log.Logger) *PlayerInventory {
	inv.logger = l
	return inv
}

// GetItemsBySKU returns all assets in someone else's inventory that match the specified SKU.
// This is necessary to check: "Did the partner actually offer the item we agreed on?"
func (inv *PlayerInventory) GetItemsBySKU(ctx context.Context, targetSKU string) ([]TF2Item, error) {
	inv.mu.Lock()
	if !inv.fetched {
		if err := inv.fetch(ctx); err != nil {
			inv.mu.Unlock()
			return nil, err
		}
	}

	items := inv.items
	inv.mu.Unlock()

	var result []TF2Item

	for _, it := range items {
		if it.ToSKU() == targetSKU {
			result = append(result, it)
		}
	}

	return result, nil
}

// IsDuped checks whether the item is a duplicate.
// It queries all registered DupeCheckers in turn.
// If at least one service considers the item a duplicate, true is returned.
// If no service knows about the item, nil is returned.
func (inv *PlayerInventory) IsDuped(ctx context.Context, assetID uint64) (*bool, error) {
	duped, recorded, err := inv.checkWithServices(ctx, assetID)
	if err != nil {
		return nil, err
	}

	if recorded {
		return &duped, nil
	}

	inv.mu.Lock()
	if !inv.fetched {
		if err := inv.fetch(ctx); err != nil {
			inv.mu.Unlock()
			return nil, err
		}
	}

	items := inv.items
	inv.mu.Unlock()

	var targetItem *TF2Item

	for i := range items {
		if items[i].ID == assetID {
			targetItem = &items[i]
			break
		}
	}

	if targetItem == nil {
		return nil, ErrItemNotFound
	}

	duped, recorded, err = inv.checkWithServices(ctx, targetItem.OriginalID)
	if err != nil {
		return nil, err
	}

	if recorded {
		return &duped, nil
	}

	return nil, nil
}

func (inv *PlayerInventory) checkWithServices(
	ctx context.Context,
	assetID uint64,
) (isDuped, isRecorded bool, err error) {
	for _, checker := range inv.dupeCheckers {
		status, checkErr := checker.CheckHistory(ctx, assetID)
		if checkErr != nil {
			inv.logger.Warn("Dupe checker failed",
				log.String("service", reflect.TypeOf(checker).Name()),
				log.Err(checkErr),
			)

			continue
		}

		if status.Recorded {
			isRecorded = true

			if status.IsDuped {
				isDuped = true
				return isDuped, isRecorded, err
			}
		}
	}

	return isDuped, isRecorded, err
}

func (inv *PlayerInventory) fetch(ctx context.Context) error {
	req := &struct {
		SteamID uint64 `url:"steamid"`
	}{inv.steamID}

	resp, err := service.WebAPI[PlayerItemsResponse](ctx, inv.client, "GET", "IEconItems_440", "GetPlayerItems", 1, req)
	if err != nil {
		return fmt.Errorf("failed to fetch TF2 items: %w", err)
	}

	if resp.Result.Status != 1 {
		return fmt.Errorf("%w: %s (status %d)", ErrSteamAPI, resp.Result.StatusDetail, resp.Result.Status)
	}

	inv.slots = resp.Result.NumBackpackSlots
	inv.items = resp.Result.Items
	inv.fetched = true

	return nil
}

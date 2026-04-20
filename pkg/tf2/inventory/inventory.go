// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package inventory

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"sync"

	"github.com/lemon4ksan/g-man/pkg/bus"
	"github.com/lemon4ksan/g-man/pkg/log"
	"github.com/lemon4ksan/g-man/pkg/steam/community"
	"github.com/lemon4ksan/g-man/pkg/steam/community/inventory"
	"github.com/lemon4ksan/g-man/pkg/steam/service"
	"github.com/lemon4ksan/g-man/pkg/tf2/schema"
)

// PlayerInventory represents a specific player's TF2 inventory.
// Data is lazy-loaded on the first request.
type PlayerInventory struct {
	steamID   uint64
	client    service.Doer
	community community.Requester
	schema    *schema.Schema
	logger    log.Logger

	dupeCheckers []DupeChecker

	mu      sync.Mutex
	items   []TF2Item
	slots   int
	fetched bool
}

// WithLogger sets a custom logger for the inventory.
func WithLogger(l log.Logger) bus.Option[*PlayerInventory] {
	return func(inv *PlayerInventory) {
		inv.logger = l
	}
}

// WithCommunityBackoff sets the community client for fetching inventory when web api fails.
func WithCommunityBackoff(r community.Requester) bus.Option[*PlayerInventory] {
	return func(inv *PlayerInventory) {
		inv.community = r
	}
}

// New creates an inventory for a specific player.
// dupeCheckers is a slice of implementations (e.g. [NewBackpackTFChecker]).
func New(
	steamID uint64,
	client service.Doer,
	dupeCheckers []DupeChecker,
	opts ...bus.Option[*PlayerInventory],
) *PlayerInventory {
	p := &PlayerInventory{
		steamID:      steamID,
		client:       client,
		logger:       log.Discard,
		dupeCheckers: dupeCheckers,
		items:        make([]TF2Item, 0),
	}

	for _, opt := range opts {
		opt(p)
	}

	return p
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

	for _, item := range items {
		if item.ID == assetID {
			targetItem = &item
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

		if !status.Recorded {
			continue
		}

		isRecorded = true

		if status.IsDuped {
			isDuped = true
			break
		}
	}

	return isDuped, isRecorded, err
}

func (inv *PlayerInventory) fetch(ctx context.Context) error {
	err := inv.fetchViaWebAPI(ctx)
	if err == nil {
		inv.logger.Debug("Inventory fetched via WebAPI", log.Uint64("steam_id", inv.steamID))
		return nil
	}

	if inv.community == nil || inv.community.SessionID("") == "" {
		return fmt.Errorf("webapi failed and no community session available: %w", err)
	}

	inv.logger.Warn("WebAPI failed, attempting Community fallback",
		log.Uint64("steam_id", inv.steamID),
		log.Err(err),
	)

	return inv.fetchCommunity(ctx)
}

func (inv *PlayerInventory) fetchViaWebAPI(ctx context.Context) error {
	req := struct {
		SteamID uint64 `url:"steamid"`
	}{inv.steamID}

	resp, err := service.WebAPI[PlayerItemsResponse](ctx, inv.client, "GET", "IEconItems_440", "GetPlayerItems", 1, req)
	if err != nil {
		return err
	}

	if resp.Result.Status == 15 {
		return errors.New("inventory is private (status 15)")
	}

	if resp.Result.Status != 1 {
		return fmt.Errorf("steam api error: %s (status %d)", resp.Result.StatusDetail, resp.Result.Status)
	}

	inv.items = resp.Result.Items
	inv.slots = resp.Result.NumBackpackSlots
	inv.fetched = true

	return nil
}

func (inv *PlayerInventory) fetchCommunity(ctx context.Context) error {
	items, currencies, total, err := inventory.GetUserInventoryContents(
		ctx, inv.community, inv.steamID, 440, 2, false, "english",
	)
	if err != nil {
		return fmt.Errorf("community fallback failed: %w", err)
	}

	unifiedItems := make([]TF2Item, 0, len(items)+len(currencies))
	for _, it := range items {
		unifiedItems = append(unifiedItems, mapCEconToTF2(it, inv.schema))
	}

	for _, it := range currencies {
		unifiedItems = append(unifiedItems, mapCEconToTF2(it, inv.schema))
	}

	inv.items = unifiedItems
	inv.slots = total
	inv.fetched = true

	return nil
}

// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package backpack

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
	"github.com/lemon4ksan/g-man/pkg/steam/webapi"
	"github.com/lemon4ksan/g-man/pkg/tf2/currency"
	"github.com/lemon4ksan/g-man/pkg/tf2/schema"
	"github.com/lemon4ksan/g-man/pkg/trading"
)

// Remote represents a specific player's TF2 inventory.
// Data is lazy-loaded on the first request.
type Remote struct {
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

// Option defines a functional configuration for the Remote.
type Option = bus.Option[*Remote]

// WithLogger sets a custom logger for the inventory.
func WithLogger(l log.Logger) Option {
	return func(inv *Remote) {
		inv.logger = l
	}
}

// WithCommunityBackoff sets the community client for fetching inventory when web api fails.
func WithCommunityBackoff(r community.Requester) Option {
	return func(inv *Remote) {
		inv.community = r
	}
}

// WithDupeCheckers allows to check duped items.
func WithDupeCheckers(dc []DupeChecker) Option {
	return func(inv *Remote) {
		inv.dupeCheckers = dc
	}
}

// NewRemote creates an inventory for a specific player.
// dupeCheckers is a slice of implementations (e.g. [NewBackpackTFChecker]).
func NewRemote(steamID uint64, client service.Doer, opts ...Option) *Remote {
	p := &Remote{
		steamID:      steamID,
		client:       client,
		logger:       log.Discard,
		dupeCheckers: make([]DupeChecker, 0),
		items:        make([]TF2Item, 0),
	}

	for _, opt := range opts {
		opt(p)
	}

	return p
}

// GetItemsBySKU returns all assets in someone else's inventory that match the specified SKU.
// This is necessary to check: "Did the partner actually offer the item we agreed on?"
func (r *Remote) GetItemsBySKU(ctx context.Context, targetSKU string) ([]TF2Item, error) {
	r.mu.Lock()
	if !r.fetched {
		if err := r.fetch(ctx); err != nil {
			r.mu.Unlock()
			return nil, err
		}
	}

	items := r.items
	r.mu.Unlock()

	var result []TF2Item

	for _, it := range items {
		if it.ToSKU() == targetSKU {
			result = append(result, it)
		}
	}

	return result, nil
}

// CanTradeWithoutHold calls the steam api to check whether
// the trade with remote user can be performed without hold.
func (r *Remote) CanTradeWithoutHold(ctx context.Context, token string) (bool, error) {
	req := &webapi.IEconService_GetTradeHoldDurations_v1_Request{
		SteamIDTarget: r.steamID, TradeOfferAccessToken: token,
	}

	type resp struct {
		TheirHold int `json:"their_escrow"`
		MyHold    int `json:"my_escrow"`
	}

	res, err := webapi.IEconService_GetTradeHoldDurations_v1[resp](ctx, r.client, req)
	if err != nil {
		return false, err
	}

	return res.TheirHold == 0, nil
}

// IsDuped checks whether the item is a duplicate.
// It queries all registered DupeCheckers in turn.
// If at least one service considers the item a duplicate, true is returned.
// If no service knows about the item, nil is returned.
func (r *Remote) IsDuped(ctx context.Context, assetID uint64) (*bool, error) {
	duped, recorded, err := r.checkWithServices(ctx, assetID)
	if err != nil {
		return nil, err
	}

	if recorded {
		return &duped, nil
	}

	r.mu.Lock()
	if !r.fetched {
		if err := r.fetch(ctx); err != nil {
			r.mu.Unlock()
			return nil, err
		}
	}

	items := r.items
	r.mu.Unlock()

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

	duped, recorded, err = r.checkWithServices(ctx, targetItem.OriginalID)
	if err != nil {
		return nil, err
	}

	if recorded {
		return &duped, nil
	}

	return nil, nil
}

// FindMetalInPartnerInventory finds metal items in the partner's inventory.
// It uses GetItemsBySKU to find items that match the specified SKUs.
func (r *Remote) FindMetalInPartnerInventory(ctx context.Context, amount currency.Scrap) ([]*trading.Item, error) {
	skus := []string{currency.SKURefined, currency.SKUReclaimed, currency.SKUScrap}
	values := map[string]currency.Scrap{
		currency.SKURefined:   9,
		currency.SKUReclaimed: 3,
		currency.SKUScrap:     1,
	}

	var selected []*trading.Item

	remaining := amount

	for _, sku := range skus {
		val := values[sku]

		items, err := r.GetItemsBySKU(ctx, sku)
		if err != nil {
			continue
		}

		for _, it := range items {
			if remaining <= 0 {
				break
			}

			selected = append(selected, it.ToEconItem())
			remaining -= val
		}
	}

	if remaining > 0 {
		return nil, fmt.Errorf("partner is missing %d scrap for counter-offer", remaining)
	}

	return selected, nil
}

func (r *Remote) checkWithServices(
	ctx context.Context,
	assetID uint64,
) (isDuped, isRecorded bool, err error) {
	for _, checker := range r.dupeCheckers {
		status, checkErr := checker.CheckHistory(ctx, assetID)
		if checkErr != nil {
			r.logger.Warn("Dupe checker failed",
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

func (r *Remote) fetch(ctx context.Context) error {
	err := r.fetchViaWebAPI(ctx)
	if err == nil {
		r.logger.Debug("Inventory fetched via WebAPI", log.Uint64("steam_id", r.steamID))
		return nil
	}

	if r.community == nil || r.community.SessionID("") == "" {
		return fmt.Errorf("webapi failed and no community session available: %w", err)
	}

	r.logger.Warn("WebAPI failed, attempting Community fallback",
		log.Uint64("steam_id", r.steamID),
		log.Err(err),
	)

	return r.fetchCommunity(ctx)
}

func (r *Remote) fetchViaWebAPI(ctx context.Context) error {
	req := struct {
		SteamID uint64 `url:"steamid"`
	}{r.steamID}

	resp, err := service.WebAPI[PlayerItemsResponse](ctx, r.client, "GET", "IEconItems_440", "GetPlayerItems", 1, req)
	if err != nil {
		return err
	}

	if resp.Result.Status == 15 {
		return errors.New("inventory is private (status 15)")
	}

	if resp.Result.Status != 1 {
		return fmt.Errorf("steam api error: %s (status %d)", resp.Result.StatusDetail, resp.Result.Status)
	}

	r.items = resp.Result.Items
	r.slots = resp.Result.NumBackpackSlots
	r.fetched = true

	return nil
}

func (r *Remote) fetchCommunity(ctx context.Context) error {
	items, currencies, total, err := inventory.GetUserInventoryContents(
		ctx, r.community, r.steamID, 440, 2, false, "english",
	)
	if err != nil {
		return fmt.Errorf("community fallback failed: %w", err)
	}

	unifiedItems := make([]TF2Item, 0, len(items)+len(currencies))
	for _, it := range items {
		unifiedItems = append(unifiedItems, mapCEconToTF2(it, r.schema))
	}

	for _, it := range currencies {
		unifiedItems = append(unifiedItems, mapCEconToTF2(it, r.schema))
	}

	r.items = unifiedItems
	r.slots = total
	r.fetched = true

	return nil
}

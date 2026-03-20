// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package inventory

import (
	"context"
	"fmt"
	"net/http"
	"sync"

	"github.com/PuerkitoBio/goquery"
	"github.com/lemon4ksan/g-man/pkg/rest"
	"github.com/lemon4ksan/g-man/pkg/steam/service"
)

// PlayerInventory represents a specific player's TF2 inventory, lazy-loaded.
type PlayerInventory struct {
	steamID uint64

	steamClient service.Requester
	httpClient  rest.HTTPDoer

	bptfUserID string

	mu sync.Mutex

	items   []TF2Item
	slots   int
	fetched bool
}

// NewPlayerInventory initializes the inventory for a specific player.
func NewPlayerInventory(steamID uint64, c service.Requester, httpClient rest.HTTPDoer, bptfUserID string) *PlayerInventory {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	return &PlayerInventory{
		steamID:     steamID,
		steamClient: c,
		httpClient:  httpClient,
		bptfUserID:  bptfUserID,
		items:       make([]TF2Item, 0),
	}
}

// IsDuped checks if an item is a duplicate. Returns a bool pointer:
// nil - if the item has never been in backpack.tf
// *true - if it is a duplicate
// *false - if it is clean
func (inv *PlayerInventory) IsDuped(ctx context.Context, assetID uint64) (*bool, error) {
	history, err := inv.getItemHistory(ctx, assetID)
	if err != nil {
		return nil, err
	}

	if history.Recorded {
		return &history.IsDuped, nil
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

	historyOriginal, err := inv.getItemHistory(ctx, targetItem.OriginalID)
	if err != nil {
		return nil, err
	}

	if historyOriginal.Recorded {
		return &historyOriginal.IsDuped, nil
	}

	return nil, nil
}

func (inv *PlayerInventory) fetch(ctx context.Context) error {
	req := &struct {
		SteamID uint64 `url:"steamid"`
	}{inv.steamID}
	resp, err := service.WebAPI[PlayerItemsResponse](ctx, inv.steamClient, "GET", "IEconItems_440", "GetPlayerItems", 1, req)
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

// ItemHistoryResult represents the result of scraping backpack.tf.
type ItemHistoryResult struct {
	Recorded bool
	IsDuped  bool
}

func (inv *PlayerInventory) getItemHistory(ctx context.Context, assetID uint64) (ItemHistoryResult, error) {
	url := fmt.Sprintf("https://backpack.tf/item/%d", assetID)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return ItemHistoryResult{}, err
	}

	req.Header.Set("User-Agent", "G-man Bot")
	req.Header.Set("Cookie", "user-id="+inv.bptfUserID) // uid(12)

	resp, err := inv.httpClient.Do(req)
	if err != nil {
		return ItemHistoryResult{}, fmt.Errorf("backpack.tf request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		if resp.StatusCode == http.StatusNotFound {
			return ItemHistoryResult{Recorded: false}, nil
		}
		return ItemHistoryResult{}, fmt.Errorf("backpack.tf returned status: %d", resp.StatusCode)
	}

	doc, err := goquery.NewDocumentFromReader(resp.Body)
	if err != nil {
		return ItemHistoryResult{}, fmt.Errorf("failed to parse HTML: %w", err)
	}

	if doc.Find("table").Length() != 1 {
		return ItemHistoryResult{Recorded: false}, nil
	}

	isDuped := doc.Find("#dupe-modal-btn").Length() > 0

	return ItemHistoryResult{
		Recorded: true,
		IsDuped:  isDuped,
	}, nil
}

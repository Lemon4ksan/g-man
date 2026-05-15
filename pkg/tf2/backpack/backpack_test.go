// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package backpack

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/lemon4ksan/g-man/pkg/bus"
	"github.com/lemon4ksan/g-man/pkg/log"
	"github.com/lemon4ksan/g-man/pkg/tf2"
	"github.com/lemon4ksan/g-man/pkg/tf2/currency"
	"github.com/lemon4ksan/g-man/pkg/tf2/schema"
	"github.com/lemon4ksan/g-man/pkg/trading"
)

type mockCache struct {
	items    []*tf2.Item
	maxSlots int
}

func (m *mockCache) GetItems() []*tf2.Item {
	return m.items
}

func (m *mockCache) GetItem(id uint64) (*tf2.Item, bool) {
	for _, it := range m.items {
		if it.ID == id {
			return it, true
		}
	}

	return nil, false
}

func (m *mockCache) GetMaxSlots() int {
	return m.maxSlots
}

type mockSchemaProvider struct {
	s *schema.Schema
}

func (m *mockSchemaProvider) Get() *schema.Schema {
	return m.s
}

func TestBackpack_Locking(t *testing.T) {
	bp := New()

	// Initial state
	assert.Empty(t, bp.GetLockedAssetIDs())

	// Lock some items
	bp.LockItems([]uint64{100, 200})
	locked := bp.GetLockedAssetIDs()
	assert.ElementsMatch(t, []uint64{100, 200}, locked)

	// Unlock one item
	bp.UnlockItems([]uint64{100})
	locked = bp.GetLockedAssetIDs()
	assert.ElementsMatch(t, []uint64{200}, locked)
}

func TestBackpack_GetPureStock(t *testing.T) {
	mock := &mockCache{
		items: []*tf2.Item{
			{ID: 1, DefIndex: 5021, IsTradable: true},  // Key
			{ID: 2, DefIndex: 5021, IsTradable: false}, // Untradable Key
			{ID: 3, DefIndex: 5002, IsTradable: true},  // Ref
			{ID: 4, DefIndex: 5002, IsTradable: true},  // Ref
			{ID: 5, DefIndex: 5001, IsTradable: true},  // Rec
			{ID: 6, DefIndex: 5000, IsTradable: true},  // Scrap
			{ID: 7, DefIndex: 123, IsTradable: true},   // Random weapon
		},
	}

	bp := &Backpack{cache: mock}

	stock := bp.GetPureStock()

	expected := currency.PureStock{
		Keys:      1,
		Refined:   2,
		Reclaimed: 1,
		Scrap:     1,
	}

	assert.Equal(t, expected, stock)
}

func TestBackpack_GetAssetIDs(t *testing.T) {
	mock := &mockCache{
		items: []*tf2.Item{
			{ID: 1, IsTradable: true, SKU: "target_sku"},
			{ID: 2, IsTradable: true, SKU: "target_sku"},
			{ID: 3, IsTradable: false, SKU: "target_sku"}, // Untradable
			{ID: 4, IsTradable: true, SKU: "other_sku"},
		},
	}

	bp := &Backpack{
		cache:   mock,
		locked:  make(map[uint64]bool),
		manager: &mockSchemaProvider{s: &schema.Schema{}},
	}

	// Lock item 2
	bp.LockItems([]uint64{2})

	// We expect only item 1 (tradable, matching SKU, not locked)
	ids := bp.GetAssetIDs("target_sku")
	assert.ElementsMatch(t, []uint64{1}, ids)
}

func TestBackpack_HandleEvent(t *testing.T) {
	mock := &mockCache{maxSlots: 10}
	bp := &Backpack{cache: mock}
	bp.Logger = log.Discard
	bp.Bus = bus.New()

	// 1. Full Backpack Event
	// Fill it up to 10 items
	for i := range 10 {
		mock.items = append(mock.items, &tf2.Item{ID: uint64(i)})
	}

	events := bp.handleEvent(&tf2.ItemAcquiredEvent{Item: &tf2.Item{ID: 999}})
	assert.Len(t, events, 1)
	assert.IsType(t, &FullEvent{}, events[0])
}

func TestBackpack_ApplyLayout(t *testing.T) {
	bp := New()
	bp.manager = schema.NewManager(schema.Config{})

	// Test error case: schema not ready
	err := bp.ApplyLayout(context.Background(), Layout{})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "schema not ready")
}

func TestBackpack_GetItemsBySKU(t *testing.T) {
	mock := &mockCache{
		items: []*tf2.Item{
			{ID: 1, SKU: "target_sku"},
			{ID: 2, SKU: "other_sku"},
		},
	}

	bp := &Backpack{
		cache:   mock,
		manager: &mockSchemaProvider{s: &schema.Schema{}},
	}

	ids := bp.GetItemsBySKU("target_sku")
	assert.ElementsMatch(t, []uint64{1}, ids)
}

func TestBackpack_CleanupStaleLocks(t *testing.T) {
	bp := New()
	bp.LockItems([]uint64{1, 2, 3})

	mockTrading := &mockTradingProvider{
		offers: []trading.TradeOffer{
			{
				ItemsToGive: []*trading.Item{
					{AssetID: 1},
				},
			},
		},
	}

	bp.cleanupStaleLocks(context.Background(), mockTrading)

	locked := bp.GetLockedAssetIDs()
	assert.Contains(t, locked, uint64(1))
	assert.NotContains(t, locked, uint64(2))
	assert.NotContains(t, locked, uint64(3))
}

type mockTradingProvider struct {
	offers []trading.TradeOffer
}

func (m *mockTradingProvider) GetActiveSentOffers(ctx context.Context) ([]trading.TradeOffer, error) {
	return m.offers, nil
}

func TestPositionOf(t *testing.T) {
	tests := []struct {
		page     int
		slot     int
		expected uint32
	}{
		{1, 1, 1},
		{1, 50, 50},
		{2, 1, 51},
		{3, 10, 110},
		{0, 0, 1},   // Bounds checking
		{-1, -5, 1}, // Bounds checking
	}

	for _, tt := range tests {
		t.Run("", func(t *testing.T) {
			assert.Equal(t, tt.expected, PositionOf(tt.page, tt.slot))
		})
	}
}

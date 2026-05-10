// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package backpack

import (
	"context"
	"errors"
	"sync"

	"github.com/lemon4ksan/g-man/pkg/bus"
	"github.com/lemon4ksan/g-man/pkg/log"
	"github.com/lemon4ksan/g-man/pkg/steam"
	"github.com/lemon4ksan/g-man/pkg/steam/module"
	"github.com/lemon4ksan/g-man/pkg/tf2"
	"github.com/lemon4ksan/g-man/pkg/tf2/currency"
	"github.com/lemon4ksan/g-man/pkg/tf2/schema/manager"
	"github.com/lemon4ksan/g-man/pkg/trading/web/offer"
)

// ModuleName is the name of the module.
const ModuleName = "tf2_backpack"

// WithModule returns a steam.Option that registers the backpack module.
func WithModule() steam.Option {
	return func(c *steam.Client) {
		c.RegisterModule(New())
	}
}

const (
	// ItemsPerPage is the number of items per page.
	ItemsPerPage = 50
	// SlotsPerRow is the number of slots per row.
	SlotsPerRow = 10
)

// TradingProvider is an interface for getting active sent offers.
type TradingProvider interface {
	GetActiveSentOffers(ctx context.Context) ([]offer.TradeOffer, error)
}

// PositionOf converts a page and slot (1-based) into a GC index.
// Example: Page 2, Slot 1 -> 51
func PositionOf(page, slot int) uint32 {
	if page < 1 {
		page = 1
	}

	if slot < 1 {
		slot = 1
	}

	return uint32((page-1)*ItemsPerPage + slot)
}

// Backpack is a high-level module that manages the bot's inventory.
// It combines the live data stream from the GC and detailed data from the Web.
type Backpack struct {
	module.Base

	tf2     *tf2.TF2
	manager *manager.Manager
	trading TradingProvider

	mu     sync.RWMutex
	slots  int
	items  map[uint64]*tf2.Item
	skus   map[uint64]string // AssetID -> SKU string
	locked map[uint64]bool
}

// New creates a new backpack module for inventory management.
func New() *Backpack {
	return &Backpack{
		Base:  module.New(ModuleName),
		items: make(map[uint64]*tf2.Item),
		skus:  make(map[uint64]string),
	}
}

// Init initializes the backpack module.
func (m *Backpack) Init(init module.InitContext) error {
	if err := m.Base.Init(init); err != nil {
		return err
	}

	m.tf2 = init.Module(tf2.ModuleName).(*tf2.TF2)

	tf2Mod, ok := init.Module(tf2.ModuleName).(*tf2.TF2)
	if !ok || tf2Mod == nil {
		return errors.New("tf2 module not registered or invalid")
	}

	m.tf2 = tf2Mod

	manager, ok := init.Module(manager.ModuleName).(*manager.Manager)
	if !ok || manager == nil {
		return errors.New("schema manager module not registered or invalid")
	}

	m.manager = manager

	m.trading = init.Module("trading").(TradingProvider)

	return nil
}

// StartAuthed starts the backpack module.
func (m *Backpack) StartAuthed(ctx context.Context, authCtx module.AuthContext) error {
	m.syncWithCache()

	m.Go(func(ctx context.Context) {
		m.eventLoop(ctx)
	})

	if m.trading != nil {
		m.Go(func(ctx context.Context) {
			m.cleanupStaleLocks(ctx, m.trading)
		})
	}

	return nil
}

// LockItems locks items in the backpack.
func (m *Backpack) LockItems(ids []uint64) {
	m.mu.Lock()
	defer m.mu.Unlock()

	for _, id := range ids {
		m.locked[id] = true
	}
}

// UnlockItems unlocks items in the backpack.
func (m *Backpack) UnlockItems(ids []uint64) {
	m.mu.Lock()
	defer m.mu.Unlock()

	for _, id := range ids {
		delete(m.locked, id)
	}
}

// GetItem returns the item with the given ID.
func (m *Backpack) GetItem(id uint64) *tf2.Item {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.items[id]
}

// GetItemsBySKU returns all AssetIDs of items that match the SKU.
func (m *Backpack) GetItemsBySKU(targetSKU string) []uint64 {
	m.mu.RLock()
	defer m.mu.RUnlock()

	s := m.manager.Get()

	var result []uint64

	for id, item := range m.items {
		if s.GetSKUFromEconItem(item.ToEconItem()) == targetSKU {
			result = append(result, id)
		}
	}

	return result
}

// GetPureStock returns the amount of currency (keys and metal) for the MetalManager.
func (m *Backpack) GetPureStock() currency.PureStock {
	m.mu.RLock()
	defer m.mu.RUnlock()

	stock := currency.PureStock{}
	for _, item := range m.items {
		if !item.IsTradable {
			continue
		}

		switch item.DefIndex {
		case 5021:
			stock.Keys++
		case 5002:
			stock.Refined++
		case 5001:
			stock.Reclaimed++
		case 5000:
			stock.Scrap++
		}
	}

	return stock
}

// GetAssetIDs returns a list of available item IDs for a specific SKU.
// It automatically excludes items that are blocked (in other trades).
func (m *Backpack) GetAssetIDs(targetSKU string) []uint64 {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var result []uint64
	for id, item := range m.items {
		if !m.locked[id] && item.IsTradable && m.skus[id] == targetSKU {
			result = append(result, id)
		}
	}

	return result
}

// GetLockedAssetIDs returns currently locked asset ids
func (m *Backpack) GetLockedAssetIDs() []uint64 {
	m.mu.RLock()
	defer m.mu.RUnlock()

	result := make([]uint64, 0, len(m.locked))
	for id := range m.locked {
		result = append(result, id)
	}

	return result
}

// ApplyLayout analyzes the current inventory and moves items according to the rules.
func (m *Backpack) ApplyLayout(ctx context.Context, layout Layout) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	s := m.manager.Get()
	if s == nil {
		return errors.New("schema not ready")
	}

	locked := m.getLockedMap()
	plannedIDs := make(map[uint64]bool)

	var moves []tf2.ItemPos

	allItems := make([]*tf2.Item, 0, len(m.items))
	for _, it := range m.items {
		allItems = append(allItems, it)
	}

	for page, cfg := range layout.Pages {
		currentSlot := 1

		for _, filter := range cfg.Filters {
			for _, item := range allItems {
				if plannedIDs[item.ID] || locked[item.ID] {
					continue
				}

				if filter(item, s) {
					targetPos := PositionOf(page, currentSlot)
					plannedIDs[item.ID] = true

					if item.Inventory != targetPos {
						moves = append(moves, tf2.ItemPos{
							Id:       item.ID,
							Position: targetPos,
						})
					}

					currentSlot++
					if currentSlot > ItemsPerPage {
						break
					}
				}
			}
		}
	}

	if len(moves) == 0 {
		m.Logger.Info("Inventory is already sorted according to layout")
		return nil
	}

	m.Logger.Info("Applying inventory layout", log.Int("moves_count", len(moves)))

	return m.tf2.MoveItems(ctx, moves)
}

func (m *Backpack) eventLoop(ctx context.Context) {
	sub := m.Bus.Subscribe(
		&tf2.BackpackLoadedEvent{},
		&tf2.ItemAcquiredEvent{},
		&tf2.ItemRemovedEvent{},
		&tf2.ItemUpdatedEvent{},
	)
	defer sub.Unsubscribe()

	for {
		select {
		case <-ctx.Done():
			return
		case ev := <-sub.C():
			events := m.handleEvent(ev)
			for _, e := range events {
				m.Bus.Publish(e)
			}
		}
	}
}

func (m *Backpack) handleEvent(ev bus.Event) []bus.Event {
	m.mu.Lock()
	defer m.mu.Unlock()

	var events []bus.Event

	s := m.manager.Get()

	switch e := ev.(type) {
	case *tf2.BackpackLoadedEvent:
		m.syncWithCache()

	case *tf2.ItemAcquiredEvent:
		m.items[e.Item.ID] = e.Item
		if s != nil {
			m.skus[e.Item.ID] = s.GetSKUFromEconItem(e.Item.ToEconItem())
		}

		if m.slots > 0 && len(m.items) >= m.slots {
			m.Logger.Warn("Backpack is FULL!", log.Int("count", len(m.items)), log.Int("max", m.slots))
			events = append(events, &FullEvent{Count: len(m.items), Max: m.slots})
		}

	case *tf2.ItemRemovedEvent:
		delete(m.items, e.ItemID)
		delete(m.skus, e.ItemID)

	case *tf2.ItemUpdatedEvent:
		m.items[e.Item.ID] = e.Item
		if s != nil {
			m.skus[e.Item.ID] = s.GetSKUFromEconItem(e.Item.ToEconItem())
		}
	}

	return events
}

func (m *Backpack) cleanupStaleLocks(ctx context.Context, tradingModule TradingProvider) {
	activeOffers, err := tradingModule.GetActiveSentOffers(ctx)
	if err != nil {
		m.Logger.Error("Failed to get active offers for stale lock cleanup", log.Err(err))
		return
	}

	activeItems := make(map[uint64]bool)
	for _, off := range activeOffers {
		for _, it := range off.ItemsToGive {
			activeItems[it.AssetID] = true
		}
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	cleanedCount := 0
	for lockedID := range m.locked {
		if !activeItems[lockedID] {
			delete(m.locked, lockedID)

			cleanedCount++
		}
	}

	if cleanedCount > 0 {
		m.Logger.Info("Cleaned up stale item locks", log.Int("count", cleanedCount))
	}
}

func (m *Backpack) syncWithCache() {
	cache := m.tf2.Cache()
	s := m.manager.Get()

	m.items = make(map[uint64]*tf2.Item)
	m.skus = make(map[uint64]string)

	items := cache.GetItems()
	for _, it := range items {
		m.items[it.ID] = it
		if s != nil {
			m.skus[it.ID] = s.GetSKUFromEconItem(it.ToEconItem())
		}
	}

	m.slots = int(cache.GetMaxSlots())

	m.Logger.Info("Backpack synchronized", log.Int("items", len(m.items)), log.Int("slots", m.slots))
}

func (m *Backpack) getLockedMap() map[uint64]bool {
	locked := make(map[uint64]bool)
	for _, id := range m.GetLockedAssetIDs() {
		locked[id] = true
	}

	return locked
}

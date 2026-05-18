// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package backpack

import (
	"context"
	"errors"
	"slices"
	"sync"
	"time"

	"github.com/lemon4ksan/g-man/pkg/bus"
	"github.com/lemon4ksan/g-man/pkg/log"
	"github.com/lemon4ksan/g-man/pkg/steam"
	"github.com/lemon4ksan/g-man/pkg/steam/module"
	"github.com/lemon4ksan/g-man/pkg/tf2"
	"github.com/lemon4ksan/g-man/pkg/tf2/currency"
	"github.com/lemon4ksan/g-man/pkg/tf2/schema"
	"github.com/lemon4ksan/g-man/pkg/trading"
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
	GetActiveSentOffers(ctx context.Context) ([]trading.TradeOffer, error)
}

// SchemaProvider defines the interface for getting the current schema.
type SchemaProvider interface {
	Get() *schema.Schema
}

// ItemCache defines the interface for accessing the TF2 item cache.
type ItemCache interface {
	GetItems() []*tf2.Item
	GetItem(id uint64) (*tf2.Item, bool)
	GetMaxSlots() int
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

// Backpack is a high-level module for managing the TF2 inventory.
// Unlike traditional implementations, this module does not store a redundant copy of items.
// Instead, it acts as a lightweight view over the SOCache, providing utility methods
// for filtering by SKU, managing item locks for trading, and applying inventory layouts.
//
// It is designed to be highly memory-efficient and remains perfectly synchronized
// with the Game Coordinator state at all times.
type Backpack struct {
	module.Base

	tf2     *tf2.TF2
	cache   ItemCache
	manager SchemaProvider
	trading TradingProvider

	mu     sync.RWMutex
	locked map[uint64]bool
}

// New creates a new backpack module for inventory management.
func New() *Backpack {
	return &Backpack{
		Base:   module.New(ModuleName),
		locked: make(map[uint64]bool),
	}
}

// Init initializes the backpack module.
func (m *Backpack) Init(init module.InitContext) error {
	if err := m.Base.Init(init); err != nil {
		return err
	}

	tf2Mod, ok := init.Module(tf2.ModuleName).(*tf2.TF2)
	if !ok || tf2Mod == nil {
		return errors.New("tf2 module not registered or invalid")
	}

	m.tf2 = tf2Mod
	m.cache = tf2Mod.Cache()

	managerMod, ok := init.Module(schema.ModuleName).(*schema.Manager)
	if !ok || managerMod == nil {
		return errors.New("schema manager module not registered or invalid")
	}

	m.manager = managerMod

	m.trading = init.Module("trading").(TradingProvider)

	return nil
}

// StartAuthed starts the backpack module.
func (m *Backpack) StartAuthed(ctx context.Context, authCtx module.AuthContext) error {
	m.Go(m.eventLoop)

	if m.trading != nil {
		m.Go(func(ctx context.Context) {
			m.cleanupStaleLocks(ctx, m.trading)

			ticker := time.NewTicker(15 * time.Minute)
			defer ticker.Stop()

			for {
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
					m.cleanupStaleLocks(ctx, m.trading)
				}
			}
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

// Cache returns the underlying item cache.
func (m *Backpack) Cache() ItemCache {
	return m.cache
}

// Schema returns the schema provider.
func (m *Backpack) Schema() SchemaProvider {
	return m.manager
}

// GetItem returns the item with the given ID.
func (m *Backpack) GetItem(id uint64) (*tf2.Item, bool) {
	return m.cache.GetItem(id)
}

// GetItemsBySKU returns all AssetIDs of items that match the SKU.
func (m *Backpack) GetItemsBySKU(targetSKU string) []uint64 {
	s := m.manager.Get()
	if s == nil {
		return nil
	}

	var result []uint64
	for _, item := range m.cache.GetItems() {
		if item.GetSKU(s) == targetSKU {
			result = append(result, item.ID)
		}
	}

	return result
}

// GetPureStock returns the amount of currency (keys and metal) for the MetalManager.
func (m *Backpack) GetPureStock() currency.PureStock {
	stock := currency.PureStock{}
	for _, item := range m.cache.GetItems() {
		if !item.IsTradable {
			continue
		}

		switch schema.NormalizeDefindex(int(item.DefIndex)) {
		case schema.DefKey:
			stock.Keys++
		case schema.DefRefined:
			stock.Refined++
		case schema.DefReclaimed:
			stock.Reclaimed++
		case schema.DefScrap:
			stock.Scrap++
		}
	}

	return stock
}

// FindCraftableItems returns a list of AssetIDs for items that can be used in crafting.
func (m *Backpack) FindCraftableItems(defIndex uint32, count int) []uint64 {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var result []uint64
	for _, item := range m.cache.GetItems() {
		if item.DefIndex == defIndex && item.IsCraftable && !m.locked[item.ID] {
			result = append(result, item.ID)
			if len(result) == count {
				break
			}
		}
	}

	return result
}

// GetTotalCount returns the total number of items in the backpack.
func (m *Backpack) GetTotalCount() int {
	return len(m.cache.GetItems())
}

// GetStock returns the current stock count for a specific SKU.
func (m *Backpack) GetStock(sku string) int {
	s := m.manager.Get()
	if s == nil {
		return 0
	}

	count := 0
	for _, item := range m.cache.GetItems() {
		if item.GetSKU(s) == sku {
			count++
		}
	}

	return count
}

// FindWeaponsByClass returns all craftable weapons that can be used by the given class.
func (m *Backpack) FindWeaponsByClass(class string) []*tf2.Item {
	s := m.manager.Get()
	if s == nil {
		return nil
	}

	m.mu.RLock()
	defer m.mu.RUnlock()

	var result []*tf2.Item
	for _, item := range m.cache.GetItems() {
		if !item.IsCraftable || !item.IsTradable || m.locked[item.ID] {
			continue
		}

		sch := s.ItemByDef(int(item.DefIndex))
		if sch == nil || sch.CraftClass != "weapon" {
			continue
		}

		if slices.Contains(sch.UsedByClasses, class) {
			result = append(result, item)
		}
	}

	return result
}

// GetMetalCount returns the number of items with the given DefIndex.
func (m *Backpack) GetMetalCount(defIndex uint32) int {
	count := 0
	for _, item := range m.cache.GetItems() {
		if item.DefIndex == defIndex {
			count++
		}
	}

	return count
}

// GetAssetIDs returns a list of available item IDs for a specific SKU.
// It automatically excludes items that are blocked (in other trades).
func (m *Backpack) GetAssetIDs(targetSKU string) []uint64 {
	s := m.manager.Get()
	if s == nil {
		return nil
	}

	m.mu.RLock()
	defer m.mu.RUnlock()

	var result []uint64
	for _, item := range m.cache.GetItems() {
		if !m.locked[item.ID] && item.IsTradable && item.GetSKU(s) == targetSKU {
			result = append(result, item.ID)
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

	allItems := m.cache.GetItems()

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
		&schema.UpdatedEvent{},
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
	var events []bus.Event

	if _, ok := ev.(*tf2.ItemAcquiredEvent); ok {
		count := len(m.cache.GetItems())
		slots := m.cache.GetMaxSlots()

		if slots > 0 && count >= slots {
			m.Logger.Warn("Backpack is FULL!", log.Int("count", count), log.Int("max", slots))
			events = append(events, &FullEvent{Count: count, Max: slots})
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

func (m *Backpack) getLockedMap() map[uint64]bool {
	locked := make(map[uint64]bool)
	for _, id := range m.GetLockedAssetIDs() {
		locked[id] = true
	}

	return locked
}

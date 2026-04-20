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
	"github.com/lemon4ksan/g-man/pkg/tf2/schema"
)

const ModuleName = "backpack"

func WithModule() steam.Option {
	return func(c *steam.Client) {
		c.RegisterModule(New())
	}
}

type BusyProvider interface {
	GetLockedAssetIDs() []uint64
}

const (
	ItemsPerPage = 50
	SlotsPerRow  = 10
)

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

	busy   BusyProvider
	tf2    *tf2.TF2
	schema *schema.Manager

	mu    sync.RWMutex
	items map[uint64]*tf2.Item
	skus  map[uint64]string // AssetID -> SKU string
	slots int
}

func New() *Backpack {
	return &Backpack{
		Base:  module.New(ModuleName),
		items: make(map[uint64]*tf2.Item),
		skus:  make(map[uint64]string),
	}
}

func (m *Backpack) Init(init module.InitContext) error {
	if err := m.Base.Init(init); err != nil {
		return err
	}

	m.tf2 = init.Module(tf2.ModuleName).(*tf2.TF2)
	m.schema = init.Module(schema.ModuleName).(*schema.Manager)

	return nil
}

func (m *Backpack) StartAuthed(ctx context.Context, _ module.AuthContext) error {
	m.syncWithCache()

	m.Go(func(ctx context.Context) {
		m.eventLoop(ctx)
	})

	return nil
}

// GetItemsBySKU returns all AssetIDs of bot items that match the SKU.
func (m *Backpack) GetItemsBySKU(targetSKU string) []uint64 {
	m.mu.RLock()
	defer m.mu.RUnlock()

	s := m.schema.Get()

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

	locked := m.getLockedMap()

	for id, item := range m.items {
		if m.skus[id] == targetSKU && item.IsTradable && !locked[id] {
			result = append(result, id)
		}
	}

	return result
}

// ApplyLayout analyzes the current inventory and moves items according to the rules.
func (m *Backpack) ApplyLayout(ctx context.Context, layout Layout) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	s := m.schema.Get()
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
			m.handleEvent(ev)
		}
	}
}

func (m *Backpack) handleEvent(ev bus.Event) {
	m.mu.Lock()
	defer m.mu.Unlock()

	s := m.schema.Get()

	switch e := ev.(type) {
	case *tf2.BackpackLoadedEvent:
		m.syncWithCache()

	case *tf2.ItemAcquiredEvent:
		m.items[e.Item.ID] = e.Item
		if s != nil {
			m.skus[e.Item.ID] = s.GetSKUFromEconItem(e.Item.ToEconItem())
		}

		m.checkBackpackFull()

	case *tf2.ItemRemovedEvent:
		delete(m.items, e.ItemID)
		delete(m.skus, e.ItemID)

	case *tf2.ItemUpdatedEvent:
		m.items[e.Item.ID] = e.Item
		if s != nil {
			m.skus[e.Item.ID] = s.GetSKUFromEconItem(e.Item.ToEconItem())
		}
	}
}

func (m *Backpack) syncWithCache() {
	cache := m.tf2.Cache()
	s := m.schema.Get()

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

func (m *Backpack) checkBackpackFull() {
	if m.slots > 0 && len(m.items) >= m.slots {
		m.Logger.Warn("Backpack is FULL!", log.Int("count", len(m.items)), log.Int("max", m.slots))
		m.Bus.Publish(&BackpackFullEvent{Count: len(m.items), Max: m.slots})
	}
}

func (m *Backpack) getLockedMap() map[uint64]bool {
	locked := make(map[uint64]bool)
	if m.busy != nil {
		for _, id := range m.busy.GetLockedAssetIDs() {
			locked[id] = true
		}
	}

	return locked
}

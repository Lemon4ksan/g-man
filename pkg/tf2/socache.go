// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package tf2

import (
	"context"
	"slices"
	"sync"
	"sync/atomic"

	"google.golang.org/protobuf/proto"

	"github.com/lemon4ksan/g-man/pkg/bus"
	"github.com/lemon4ksan/g-man/pkg/log"
	pb "github.com/lemon4ksan/g-man/pkg/protobuf/tf2"
	"github.com/lemon4ksan/g-man/pkg/steam/protocol"
	"github.com/lemon4ksan/g-man/pkg/tf2/schema"
	"github.com/lemon4ksan/g-man/pkg/trading"
)

// Item represents a parsed TF2 item.
type Item struct {
	ID           uint64
	DefIndex     uint32
	Level        uint32
	Quality      uint32
	Rarity       uint32
	Position     uint32
	AccountID    uint32
	Inventory    uint32
	Origin       uint32
	Quantity     uint32
	OriginalID   uint64
	CustomName   string
	CustomDesc   string
	IsTradable   bool
	IsMarketable bool
	SKU          string
}

// GetSchema returns data about an item from the provided schema.
func (i *Item) GetSchema(s *schema.Schema) *schema.ItemSchema {
	return s.GetItemByDef(int(i.DefIndex))
}

// IsWeapon checks if an item is a weapon using the schema.
func (i *Item) IsWeapon(s *schema.Schema) bool {
	sch := i.GetSchema(s)
	return sch != nil && sch.CraftClass == "weapon"
}

func (i Item) ToEconItem() *trading.Item {
	return &trading.Item{
		AppID:          AppID,
		ContextID:      2, // For TF2 it's always 2
		AssetID:        i.ID,
		Amount:         int64(i.Quantity),
		Name:           i.CustomName,
		MarketHashName: "", // Filled later by Schema.GetSKUFromEconItem
		Tradable:       i.IsTradable,
	}
}

// SO Type IDs specific to Team Fortress 2.
const (
	SOTypeEconItem              int32 = 1
	SOTypeEconGameAccountClient int32 = 7
)

// WithLogger sets a custom logger for the module.
func WithLogger(l log.Logger) bus.Option[*SOCache] {
	return func(s *SOCache) {
		s.logger = l.With(log.Component("so_cache"))
	}
}

// WithBus sets a custom event bus for emitting events.
func WithBus(b *bus.Bus) bus.Option[*SOCache] {
	return func(s *SOCache) {
		s.bus = b
	}
}

// WithSchema allows filling out the item SKU's during processing.
func WithSchema(schema *schema.Schema) bus.Option[*SOCache] {
	return func(s *SOCache) {
		s.schema = schema
	}
}

// SOCache maintains a live, in-memory mirror of the bot's TF2 inventory and account state.
// It is updated automatically in real-time by the Game Coordinator via SO messages.
type SOCache struct {
	mu sync.RWMutex

	bus    *bus.Bus
	schema *schema.Schema
	logger log.Logger

	items     map[uint64]*Item
	slots     uint32
	isPremium bool

	// Synchronization tracking
	version atomic.Uint64
	ownerID atomic.Uint64

	coord CoordinatorProvider
}

// NewSOCache creates a new empty Shared Object Cache.
func NewSOCache(coord CoordinatorProvider, opts ...bus.Option[*SOCache]) *SOCache {
	s := &SOCache{
		items: make(map[uint64]*Item),
		coord: coord,
	}

	for _, opt := range opts {
		opt(s)
	}

	return s
}

// GetItems returns a snapshot of the current inventory.
func (c *SOCache) GetItems() []*Item {
	c.mu.RLock()
	defer c.mu.RUnlock()

	list := make([]*Item, 0, len(c.items))
	for _, item := range c.items {
		list = append(list, item)
	}

	return list
}

// GetItem returns a specific item by its AssetID, or nil if not found.
func (c *SOCache) GetItem(id uint64) *Item {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.items[id]
}

// GetMetal returns the list of metal item ids.
func (c *SOCache) GetMetal(defIndex uint32, count int) []uint64 {
	c.mu.RLock()
	defer c.mu.RUnlock()

	var ids []uint64

	for _, item := range c.items {
		if item.DefIndex == defIndex && item.IsTradable {
			ids = append(ids, item.ID)
			if len(ids) == count {
				return ids
			}
		}
	}

	return nil
}

// FindCraftableItems searches for items by DefIndex that are safe to use in crafting.
// Returns an array of item IDs. If count > 0, returns the maximum count of items.
func (c *SOCache) FindCraftableItems(defIndex uint32, count int) []uint64 {
	c.mu.RLock()
	defer c.mu.RUnlock()

	var ids []uint64

	for id, item := range c.items {
		// CRITICALLY IMPORTANT: We only accept tradable items!
		// Otherwise, the crafted metal will become non-tradable.
		if item.DefIndex == defIndex && item.IsTradable {
			ids = append(ids, id)
			if count > 0 && len(ids) == count {
				return ids
			}
		}
	}

	return ids
}

func (c *SOCache) FindWeaponsByClass(class string) []*Item {
	c.mu.RLock()
	defer c.mu.RUnlock()

	var result []*Item

	for _, item := range c.items {
		sch := item.GetSchema(c.schema)
		if sch != nil && sch.CraftClass == "weapon" && item.IsTradable {
			if slices.Contains(sch.UsedByClasses, class) {
				result = append(result, item)
			}
		}
	}

	return result
}

// GetMetalCount returns the amount of available metal of a given type.
func (c *SOCache) GetMetalCount(defIndex uint32) int {
	c.mu.RLock()
	defer c.mu.RUnlock()

	count := 0

	for _, item := range c.items {
		if item.DefIndex == defIndex && item.IsTradable {
			count++
		}
	}

	return count
}

// GetAssetIDsBySKU returns a list of AssetIDs for a given item.
// If limit > 0, returns up to limit items.
func (c *SOCache) GetAssetIDsBySKU(targetSKU string, limit int) []uint64 {
	c.mu.RLock()
	defer c.mu.RUnlock()

	var result []uint64

	for _, item := range c.items {
		if item.SKU == targetSKU && item.IsTradable {
			result = append(result, item.ID)
		}
	}

	return result
}

// HandleSubscribed processes the initial full synchronization of the cache.
func (c *SOCache) HandleSubscribed(pkt *protocol.GCPacket) {
	msg := &pb.CMsgSOCacheSubscribed{}
	if err := proto.Unmarshal(pkt.Payload, msg); err != nil {
		c.logger.Error("Failed to unmarshal SOCacheSubscribed", log.Err(err))
		return
	}

	c.version.Store(msg.GetVersion())
	c.ownerID.Store(msg.GetOwner())

	c.mu.Lock()
	defer c.mu.Unlock()

	clear(c.items)

	for _, subType := range msg.GetObjects() {
		typeID := subType.GetTypeId()
		for _, objData := range subType.GetObjectData() {
			c.processObject(typeID, objData, true)
		}
	}

	c.logger.Info("TF2 SOCache loaded/resynced",
		log.Int("items", len(c.items)),
		log.Uint64("version", msg.GetVersion()),
	)

	c.bus.Publish(&BackpackLoadedEvent{
		Count: len(c.items),
	})
}

// HandleSOUpdate routes incremental events (Create, Update, Destroy, Multiple).
func (c *SOCache) HandleSOUpdate(pkt *protocol.GCPacket) {
	msgType := pb.ESOMsg(pkt.MsgType &^ protocol.ProtoMask)

	var newVersion uint64

	c.mu.Lock()
	defer c.mu.Unlock()

	switch msgType {
	case pb.ESOMsg_k_ESOMsg_Create:
		msg := &pb.CMsgSOSingleObject{}
		if proto.Unmarshal(pkt.Payload, msg) == nil {
			newVersion = msg.GetVersion()
			c.processObject(msg.GetTypeId(), msg.GetObjectData(), false)
		}

	case pb.ESOMsg_k_ESOMsg_Update:
		msg := &pb.CMsgSOSingleObject{}
		if proto.Unmarshal(pkt.Payload, msg) == nil {
			newVersion = msg.GetVersion()
			c.processObject(msg.GetTypeId(), msg.GetObjectData(), false)
		}

	case pb.ESOMsg_k_ESOMsg_Destroy:
		msg := &pb.CMsgSOSingleObject{}
		if proto.Unmarshal(pkt.Payload, msg) == nil {
			newVersion = msg.GetVersion()
			c.processDestroy(msg.GetTypeId(), msg.GetObjectData())
		}

	case pb.ESOMsg_k_ESOMsg_UpdateMultiple:
		msg := &pb.CMsgSOMultipleObjects{}
		if err := proto.Unmarshal(pkt.Payload, msg); err == nil {
			newVersion = msg.GetVersion()

			for _, obj := range msg.GetObjects() {
				c.processObject(obj.GetTypeId(), obj.GetObjectData(), false)
			}
		} else {
			c.logger.Error("Failed to unmarshal SOMultipleObjects", log.Err(err))
		}
	}

	if newVersion > 0 {
		c.version.Store(newVersion)
	}
}

// HandleSOCacheCheck processes k_ESOMsg_CacheSubscriptionCheck (27).
// GC asks if we are still in sync.
func (c *SOCache) HandleSOCacheCheck(ctx context.Context, pkt *protocol.GCPacket) {
	msg := &pb.CMsgSOCacheSubscriptionCheck{}
	if err := proto.Unmarshal(pkt.Payload, msg); err != nil {
		c.logger.Error("Failed to unmarshal CacheSubscriptionCheck", log.Err(err))
		return
	}

	gcVersion := msg.GetVersion()
	ourVersion := c.version.Load()
	owner := msg.GetOwner()

	c.logger.Debug("Received SOCache Check",
		log.Uint64("gc_version", gcVersion),
		log.Uint64("our_version", ourVersion),
	)

	if gcVersion != ourVersion {
		c.logger.Warn("SOCache desync detected. Requesting refresh...",
			log.Uint64("expected", gcVersion),
			log.Uint64("actual", ourVersion),
		)
		c.requestRefresh(ctx, owner, c.logger)
	}
}

// HandleUpToDate processes k_ESOMsg_CacheSubscribedUpToDate (29).
// Sent by GC if we requested a Refresh but we already had the latest data.
func (c *SOCache) HandleUpToDate(pkt *protocol.GCPacket) {
	msg := &pb.CMsgSOCacheSubscribedUpToDate{}
	if err := proto.Unmarshal(pkt.Payload, msg); err == nil {
		c.version.Store(msg.GetVersion())
		c.logger.Debug("SOCache is up-to-date", log.Uint64("version", msg.GetVersion()))
	}
}

// requestRefresh sends k_ESOMsg_CacheSubscriptionRefresh (28) to the GC.
func (c *SOCache) requestRefresh(ctx context.Context, owner uint64, logger log.Logger) {
	req := &pb.CMsgSOCacheSubscriptionRefresh{
		Owner: proto.Uint64(owner),
	}

	err := c.coord.Send(ctx, AppID, uint32(pb.ESOMsg_k_ESOMsg_CacheSubscriptionRefresh), req)
	if err != nil {
		logger.Error("Failed to send CacheSubscriptionRefresh", log.Err(err))
	}
}

// processObject parses the raw bytes and updates the internal maps.
// Caller MUST hold the mutex.
func (c *SOCache) processObject(typeID int32, data []byte, isBulk bool) {
	switch typeID {
	case SOTypeEconItem: // Type 1: TF2 Item
		econItem := &pb.CSOEconItem{}
		if err := proto.Unmarshal(data, econItem); err != nil {
			c.logger.Error("Failed to unmarshal CSOEconItem", log.Err(err))
			return
		}

		item := c.protoToItem(econItem)
		if c.schema != nil {
			item.SKU = c.schema.GetSKUFromEconItem(item.ToEconItem())
		}

		// Check if it's an update or a new item
		_, exists := c.items[item.ID]
		c.items[item.ID] = item

		// Fire events only if we are not in the middle of initial bulk loading
		if !isBulk {
			if exists {
				c.bus.Publish(&ItemUpdatedEvent{Item: item})
				c.logger.Debug("Item updated in GC", log.Uint64("id", item.ID))
			} else {
				c.bus.Publish(&ItemAcquiredEvent{Item: item})
				c.logger.Debug("New item acquired from GC", log.Uint64("id", item.ID))
			}
		}

	case SOTypeEconGameAccountClient: // Type 7: Account Settings
		acc := &pb.CSOEconGameAccountClient{}
		if err := proto.Unmarshal(data, acc); err == nil {
			// TF2 gives you 50 slots by default. Premium gives +250 (300 total).
			// Backpack expanders add extra slots via bonus_backpack_slots.
			baseSlots := uint32(50)

			if acc.GetTrialAccount() {
				c.isPremium = false
			} else {
				c.isPremium = true
				baseSlots = 300
			}

			// We divide by 4 because bonus_backpack_slots is usually stored as a raw value that needs scaling,
			// or sometimes it's direct. (Check specific GC behavior if slots are miscalculated).
			// Usually, each expander gives 100 slots.
			c.slots = baseSlots + acc.GetAdditionalBackpackSlots()
		}
	}
}

// processDestroy handles the removal of items from the cache.
// Caller MUST hold the mutex.
func (c *SOCache) processDestroy(typeID int32, data []byte) {
	if typeID != SOTypeEconItem {
		return
	}

	econItem := &pb.CSOEconItem{}
	if err := proto.Unmarshal(data, econItem); err != nil {
		c.logger.Error("Failed to unmarshal CSOEconItem for destroy", log.Err(err))
		return
	}

	itemID := econItem.GetId()
	delete(c.items, itemID)

	c.bus.Publish(&ItemRemovedEvent{ItemID: itemID})
	c.logger.Debug("Item removed from GC", log.Uint64("id", itemID))
}

// protoToItem converts the raw Protobuf object into our internal struct.
func (c *SOCache) protoToItem(p *pb.CSOEconItem) *Item {
	item := &Item{
		ID:         p.GetId(),
		OriginalID: p.GetOriginalId(),
		DefIndex:   p.GetDefIndex(),
		Level:      p.GetLevel(),
		Quality:    p.GetQuality(),
		Inventory:  p.GetInventory(),
		Quantity:   p.GetQuantity(),
		Origin:     p.GetOrigin(),
		AccountID:  p.GetAccountId(),

		// By default, items are tradable unless a specific flag/attribute is set
		IsTradable: true,
	}

	// Extract attributes
	for _, attr := range p.GetAttribute() {
		// Custom Name (Attribute 111)
		if attr.GetDefIndex() == 111 {
			// Name is stored in value_bytes
			item.CustomName = string(attr.GetValueBytes())
		}

		// Custom Desc (Attribute 112)
		if attr.GetDefIndex() == 112 {
			item.CustomDesc = string(attr.GetValueBytes())
		}

		// Cannot Trade Flag (Attribute 153)
		if attr.GetDefIndex() == 153 {
			item.IsTradable = false
		}
	}

	// 1073741824 = unassigned position / unacknowledged (newly dropped item)
	if item.Inventory == 0 || item.Inventory >= 1073741824 {
		// Unacknowledged item
	}

	return item
}

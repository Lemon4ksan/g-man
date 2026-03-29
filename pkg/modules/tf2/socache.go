// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package tf2

import (
	"context"
	"sync"
	"sync/atomic"

	"github.com/lemon4ksan/g-man/pkg/log"
	gc "github.com/lemon4ksan/g-man/pkg/modules/coordinator/protocol"
	"github.com/lemon4ksan/g-man/pkg/steam/bus"
	pb "github.com/lemon4ksan/g-man/pkg/tf2/protobuf"
	"google.golang.org/protobuf/proto"
)

// SO Type IDs specific to Team Fortress 2.
const (
	SOTypeEconItem              int32 = 1
	SOTypeEconGameAccountClient int32 = 7
)

// SOCache maintains a live, in-memory mirror of the bot's TF2 inventory and account state.
// It is updated automatically in real-time by the Game Coordinator via SO messages.
type SOCache struct {
	mu sync.RWMutex

	items     map[uint64]*Item
	slots     uint32
	isPremium bool

	// Synchronization tracking
	version atomic.Uint64
	ownerID atomic.Uint64

	// Зависимость для отправки запроса Refresh
	coord CoordinatorProvider
}

// NewSOCache creates a new empty Shared Object Cache.
func NewSOCache(coord CoordinatorProvider) *SOCache {
	return &SOCache{
		items: make(map[uint64]*Item),
		coord: coord,
	}
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

// HandleSubscribed processes the initial full synchronization of the cache.
func (c *SOCache) HandleSubscribed(pkt *gc.Packet, logger log.Logger, b *bus.Bus) {
	msg := &pb.CMsgSOCacheSubscribed{}
	if err := proto.Unmarshal(pkt.Payload, msg); err != nil {
		logger.Error("Failed to unmarshal SOCacheSubscribed", log.Err(err))
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
			c.processObject(typeID, objData, logger, b, true)
		}
	}

	logger.Info("TF2 SOCache loaded/resynced",
		log.Int("items", len(c.items)),
		log.Uint64("version", msg.GetVersion()),
	)

	b.Publish(&BackpackLoadedEvent{
		Count: len(c.items),
	})
}

// HandleSOUpdate routes incremental events (Create, Update, Destroy, Multiple).
func (c *SOCache) HandleSOUpdate(pkt *gc.Packet, logger log.Logger, b *bus.Bus) {
	msgType := pb.ESOMsg(pkt.MsgType &^ gc.ProtoMask)

	var newVersion uint64

	c.mu.Lock()
	defer c.mu.Unlock()

	switch msgType {
	case pb.ESOMsg_k_ESOMsg_Create:
		msg := &pb.CMsgSOSingleObject{}
		if proto.Unmarshal(pkt.Payload, msg) == nil {
			newVersion = msg.GetVersion()
			c.processObject(msg.GetTypeId(), msg.GetObjectData(), logger, b, false)
		}
	case pb.ESOMsg_k_ESOMsg_Update:
		msg := &pb.CMsgSOSingleObject{}
		if proto.Unmarshal(pkt.Payload, msg) == nil {
			newVersion = msg.GetVersion()
			c.processObject(msg.GetTypeId(), msg.GetObjectData(), logger, b, false)
		}
	case pb.ESOMsg_k_ESOMsg_Destroy:
		msg := &pb.CMsgSOSingleObject{}
		if proto.Unmarshal(pkt.Payload, msg) == nil {
			newVersion = msg.GetVersion()
			c.processDestroy(msg.GetTypeId(), msg.GetObjectData(), logger, b)
		}
	case pb.ESOMsg_k_ESOMsg_UpdateMultiple:
		msg := &pb.CMsgSOMultipleObjects{}
		if err := proto.Unmarshal(pkt.Payload, msg); err == nil {
			newVersion = msg.GetVersion()

			for _, obj := range msg.GetObjects() {
				c.processObject(obj.GetTypeId(), obj.GetObjectData(), logger, b, false)
			}
		} else {
			logger.Error("Failed to unmarshal SOMultipleObjects", log.Err(err))
		}
	}

	if newVersion > 0 {
		c.version.Store(newVersion)
	}
}

// HandleCacheCheck processes k_ESOMsg_CacheSubscriptionCheck (27).
// GC asks if we are still in sync.
func (c *SOCache) HandleCacheCheck(ctx context.Context, pkt *gc.Packet, logger log.Logger) {
	msg := &pb.CMsgSOCacheSubscriptionCheck{}
	if err := proto.Unmarshal(pkt.Payload, msg); err != nil {
		logger.Error("Failed to unmarshal CacheSubscriptionCheck", log.Err(err))
		return
	}

	gcVersion := msg.GetVersion()
	ourVersion := c.version.Load()
	owner := msg.GetOwner()

	logger.Debug("Received SOCache Check",
		log.Uint64("gc_version", gcVersion),
		log.Uint64("our_version", ourVersion),
	)

	if gcVersion != ourVersion {
		logger.Warn("SOCache desync detected. Requesting refresh...",
			log.Uint64("expected", gcVersion),
			log.Uint64("actual", ourVersion),
		)
		c.requestRefresh(ctx, owner, logger)
	}
}

// HandleUpToDate processes k_ESOMsg_CacheSubscribedUpToDate (29).
// Sent by GC if we requested a Refresh but we already had the latest data.
func (c *SOCache) HandleUpToDate(pkt *gc.Packet, logger log.Logger) {
	msg := &pb.CMsgSOCacheSubscribedUpToDate{}
	if err := proto.Unmarshal(pkt.Payload, msg); err == nil {
		c.version.Store(msg.GetVersion())
		logger.Debug("SOCache is up-to-date", log.Uint64("version", msg.GetVersion()))
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
func (c *SOCache) processObject(typeID int32, data []byte, logger log.Logger, b *bus.Bus, isBulk bool) {
	switch typeID {
	case SOTypeEconItem: // Type 1: TF2 Item
		econItem := &pb.CSOEconItem{}
		if err := proto.Unmarshal(data, econItem); err != nil {
			logger.Error("Failed to unmarshal CSOEconItem", log.Err(err))
			return
		}

		item := c.protoToItem(econItem)

		// Check if it's an update or a new item
		_, exists := c.items[item.ID]
		c.items[item.ID] = item

		// Fire events only if we are not in the middle of initial bulk loading
		if !isBulk {
			if exists {
				b.Publish(&ItemUpdatedEvent{Item: item})
				logger.Debug("Item updated in GC", log.Uint64("id", item.ID))
			} else {
				b.Publish(&ItemAcquiredEvent{Item: item})
				logger.Debug("New item acquired from GC", log.Uint64("id", item.ID))
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
func (c *SOCache) processDestroy(typeID int32, data []byte, logger log.Logger, b *bus.Bus) {
	if typeID != SOTypeEconItem {
		return
	}

	econItem := &pb.CSOEconItem{}
	if err := proto.Unmarshal(data, econItem); err != nil {
		logger.Error("Failed to unmarshal CSOEconItem for destroy", log.Err(err))
		return
	}

	itemID := econItem.GetId()
	delete(c.items, itemID)

	b.Publish(&ItemRemovedEvent{ItemID: itemID})
	logger.Debug("Item removed from GC", log.Uint64("id", itemID))
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

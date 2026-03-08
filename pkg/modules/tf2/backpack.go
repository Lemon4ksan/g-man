// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package tf2

import (
	"context"
	"sync"

	"github.com/lemon4ksan/g-man/pkg/log"
	"github.com/lemon4ksan/g-man/pkg/modules/coordinator"
	"github.com/lemon4ksan/g-man/pkg/steam/bus"
	"github.com/lemon4ksan/g-man/pkg/steam/protocol/gc"

	pb "github.com/lemon4ksan/g-man/pkg/tf2/protobuf"
	"google.golang.org/protobuf/proto"
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
}

type Backpack struct {
	mu     sync.RWMutex
	items  map[uint64]*Item
	logger log.Logger
	bus    *bus.Bus

	isLoaded  bool
	isPremium bool
	slots     uint32
}

func NewBackpack(logger log.Logger, b *bus.Bus) *Backpack {
	return &Backpack{
		items:  make(map[uint64]*Item),
		logger: logger,
		bus:    b,
	}
}

func (b *Backpack) Items() []*Item {
	b.mu.RLock()
	defer b.mu.RUnlock()

	out := make([]*Item, 0, len(b.items))
	for _, it := range b.items {
		out = append(out, it)
	}
	return out
}

func (b *Backpack) IsLoaded() bool {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.isLoaded
}

// HandleCacheCheck handles the GC asking "do you have the latest data?".
// We usually just say "refresh please" to be safe or verify versions.
func (b *Backpack) HandleCacheCheck(coord *coordinator.Coordinator, steamID uint64) {
	b.logger.Debug("GC requested SO Cache Refresh")
	req := &pb.CMsgSOCacheSubscriptionRefresh{
		Owner: proto.Uint64(steamID),
	}
	coord.Send(context.Background(), AppID, uint32(pb.ESOMsg_k_ESOMsg_CacheSubscriptionRefresh), req)
}

func (b *Backpack) HandleSubscribed(pkt *gc.Packet) {
	msg := &pb.CMsgSOCacheSubscribed{}
	if err := proto.Unmarshal(pkt.Payload, msg); err != nil {
		b.logger.Error("Backpack: failed to unmarshal SO_Subscribed", log.Err(err))
		return
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	for _, obj := range msg.GetObjects() {
		b.processSOObject(obj.GetTypeId(), obj.GetObjectData())
	}

	b.isLoaded = true
	b.logger.Info("Backpack loaded", log.Int("items", len(b.items)))
	b.bus.Publish(&BackpackLoadedEvent{Count: len(b.items)})
}

func (b *Backpack) HandleCreate(pkt *gc.Packet) {
	msg := &pb.CMsgSOSingleObject{}
	if err := proto.Unmarshal(pkt.Payload, msg); err != nil {
		return
	}

	b.mu.Lock()
	item := b.processSingleSO(msg.GetTypeId(), msg.GetObjectData())
	b.mu.Unlock()

	if item != nil {
		b.bus.Publish(&ItemAcquiredEvent{Item: item})
	}
}

func (b *Backpack) HandleUpdate(pkt *gc.Packet) {
	msg := &pb.CMsgSOSingleObject{}
	if err := proto.Unmarshal(pkt.Payload, msg); err != nil {
		return
	}

	b.mu.Lock()
	item := b.processSingleSO(msg.GetTypeId(), msg.GetObjectData())
	b.mu.Unlock()

	if item != nil {
		// In a real app, you'd want to emit (OldItem, NewItem)
		b.bus.Publish(&ItemUpdatedEvent{Item: item})
	}
}

func (b *Backpack) HandleDestroy(pkt *gc.Packet) {
	msg := &pb.CMsgSOSingleObject{}
	if err := proto.Unmarshal(pkt.Payload, msg); err != nil {
		return
	}

	if msg.GetTypeId() == 1 { // Item
		protoItem := &pb.CSOEconItem{}
		if err := proto.Unmarshal(msg.GetObjectData(), protoItem); err == nil {
			id := protoItem.GetId()

			b.mu.Lock()
			delete(b.items, id)
			b.mu.Unlock()

			b.bus.Publish(&ItemRemovedEvent{ItemID: id})
		}
	}
}

func (b *Backpack) processSOObject(typeID int32, data [][]byte) {
	for _, d := range data {
		b.processSingleSO(typeID, d)
	}
}

func (b *Backpack) processSingleSO(typeID int32, data []byte) *Item {
	switch typeID {
	case 1: // CSOEconItem
		it := &pb.CSOEconItem{}
		if err := proto.Unmarshal(data, it); err == nil {
			item := b.protoToItem(it)
			b.items[item.ID] = item
			return item
		}
	case 7: // CSOEconGameAccountClient
		acc := &pb.CSOEconGameAccountClient{}
		if err := proto.Unmarshal(data, acc); err == nil {
			b.isPremium = !acc.GetTrialAccount()
			// Calculate slots: 50 (F2P) or 300 (Premium) + expanders
			base := uint32(50)
			if b.isPremium {
				base = 300
			}
			b.slots = base + acc.GetAdditionalBackpackSlots()
		}
	}
	return nil
}

func (b *Backpack) protoToItem(p *pb.CSOEconItem) *Item {
	return &Item{
		ID:           p.GetId(),
		DefIndex:     p.GetDefIndex(),
		Level:        p.GetLevel(),
		Quality:      p.GetQuality(),
		Position:     p.GetInventory() & 0xFFFF,
		AccountID:    p.GetAccountId(),
		Inventory:    p.GetInventory(),
		Origin:       p.GetOrigin(),
		Quantity:     p.GetQuantity(),
		OriginalID:   p.GetOriginalId(),
		CustomName:   p.GetCustomName(),
		CustomDesc:   p.GetCustomDesc(),
		IsTradable:   true, // Simplified; normally check p.GetAttribute() for un-tradable flags
		IsMarketable: true,
	}
}

// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package tf2

import (
	"github.com/lemon4ksan/g-man/pkg/bus"
)

// ConnectedEvent is emitted when the GC is connected.
type ConnectedEvent struct {
	bus.BaseEvent
	Version uint32
}

// DisconnectedEvent is emitted when the GC is disconnected.
type DisconnectedEvent struct {
	bus.BaseEvent
}

// BackpackLoadedEvent is emitted when the backpack is loaded.
type BackpackLoadedEvent struct {
	bus.BaseEvent
	Count int
}

// ItemAcquiredEvent is emitted when a new item is acquired.
type ItemAcquiredEvent struct {
	bus.BaseEvent
	Item *Item
}

// ItemRemovedEvent is emitted when an item is removed.
type ItemRemovedEvent struct {
	bus.BaseEvent
	ItemID uint64
}

// ItemUpdatedEvent is emitted when an item is updated.
type ItemUpdatedEvent struct {
	bus.BaseEvent
	Item *Item
}

// CraftResponseEvent is emitted when a craft request is finished.
type CraftResponseEvent struct {
	bus.BaseEvent
	BlueprintID  uint16
	CreatedItems []uint64
}

// TradeRequestEvent is emitted when another player invites us to trade via GC.
type TradeRequestEvent struct {
	bus.BaseEvent
	SteamID uint64
	TradeID uint32
}

// CraftingCompleteEvent is emitted when a craft request is finished.
type CraftingCompleteEvent struct {
	bus.BaseEvent
	RecipeID     int16
	ItemsCreated []uint64
}

// NotificationEvent is emitted when TF2 sends a client display notification
// (e.g., "You have new items!", or matchmaking alerts).
type NotificationEvent struct {
	bus.BaseEvent
	TitleLocalizationKey string
	BodyLocalizationKey  string
	ReplacementStrings   map[string]string
}

// ItemBroadcastEvent is emitted for global events (Golden Wrench, Saxxy, Something Special).
type ItemBroadcastEvent struct {
	bus.BaseEvent
	UserName       string
	WasDestruction bool
	DefIndex       uint32
}

// BackpackSortFinishedEvent is emitted when a sort request is completed by the GC.
type BackpackSortFinishedEvent struct {
	bus.BaseEvent
}

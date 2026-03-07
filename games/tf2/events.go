// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package tf2

import (
	"github.com/lemon4ksan/g-man/steam/bus"
)

type GCConnectedEvent struct {
	bus.BaseEvent
	Version uint32
}

type GCDisconnectedEvent struct {
	bus.BaseEvent
}

type BackpackLoadedEvent struct {
	bus.BaseEvent
	Count int
}

type ItemAcquiredEvent struct {
	bus.BaseEvent
	Item *Item
}

type ItemRemovedEvent struct {
	bus.BaseEvent
	ItemID uint64
}

type ItemUpdatedEvent struct {
	bus.BaseEvent
	Item *Item
}

type CraftResponseEvent struct {
	bus.BaseEvent
	BlueprintID  int16
	CreatedItems []uint64
}

// TradeRequestEvent is emitted when another player invites us to trade via GC.
type TradeRequestEvent struct {
	bus.BaseEvent
	SteamID uint64
	TradeID uint32
}

func (e *TradeRequestEvent) Topic() string { return "tf2.trade_request" }

// CraftingCompleteEvent is emitted when a craft request is finished.
type CraftingCompleteEvent struct {
	bus.BaseEvent
	RecipeID     int16
	ItemsCreated []uint64
}

func (e *CraftingCompleteEvent) Topic() string { return "tf2.crafting_complete" }

// NotificationEvent is emitted when TF2 sends a client display notification
// (e.g., "You have new items!", or matchmaking alerts).
type NotificationEvent struct {
	bus.BaseEvent
	TitleLocalizationKey string
	BodyLocalizationKey  string
	ReplacementStrings   map[string]string
}

func (e *NotificationEvent) Topic() string { return "tf2.notification" }

// ItemBroadcastEvent is emitted for global events (Golden Wrench, Saxxy, Something Special).
type ItemBroadcastEvent struct {
	bus.BaseEvent
	UserName       string
	WasDestruction bool
	DefIndex       uint32
}

func (e *ItemBroadcastEvent) Topic() string { return "tf2.item_broadcast" }

// BackpackSortFinishedEvent is emitted when a sort request is completed by the GC.
type BackpackSortFinishedEvent struct {
	bus.BaseEvent
}

func (e *BackpackSortFinishedEvent) Topic() string { return "tf2.backpack_sorted" }

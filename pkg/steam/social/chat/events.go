// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package chat

import (
	"time"

	"github.com/lemon4ksan/miyako/bus"
)

const (
	ChatEntryTypeChatMsg          = 1
	ChatEntryTypeTyping           = 2
	ChatEntryTypeLeftConversation = 6
	ChatEntryTypeEmote            = 8
	ChatEntryTypeSticker          = 10
)

// MessageEvent is published when a private text message arrives from a friend.
type MessageEvent struct {
	bus.BaseEvent
	SenderID  uint64
	Message   string
	Timestamp time.Time
	Ordinal   uint32
}

// StickerEvent is published when an animated sticker message arrives.
type StickerEvent struct {
	bus.BaseEvent
	SenderID  uint64
	StickerID string
	Timestamp time.Time
}

// TypingEvent is published when a friend sends a typing signal.
type TypingEvent struct {
	bus.BaseEvent
	SenderID uint64
}

// GroupMessageEvent is published when a message arrives in a group chat room.
type GroupMessageEvent struct {
	bus.BaseEvent
	ChatGroupID uint64
	ChatID      uint64
	SenderID    uint64
	Message     string
	Timestamp   time.Time
}

// ReactionEvent is published when an emoji reaction is updated on a private message.
type ReactionEvent struct {
	bus.BaseEvent
	FriendSteamID   uint64
	ReactorSteamID  uint64
	ServerTimestamp uint32
	Ordinal         uint32
	Reaction        string
	ReactionType    int32
	IsAdd           bool
}

// GroupReactionEvent is published when an emoji reaction is updated in a group chat channel.
type GroupReactionEvent struct {
	bus.BaseEvent
	ChatGroupID     uint64
	ChatID          uint64
	ReactorSteamID  uint64
	ServerTimestamp uint32
	Ordinal         uint32
	Reaction        string
	ReactionType    int32
	IsAdd           bool
}

// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package chat

import (
	"time"

	"github.com/lemon4ksan/g-man/pkg/bus"
)

// Chat message types.
const (
	ChatEntryTypeChatMsg          = 1
	ChatEntryTypeTyping           = 2
	ChatEntryTypeLeftConversation = 6
	ChatEntryTypeEmote            = 8  // e.g. :steamhappy:
	ChatEntryTypeSticker          = 10 // Animated sticker
)

// MessageEvent represents a standard text message from a friend.
type MessageEvent struct {
	bus.BaseEvent
	SenderID  uint64
	Message   string
	Timestamp time.Time
	// Ordinal helps correctly sequence messages that arrive out of order.
	Ordinal uint32
}

// StickerEvent represents an animated sticker message from a friend.
type StickerEvent struct {
	bus.BaseEvent
	SenderID  uint64
	StickerID string // ID or URL of the sticker
	Timestamp time.Time
}

// TypingEvent is fired when a friend starts typing a message.
type TypingEvent struct {
	bus.BaseEvent
	SenderID uint64
}

// GroupMessageEvent represents a text message in a group chat.
type GroupMessageEvent struct {
	bus.BaseEvent
	ChatGroupID uint64
	ChatID      uint64
	SenderID    uint64
	Message     string
	Timestamp   time.Time
}

// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package chat

import (
	"time"

	"github.com/lemon4ksan/miyako/bus"
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
	// SenderID is the 64-bit Steam ID of the user who sent the message.
	SenderID uint64
	// Message is the plain-text content of the message.
	Message string
	// Timestamp is the server time when the message was received.
	Timestamp time.Time
	// Ordinal helps correctly sequence messages that arrive out of order.
	Ordinal uint32
}

// StickerEvent represents an animated sticker message from a friend.
type StickerEvent struct {
	bus.BaseEvent
	// SenderID is the 64-bit Steam ID of the user who sent the sticker.
	SenderID uint64
	// StickerID is the unique identifier or URL of the sticker.
	StickerID string
	// Timestamp is the server time when the sticker was received.
	Timestamp time.Time
}

// TypingEvent is fired when a friend starts typing a message.
type TypingEvent struct {
	bus.BaseEvent
	// SenderID is the 64-bit Steam ID of the friend who is typing.
	SenderID uint64
}

// GroupMessageEvent represents a text message in a group chat.
type GroupMessageEvent struct {
	bus.BaseEvent
	// ChatGroupID is the unique identifier of the chat group.
	ChatGroupID uint64
	// ChatID is the unique identifier of the active channel inside the group.
	ChatID uint64
	// SenderID is the 64-bit Steam ID of the user who sent the message.
	SenderID uint64
	// Message is the plain-text content of the message.
	Message string
	// Timestamp is the server time when the message was received.
	Timestamp time.Time
}

// ReactionEvent represents a reaction to a friend message.
type ReactionEvent struct {
	bus.BaseEvent
	// FriendSteamID is the 64-bit Steam ID of the friend whose message was reacted to.
	FriendSteamID uint64
	// ReactorSteamID is the 64-bit Steam ID of the user who reacted to the message.
	ReactorSteamID uint64
	// ServerTimestamp is the server time when the reaction occurred.
	ServerTimestamp uint32
	// Ordinal helps correctly sequence reactions that arrive out of order.
	Ordinal uint32
	// Reaction is the string or emoji representing the reaction.
	Reaction string
	// ReactionType is the numeric type identifier of the reaction.
	ReactionType int32
	// IsAdd is true if the reaction was added, false if it was removed.
	IsAdd bool
}

// GroupReactionEvent represents a reaction to a message in a group chat room.
type GroupReactionEvent struct {
	bus.BaseEvent
	// ChatGroupID is the unique identifier of the chat group.
	ChatGroupID uint64
	// ChatID is the unique identifier of the active channel inside the group.
	ChatID uint64
	// ReactorSteamID is the 64-bit Steam ID of the user who reacted to the message.
	ReactorSteamID uint64
	// ServerTimestamp is the server time when the reaction occurred.
	ServerTimestamp uint32
	// Ordinal helps correctly sequence reactions that arrive out of order.
	Ordinal uint32
	// Reaction is the string or emoji representing the reaction.
	Reaction string
	// ReactionType is the numeric type identifier of the reaction.
	ReactionType int32
	// IsAdd is true if the reaction was added, false if it was removed.
	IsAdd bool
}

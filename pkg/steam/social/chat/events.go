// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package chat

import (
	"time"

	"github.com/lemon4ksan/g-man/pkg/bus"
)

const (
	ChatEntryTypeChatMsg          = 1
	ChatEntryTypeTyping           = 2
	ChatEntryTypeLeftConversation = 6
)

type MessageEvent struct {
	bus.BaseEvent
	SenderID  uint64
	Message   string
	Timestamp time.Time
	Ordinal   uint32
}

func (e *MessageEvent) Topic() string { return "chat.message_received" }

type TypingEvent struct {
	bus.BaseEvent
	SenderID uint64
}

func (e *TypingEvent) Topic() string { return "chat.typing" }

type GroupMessageEvent struct {
	bus.BaseEvent
	ChatGroupID uint64
	ChatID      uint64
	SenderID    uint64
	Message     string
	Timestamp   time.Time
}

func (e *GroupMessageEvent) Topic() string { return "chat.group_message_received" }

// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package chat manages one-on-one friend messages and Steam group chats.
//
// It provides real-time chat capabilities, tracks online friend typing status,
// and automates message history synchronization.
//
// # Key Components
//
//   - [Chat]: The central module manager that coordinates sending and receiving chat messages.
//   - [MessageEvent]: Emitted when a new plain-text message is received from a friend.
//   - [GroupMessageEvent]: Emitted when a new message is posted in a group chatroom.
//
// # Basic Usage Example
//
//	package main
//
//	import (
//		"context"
//		"fmt"
//		"github.com/lemon4ksan/g-man/pkg/bus"
//		"github.com/lemon4ksan/g-man/pkg/steam/social/chat"
//	)
//
//	func main() {
//		eventBus := bus.New()
//
//		// Subscribe to friend message events
//		sub := eventBus.Subscribe(&chat.MessageEvent{})
//		defer sub.Unsubscribe()
//
//		fmt.Println("Listening for chat messages...")
//		for ev := range sub.C() {
//			msgEvent := ev.(*chat.MessageEvent)
//			fmt.Printf("Received message from %d: %s\n", msgEvent.SenderID, msgEvent.Message)
//		}
//	}
package chat

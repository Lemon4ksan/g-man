// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package live manages real-time trade invitations via the Steam Connection Manager (CM).
//
// Unlike the web variant which polls for asynchronous trade offers, this
// module handles the immediate, pop-up style trade requests that occur when
// two users are online and agree to trade live.
//
// # Key Components
//
//   - [Manager]: The central service that orchestrates live trade invitations, responses, and session state.
//   - [TradeProposedEvent]: Emitted via the event bus when an incoming live trade request is received.
//   - [TradeResultEvent]: Emitted via the event bus when a trade proposal is answered or fails.
//   - [TradeSessionStartedEvent]: Emitted via the event bus when a live trade window successfully opens.
//
// # Basic Usage Example
//
//	package main
//
//	import (
//		"context"
//		"fmt"
//		"github.com/lemon4ksan/g-man/pkg/log"
//		"github.com/lemon4ksan/g-man/pkg/trading/live"
//	)
//
//	func main() {
//		ctx := context.Background()
//		logger := log.New(log.DefaultConfig(log.LevelInfo))
//
//		// Create a new live trade manager
//		manager := live.New()
//		partnerID := uint64(76561198000000000)
//
//		// Send invitation
//		err := manager.Invite(ctx, partnerID)
//		if err != nil {
//			fmt.Println("Failed to send invitation:", err)
//			return
//		}
//		fmt.Println("Trade invitation sent to:", partnerID)
//	}
package live

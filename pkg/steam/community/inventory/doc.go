// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package inventory allows retrieving user inventory using community requester.
//
// It provides functions to recursively parse user items, fetch inventory contexts,
// and parse detailed trade history.
//
// # Key Components
//
//   - [CEconItem]: Represents a standard item in the user's Steam inventory with its asset and description.
//   - [AppContext]: Represents an application context block specifying supported contexts (such as TF2 contexts).
//   - [TradeHistoryRow]: Represents a single completed or pending trade event parsed from inventory history.
//
// # Basic Usage Example
//
//	package main
//
//	import (
//		"context"
//		"fmt"
//		"github.com/lemon4ksan/g-man/pkg/steam/community"
//		"github.com/lemon4ksan/g-man/pkg/steam/community/inventory"
//	)
//
//	func main() {
//		ctx := context.Background()
//
//		// Mock sessionID extractor returning a dummy token
//		sessionFunc := func(url string) string {
//			return "mock_session_id_12345"
//		}
//
//		// Create a new Community Client
//		client := community.NewClient(nil, sessionFunc)
//
//		// Fetch inventory contents for Team Fortress 2 (AppID: 440, ContextID: 2)
//		items, currencies, total, err := inventory.GetUserInventoryContents(
//			ctx, client, 76561197960265728, 440, 2, true, "english",
//		)
//		if err != nil {
//			fmt.Println("Failed to retrieve inventory:", err)
//			return
//		}
//
//		fmt.Printf("Parsed %d items, %d currencies, total count: %d\n", len(items), len(currencies), total)
//	}
package inventory

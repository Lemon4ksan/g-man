// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package notifications provides a flexible system for generating user-facing
// chat messages based on trade outcomes.
//
// # Key Components
//
//   - [Manager]: The central service responsible for rendering and dispatching notifications using Go text templates.
//   - [TradeInfo]: Contains detailed metadata about a finalized trade, used to populate templates.
//   - [ChatProvider]: Defines the contract for sending messages to Steam users.
//   - [ConfigProvider]: Defines the contract for retrieving customized notification templates and prefixes.
//
// # Basic Usage Example
//
//	package main
//
//	import (
//		"context"
//		"fmt"
//		"github.com/lemon4ksan/g-man/pkg/log"
//		"github.com/lemon4ksan/g-man/pkg/steam/id"
//		"github.com/lemon4ksan/g-man/pkg/trading/notifications"
//	)
//
//	// MockChat implements notifications.ChatProvider for testing.
//	type MockChat struct{}
//
//	func (c MockChat) SendMessage(ctx context.Context, steamID id.ID, message string) error {
//		fmt.Println("Sent Chat:", message)
//		return nil
//	}
//
//	// MockConfig implements notifications.ConfigProvider for testing.
//	type MockConfig struct{}
//
//	func (c MockConfig) GetTemplate(key string) string {
//		return "Trade accepted! Thank you!"
//	}
//
//	func (c MockConfig) GetCommandPrefix() string {
//		return "!"
//	}
//
//	func main() {
//		ctx := context.Background()
//		logger := log.New(log.DefaultConfig(log.LevelInfo))
//
//		// Create and initialize the manager
//		mgr := notifications.NewManager(MockChat{}, MockConfig{}, logger)
//		partnerID := id.FromAccountID(123456)
//
//		// Prepare trade info for an accepted trade
//		info := &notifications.TradeInfo{
//			OfferID:        78910,
//			PartnerSteamID: partnerID,
//			OldState:       notifications.StateAccepted,
//		}
//
//		// Dispatch the notification
//		_ = mgr.SendNotification(ctx, info)
//	}
package notifications

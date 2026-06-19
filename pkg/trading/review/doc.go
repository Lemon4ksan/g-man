// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package review handles detailed reporting and alerting for bot administrators.
//
// It translates raw trade reasoning into human-readable summaries.
//
// # Key Components
//
//   - [Reviewer]: The primary service that compiles trade metadata into structured reports and dispatches alerts.
//   - [Formatter]: Determines the visual style and formatting rules for specific platforms (such as Steam chat or Discord webhooks).
//   - [TradeMetadata]: Holds all metadata, reasons, and timing details collected during trade offer processing.
//   - [Report]: Contains formatted strings ready for output.
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
//		"github.com/lemon4ksan/g-man/pkg/trading/review"
//	)
//
//	// MockSchema implements review.SchemaProvider for testing.
//	type MockSchema struct{}
//
//	func (s MockSchema) GetName(sku string, useDefindex bool) string {
//		return "Test Item Name"
//	}
//
//	// MockChat implements review.ChatProvider for testing.
//	type MockChat struct{}
//
//	func (c MockChat) SendMessage(ctx context.Context, steamID uint64, message string) error {
//		return nil
//	}
//
//	func (c MockChat) MessageAdmins(ctx context.Context, message string) error {
//		fmt.Println("Admin Alert:", message)
//		return nil
//	}
//
//	func main() {
//		logger := log.New(log.DefaultConfig(log.LevelInfo))
//		reviewer := review.New(MockSchema{}, MockChat{}, logger)
//
//		ctx := context.Background()
//		partnerID := id.FromAccountID(12345678)
//		meta := &review.TradeMetadata{
//			PrimaryReason: "OVERSTOCKED",
//			ProcessTimeMS: 45,
//		}
//
//		// Send alert to administrators
//		_ = reviewer.SendReviewAlert(ctx, 123456, partnerID, meta)
//	}
package review

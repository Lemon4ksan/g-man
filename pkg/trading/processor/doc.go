// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package processor acts as the central orchestrator for the trade management subsystem.
//
// It coordinates the sequential evaluation and execution of trade offers, ensuring
// concurrency safety and transaction integrity.
//
// # Key Components
//
//   - [Processor]: The primary orchestrator that coordinates sequential trade offer processing, asset locking, and action execution.
//   - [TradeExecutor]: Defines the contract for executing final trade actions (Accept/Decline) on Steam.
//
// # Basic Usage Example
//
//	package main
//
//	import (
//		"context"
//		"fmt"
//		"github.com/lemon4ksan/g-man/pkg/log"
//		"github.com/lemon4ksan/g-man/pkg/trading"
//		"github.com/lemon4ksan/g-man/pkg/trading/engine"
//		"github.com/lemon4ksan/g-man/pkg/trading/notifications"
//		"github.com/lemon4ksan/g-man/pkg/trading/processor"
//		"github.com/lemon4ksan/g-man/pkg/trading/review"
//	)
//
//	// MockExecutor implements processor.TradeExecutor for testing.
//	type MockExecutor struct{}
//
//	func (e MockExecutor) AcceptOffer(ctx context.Context, id uint64) error {
//		fmt.Println("Accepted offer:", id)
//		return nil
//	}
//
//	func (e MockExecutor) DeclineOffer(ctx context.Context, id uint64) error {
//		fmt.Println("Declined offer:", id)
//		return nil
//	}
//
//	func main() {
//		ctx := context.Background()
//		logger := log.New(log.DefaultConfig(log.LevelInfo))
//
//		// Initialize dependencies
//		exec := MockExecutor{}
//		eng := engine.New()
//		notif := &notifications.Manager{}
//		reviewer := &review.Reviewer{}
//
//		// Create and start the processor
//		p := processor.New(exec, eng, notif, reviewer, logger)
//		p.Start(ctx)
//	}
package processor

// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package engine implements a high-performance, middleware-based reasoning system for Steam trade offers.
//
// It follows the "Chain of Responsibility" (Onion) pattern, similar to modern web frameworks.
//
// # Key Components
//
//   - [Engine]: The central orchestrator that registers middleware and executes the reasoning pipeline.
//   - [TradeContext]: Carries the state, metadata, and decision verdict of a single trade offer as it passes through the pipeline.
//   - [Middleware]: A function type representing an isolated check or step in the reasoning pipeline.
//   - [Verdict]: Holds the final action and justification returned by the reasoning engine.
//
// # Basic Usage Example
//
//	package main
//
//	import (
//		"context"
//		"fmt"
//		"github.com/lemon4ksan/g-man/pkg/trading"
//		"github.com/lemon4ksan/g-man/pkg/trading/engine"
//	)
//
//	func main() {
//		ctx := context.Background()
//		eng := engine.New()
//
//		// Register a basic middleware that declines empty offers
//		eng.Use(func(next engine.Handler) engine.Handler {
//			return func(c *engine.TradeContext) error {
//				if len(c.Offer.ItemsToGive) > 0 && len(c.Offer.ItemsToReceive) == 0 {
//					c.Decline("begging")
//					return nil // Short-circuit: do not call next(c)
//				}
//				return next(c)
//			}
//		})
//
//		// Prepare a fake offer
//		offer := &trading.TradeOffer{
//			ID:             12345,
//			ItemsToGive:    []*trading.Item{{AssetID: 111}},
//			ItemsToReceive: []*trading.Item{},
//		}
//
//		// Process the offer
//		verdict, err := eng.Process(ctx, offer)
//		if err != nil {
//			fmt.Println("Processing failed:", err)
//			return
//		}
//
//		fmt.Printf("Decision: Action=%s, Reason=%s\n", verdict.Action, verdict.Reason)
//	}
package engine

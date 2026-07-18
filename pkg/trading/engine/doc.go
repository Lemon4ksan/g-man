// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package engine evaluates and processes incoming trade offers using a middleware pipeline.
//
// Register middleware handlers using [Engine.Use] to form a processing chain. Middlewares
// can short-circuit the pipeline by making a final decision (e.g., accepting or declining)
// and returning early without invoking the next handler.
//
// # Quick Start
//
// Set up an engine with a middleware that automatically declines begging offers:
//
//	eng := engine.New()
//
//	eng.Use(func(next engine.Handler) engine.Handler {
//		return func(c *engine.TradeContext) error {
//			if len(c.Offer.ItemsToGive) > 0 && len(c.Offer.ItemsToReceive) == 0 {
//				c.Decline("begging")
//				return nil // Short-circuit the pipeline
//			}
//			return next(c)
//		}
//	})
//
//	verdict, err := eng.Process(ctx, offer)
package engine

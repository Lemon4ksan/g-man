// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"context"
	"fmt"

	tf2trading "github.com/lemon4ksan/g-man/pkg/tf2/trading"
	"github.com/lemon4ksan/g-man/pkg/trading"
	"github.com/lemon4ksan/g-man/pkg/trading/engine"
	"github.com/lemon4ksan/g-man/pkg/trading/reason"
	tradingtest "github.com/lemon4ksan/g-man/test/trading"
)

// TF2 Attribute IDs (from schema)
const (
	AttrItemOrigin = 12345 // Placeholder for demo, in reality TF2 uses internal fields for Origin
)

func main() {
	fmt.Println("G-man: Advanced Trade Testing Engine Example")
	fmt.Println("--------------------------------------------")

	// 1. Initialize the TF2 Trade Tester with a base price feed.
	tester := tf2trading.NewTF2TradeTester().
		WithPrices(map[string]int{
			"5021;6": 60, // Key: 60 Ref
			"5002;6": 1,  // Refined Metal: 1 Ref
			"263;6":  5,  // Reclaimed Metal: 0.33 Ref (represented as 5 units in our internal int-math)
		})

	// 2. Add "Bulk Discount" Middleware
	// If a partner sells 10+ keys, we give them a 1 Ref bonus per key.
	tester.AddMiddleware(func(next engine.Handler) engine.Handler {
		return func(ctx *engine.TradeContext) error {
			keysToReceive := 0
			for _, it := range ctx.Offer.ItemsToReceive {
				if it.SKU == "5021;6" {
					keysToReceive++
				}
			}

			if keysToReceive >= 10 {
				fmt.Printf(
					"[Logic] Bulk seller detected! Applying 1 Ref bonus per key (%d bonus Ref).\n",
					keysToReceive,
				)
				ctx.Set("bulk_bonus", keysToReceive)
			}

			return next(ctx)
		}
	})

	// 3. Add Advanced Value Validator
	// This middleware calculates the total value and compares it, considering the bulk bonus.
	tester.AddMiddleware(func(next engine.Handler) engine.Handler {
		return func(ctx *engine.TradeContext) error {
			giveValue := 0
			recvValue := 0

			for _, it := range ctx.Offer.ItemsToGive {
				if val, ok := ctx.Get("price_" + it.SKU); ok {
					giveValue += val.(int)
				}
			}

			for _, it := range ctx.Offer.ItemsToReceive {
				if val, ok := ctx.Get("price_" + it.SKU); ok {
					recvValue += val.(int)
				}
			}

			// Apply bulk bonus if exists
			if bonus, ok := ctx.Get("bulk_bonus"); ok {
				recvValue += bonus.(int)
			}

			fmt.Printf("[Value] Give: %d | Receive: %d (incl. bonus)\n", giveValue, recvValue)

			if recvValue < giveValue {
				ctx.Decline(reason.TradeReason("insufficient_value"))
				return nil
			}

			ctx.Accept(reason.AcceptCorrectValue)

			return nil
		}
	})

	// We want to buy 10 keys. Total value is 600 Ref.
	// We give 610 Ref (thinking it's fair), but with our 10 Ref bonus (1 per key),
	// the received value effectively becomes 600 + 10 = 610.
	fmt.Println("\n>>> Scenario 1: Bulk Key Sale (10 keys) with 10 Ref bonus")

	bulkOffer := tradingtest.NewOfferBuilder().
		AddReceiveItem("5021;6", 10). // 10 Keys (600 Ref)
		AddGiveItem("5002;6", 610).   // We give 610 Ref (normally we'd decline, but bonus makes it 610)
		Build()

	verdict, _ := tester.Run(context.Background(), bulkOffer)
	fmt.Printf("Result: %s (Reason: %s)\n", verdict.Action, verdict.Reason)

	// Partner sells 9 keys (no bonus). Total value is 540 Ref.
	// They want 549 Ref.
	fmt.Println("\n>>> Scenario 2: 9 Keys (no bonus) for 549 Ref")

	cheaterOffer := tradingtest.NewOfferBuilder().
		AddReceiveItem("5021;6", 9). // 9 Keys (540 Ref)
		AddGiveItem("5002;6", 549).  // They want 549 Ref
		Build()

	verdict, _ = tester.Run(context.Background(), cheaterOffer)
	fmt.Printf("Result: %s (Reason: %s)\n", verdict.Action, verdict.Reason)

	// Giving 1 Key (60), receiving 55 Refined (55) and 1 Reclaimed (5). Total 60.
	fmt.Println("\n>>> Scenario 3: Mixed Currency Trade (1 Key for 55 Ref + 1 Rec)")

	mixedOffer := tradingtest.NewOfferBuilder().
		AddGiveItem("5021;6", 1).     // Give 1 Key (60)
		AddReceiveItem("5002;6", 55). // Receive 55 Ref (55)
		AddReceiveItem("263;6", 1).   // Receive 1 Rec (5)
		Build()

	verdict, _ = tester.Run(context.Background(), mixedOffer)
	fmt.Printf("Result: %s (Reason: %s)\n", verdict.Action, verdict.Reason)

	tester = tf2trading.NewTF2TradeTester().
		WithPrices(map[string]int{
			"5021;6": 60, // Key
			"200;6":  10, // Some Scattergun
		})

	// 1. "Loaner Skin Detector" Middleware
	// Loaner skins (Origin 24) are extremely rare and valuable to collectors.
	// If we detect one in our 'give' side, we should probably STOP and REVIEW.
	// If we detect one in 'receive' side, we might want to accept it as a huge win!
	tester.AddMiddleware(func(next engine.Handler) engine.Handler {
		return func(ctx *engine.TradeContext) error {
			for _, it := range ctx.Offer.ItemsToGive {
				for _, attr := range it.Attributes {
					if attr.Defindex == AttrItemOrigin && attr.Value == "24" {
						fmt.Printf("[ALARM] We are giving away a LOANER item! AssetID: %d\n", it.AssetID)
						ctx.Review(reason.TradeReason("LOANER_GIVEAWAY_PROTECTION"))
						return nil
					}
				}
			}

			for _, it := range ctx.Offer.ItemsToReceive {
				for _, attr := range it.Attributes {
					if attr.Defindex == AttrItemOrigin && attr.Value == "24" {
						fmt.Printf("[JACKPOT] Receiving a LOANER item! AssetID: %d\n", it.AssetID)
						ctx.Set("is_jackpot", true)
					}
				}
			}

			return next(ctx)
		}
	})

	// 2. Final Validator
	tester.AddMiddleware(func(next engine.Handler) engine.Handler {
		return func(ctx *engine.TradeContext) error {
			if jackpot, _ := ctx.Get("is_jackpot"); jackpot == true {
				ctx.Accept(reason.TradeReason("COLLECTOR_ITEM_JACKPOT"))
				return nil
			}

			// Standard value check...
			ctx.Accept(reason.AcceptCorrectValue)

			return nil
		}
	})

	fmt.Println("\n>>> Scenario 5: Accidental Loaner giveaway")

	dangerousOffer := tradingtest.NewOfferBuilder().
		AddGiveItemFull(&trading.Item{
			AssetID: 12345678,
			SKU:     "200;6",
			Attributes: []trading.Attribute{
				{Defindex: AttrItemOrigin, Value: "24"}, // LOANER FLAG
			},
		}).
		AddReceiveItem("5021;6", 1). // For 1 Key
		Build()

	verdict, _ = tester.Run(context.Background(), dangerousOffer)
	fmt.Printf("Result: %s (Reason: %s)\n", verdict.Action, verdict.Reason)

	fmt.Println("\n>>> Scenario 6: Receiving a Loaner skin")

	jackpotOffer := tradingtest.NewOfferBuilder().
		AddGiveItem("5021;6", 1). // We give 1 Key
		AddReceiveItemFull(&trading.Item{
			AssetID: 87654321,
			SKU:     "200;6",
			Attributes: []trading.Attribute{
				{Defindex: AttrItemOrigin, Value: "24"}, // LOANER FLAG
			},
		}).
		Build()

	verdict, _ = tester.Run(context.Background(), jackpotOffer)
	fmt.Printf("Result: %s (Reason: %s)\n", verdict.Action, verdict.Reason)
}

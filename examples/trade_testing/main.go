// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"context"
	"fmt"

	"github.com/lemon4ksan/g-man/pkg/trading"
	"github.com/lemon4ksan/g-man/pkg/trading/engine"
	"github.com/lemon4ksan/g-man/pkg/trading/reason"
	tradingtest "github.com/lemon4ksan/g-man/test/trading"
)

// Item Attribute IDs (generic representation)
const (
	AttrItemOrigin = 12345 // Attribute ID representing item origin/type
)

func main() {
	fmt.Println("G-man: Advanced Trade Testing Engine Example")
	fmt.Println("--------------------------------------------")

	// 1. Initialize the generic Trade Tester with a base price feed.
	tester := tradingtest.NewTradeTester[int]().
		WithPrices(map[string]int{
			"item_premium":      60, // Premium item, e.g., 60 currency units
			"item_currency":     1,  // Base currency item, e.g., 1 unit
			"item_sub_currency": 5,  // Sub-currency item, e.g., 5 units
		})

	// 2. Add "Bulk Discount" Middleware
	// If a partner sells 10+ premium items, we give them a 1 currency unit bonus per premium item.
	tester.AddMiddleware(func(next engine.Handler) engine.Handler {
		return func(ctx *engine.TradeContext) error {
			premiumToReceive := 0
			for _, it := range ctx.Offer.ItemsToReceive {
				if it.SKU == "item_premium" {
					premiumToReceive++
				}
			}

			if premiumToReceive >= 10 {
				fmt.Printf(
					"[Logic] Bulk seller detected! Applying 1 currency unit bonus per premium item (%d bonus units).\n",
					premiumToReceive,
				)
				ctx.Set("bulk_bonus", premiumToReceive)
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
				if val, ok := ctx.Get("price_" + it.SKU).Value(); ok {
					giveValue += val.(int)
				}
			}

			for _, it := range ctx.Offer.ItemsToReceive {
				if val, ok := ctx.Get("price_" + it.SKU).Value(); ok {
					recvValue += val.(int)
				}
			}

			// Apply bulk bonus if exists
			if bonus, ok := ctx.Get("bulk_bonus").Value(); ok {
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

	// We want to buy 10 premium items. Total value is 600 currency units.
	// We give 610 currency units, but with our 10 unit bonus (1 per premium item),
	// the received value effectively becomes 600 + 10 = 610.
	fmt.Println("\n>>> Scenario 1: Bulk Premium Sale (10 premium items) with 10 unit bonus")

	bulkOffer := tradingtest.NewOfferBuilder().
		AddReceiveItem("item_premium", 10). // 10 premium items (600 value)
		AddGiveItem("item_currency", 610).
		// We give 610 currency units (normally we'd decline, but bonus makes it 610)
		Build()

	verdict, _ := tester.Run(context.Background(), bulkOffer)
	fmt.Printf("Result: %s (Reason: %s)\n", verdict.Action, verdict.Reason)

	// Partner sells 9 premium items (no bonus). Total value is 540.
	// They want 549 currency units.
	fmt.Println("\n>>> Scenario 2: 9 Premium Items (no bonus) for 549 currency units")

	cheaterOffer := tradingtest.NewOfferBuilder().
		AddReceiveItem("item_premium", 9). // 9 premium items (540 value)
		AddGiveItem("item_currency", 549). // They want 549 currency units
		Build()

	verdict, _ = tester.Run(context.Background(), cheaterOffer)
	fmt.Printf("Result: %s (Reason: %s)\n", verdict.Action, verdict.Reason)

	// Giving 1 premium item (60), receiving 55 base currency (55) and 1 sub-currency (5). Total 60.
	fmt.Println("\n>>> Scenario 3: Mixed Currency Trade (1 Premium Item for 55 Currency + 1 Sub-Currency)")

	mixedOffer := tradingtest.NewOfferBuilder().
		AddGiveItem("item_premium", 1).         // Give 1 Premium Item (60)
		AddReceiveItem("item_currency", 55).    // Receive 55 base currency units (55)
		AddReceiveItem("item_sub_currency", 1). // Receive 1 sub-currency unit (5)
		Build()

	verdict, _ = tester.Run(context.Background(), mixedOffer)
	fmt.Printf("Result: %s (Reason: %s)\n", verdict.Action, verdict.Reason)

	tester = tradingtest.NewTradeTester[int]().
		WithPrices(map[string]int{
			"item_premium":     60, // Premium item
			"item_rare_weapon": 10, // A rare game weapon or skin
		})

	// 1. "Special Attribute Detector" Middleware
	// Items with a special origin (Origin 24) are extremely rare and valuable to collectors.
	// If we detect one in our 'give' side, we should probably STOP and REVIEW.
	// If we detect one in 'receive' side, we might want to accept it as a huge win!
	tester.AddMiddleware(func(next engine.Handler) engine.Handler {
		return func(ctx *engine.TradeContext) error {
			for _, it := range ctx.Offer.ItemsToGive {
				for _, attr := range it.Attributes {
					if attr.Defindex == AttrItemOrigin && attr.Value == "24" {
						fmt.Printf("[ALARM] We are giving away a SPECIAL item! AssetID: %d\n", it.AssetID)
						ctx.Review(reason.TradeReason("SPECIAL_GIVEAWAY_PROTECTION"))
						return nil
					}
				}
			}

			for _, it := range ctx.Offer.ItemsToReceive {
				for _, attr := range it.Attributes {
					if attr.Defindex == AttrItemOrigin && attr.Value == "24" {
						fmt.Printf("[JACKPOT] Receiving a SPECIAL item! AssetID: %d\n", it.AssetID)
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
			if jackpot, _ := ctx.Get("is_jackpot").Value(); jackpot == true {
				ctx.Accept(reason.TradeReason("COLLECTOR_ITEM_JACKPOT"))
				return nil
			}

			// Standard value check...
			ctx.Accept(reason.AcceptCorrectValue)

			return nil
		}
	})

	fmt.Println("\n>>> Scenario 4: Accidental Special Item giveaway")

	dangerousOffer := tradingtest.NewOfferBuilder().
		AddGiveItemFull(&trading.Item{
			AssetID: 12345678,
			SKU:     "item_rare_weapon",
			Attributes: []trading.Attribute{
				{Defindex: AttrItemOrigin, Value: "24"}, // SPECIAL FLAG
			},
		}).
		AddReceiveItem("item_premium", 1). // For 1 premium item
		Build()

	verdict, _ = tester.Run(context.Background(), dangerousOffer)
	fmt.Printf("Result: %s (Reason: %s)\n", verdict.Action, verdict.Reason)

	fmt.Println("\n>>> Scenario 5: Receiving a Special Item")

	jackpotOffer := tradingtest.NewOfferBuilder().
		AddGiveItem("item_premium", 1). // We give 1 premium item
		AddReceiveItemFull(&trading.Item{
			AssetID: 87654321,
			SKU:     "item_rare_weapon",
			Attributes: []trading.Attribute{
				{Defindex: AttrItemOrigin, Value: "24"}, // SPECIAL FLAG
			},
		}).
		Build()

	verdict, _ = tester.Run(context.Background(), jackpotOffer)
	fmt.Printf("Result: %s (Reason: %s)\n", verdict.Action, verdict.Reason)
}

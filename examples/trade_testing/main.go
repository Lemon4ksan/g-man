// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"context"
	"fmt"

	tradingtest "github.com/lemon4ksan/g-man/pkg/test/trading"
	"github.com/lemon4ksan/g-man/pkg/trading"
	"github.com/lemon4ksan/g-man/pkg/trading/engine"
	"github.com/lemon4ksan/g-man/pkg/trading/reason"
)

const AttrItemOrigin = 12345

func main() {
	fmt.Println("G-man: Advanced Trade Testing Engine Example")
	fmt.Println("--------------------------------------------")

	tester := tradingtest.NewTradeTester[int]().
		WithPrices(map[string]int{
			"item_premium":      60,
			"item_currency":     1,
			"item_sub_currency": 5,
		})

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

	fmt.Println("\n>>> Scenario 1: Bulk Premium Sale (10 premium items) with 10 unit bonus")

	bulkOffer := tradingtest.NewOfferBuilder().
		AddReceiveItem("item_premium", 10).
		AddGiveItem("item_currency", 610).
		Build()

	verdict, _ := tester.Run(context.Background(), bulkOffer)
	fmt.Printf("Result: %s (Reason: %s)\n", verdict.Action, verdict.Reason)

	fmt.Println("\n>>> Scenario 2: 9 Premium Items (no bonus) for 549 currency units")

	cheaterOffer := tradingtest.NewOfferBuilder().
		AddReceiveItem("item_premium", 9).
		AddGiveItem("item_currency", 549).
		Build()

	verdict, _ = tester.Run(context.Background(), cheaterOffer)
	fmt.Printf("Result: %s (Reason: %s)\n", verdict.Action, verdict.Reason)

	fmt.Println("\n>>> Scenario 3: Mixed Currency Trade (1 Premium Item for 55 Currency + 1 Sub-Currency)")

	mixedOffer := tradingtest.NewOfferBuilder().
		AddGiveItem("item_premium", 1).
		AddReceiveItem("item_currency", 55).
		AddReceiveItem("item_sub_currency", 1).
		Build()

	verdict, _ = tester.Run(context.Background(), mixedOffer)
	fmt.Printf("Result: %s (Reason: %s)\n", verdict.Action, verdict.Reason)

	tester = tradingtest.NewTradeTester[int]().
		WithPrices(map[string]int{
			"item_premium":     60,
			"item_rare_weapon": 10,
		})

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

	tester.AddMiddleware(func(next engine.Handler) engine.Handler {
		return func(ctx *engine.TradeContext) error {
			if jackpot, _ := ctx.Get("is_jackpot").Value(); jackpot == true {
				ctx.Accept(reason.TradeReason("COLLECTOR_ITEM_JACKPOT"))
				return nil
			}

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
				{Defindex: AttrItemOrigin, Value: "24"},
			},
		}).
		AddReceiveItem("item_premium", 1).
		Build()

	verdict, _ = tester.Run(context.Background(), dangerousOffer)
	fmt.Printf("Result: %s (Reason: %s)\n", verdict.Action, verdict.Reason)

	fmt.Println("\n>>> Scenario 5: Receiving a Special Item")

	jackpotOffer := tradingtest.NewOfferBuilder().
		AddGiveItem("item_premium", 1).
		AddReceiveItemFull(&trading.Item{
			AssetID: 87654321,
			SKU:     "item_rare_weapon",
			Attributes: []trading.Attribute{
				{Defindex: AttrItemOrigin, Value: "24"},
			},
		}).
		Build()

	verdict, _ = tester.Run(context.Background(), jackpotOffer)
	fmt.Printf("Result: %s (Reason: %s)\n", verdict.Action, verdict.Reason)
}

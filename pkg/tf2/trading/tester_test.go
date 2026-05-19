// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package trading_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"

	tf2trading "github.com/lemon4ksan/g-man/pkg/tf2/trading"
	"github.com/lemon4ksan/g-man/pkg/trading"
	"github.com/lemon4ksan/g-man/pkg/trading/engine"
	tradingTest "github.com/lemon4ksan/g-man/test/trading"
)

func TestTF2TradeTester(t *testing.T) {
	// A simple mock middleware that declines if any received item price is below 10
	mockMiddleware := func(next engine.Handler) engine.Handler {
		return func(ctx *engine.TradeContext) error {
			for _, item := range ctx.Offer.ItemsToReceive {
				val, ok := ctx.Get("price_" + item.SKU)
				if ok {
					price, ok := val.(int)
					if ok && price < 10 {
						ctx.Decline("price too low")
						return nil
					}
				}
			}

			return next(ctx)
		}
	}

	tester := tf2trading.NewTF2TradeTester().
		WithPrices(map[string]int{
			"LowValueSKU":  5,
			"HighValueSKU": 50,
		}).
		AddMiddleware(mockMiddleware)

	t.Run("Declines when condition met", func(t *testing.T) {
		offer := tradingTest.NewOfferBuilder().
			AddReceiveItem("LowValueSKU", 1).
			Build()

		verdict, err := tester.Run(context.Background(), offer)

		assert.NoError(t, err)
		assert.Equal(t, trading.ActionDecline, verdict.Action)
	})

	t.Run("Passes through when condition not met", func(t *testing.T) {
		offer := tradingTest.NewOfferBuilder().
			AddReceiveItem("HighValueSKU", 1).
			Build()

		verdict, err := tester.Run(context.Background(), offer)

		assert.NoError(t, err)
		// Since no middleware explicitly accepts, the default verdict behavior depends on the engine implementation.
		// Assuming the default is ActionSkip when nothing is hit.
		assert.Equal(t, trading.ActionSkip, verdict.Action)
	})
}

// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package pricedb

import (
	"github.com/lemon4ksan/g-man/pkg/log"
	"github.com/lemon4ksan/g-man/pkg/offer/engine"
)

// PricerMiddleware enriches trade context with prices from PriceDB.
func PricerMiddleware(db *Client, logger log.Logger) engine.Middleware {
	return func(next engine.Handler) engine.Handler {
		return func(ctx *engine.TradeContext) error {
			skus := make(map[string]bool)
			for _, item := range append(ctx.Offer.ItemsToGive, ctx.Offer.ItemsToReceive...) {
				skus[item.SKU] = true
			}

			var skuList []string
			for sku := range skus {
				skuList = append(skuList, sku)
			}

			prices, err := db.GetItemsBulk(ctx, skuList)
			if err != nil {
				logger.Warn("Failed to fetch prices, marking for review", log.Err(err))
				ctx.Review("Pricer API is down")
				return nil
			}

			priceMap := make(map[string]*Price)
			for _, p := range prices {
				priceMap[p.SKU] = p
			}

			ctx.Set("prices", priceMap)

			return next(ctx)
		}
	}
}

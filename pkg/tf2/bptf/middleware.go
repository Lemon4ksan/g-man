// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package bptf

import (
	"time"

	"github.com/lemon4ksan/g-man/pkg/log"
	"github.com/lemon4ksan/g-man/pkg/steam/id"
	"github.com/lemon4ksan/g-man/pkg/storage/memory"
	"github.com/lemon4ksan/g-man/pkg/tf2/pricedb"
	"github.com/lemon4ksan/g-man/pkg/trading"
	"github.com/lemon4ksan/g-man/pkg/trading/engine"
)

// SafetyMiddleware checks bans and user trust levels on backpack.tf.
func SafetyMiddleware(bptfClient *Client, cache *memory.TTLCache, logger log.Logger) engine.Middleware {
	return func(next engine.Handler) engine.Handler {
		return func(ctx *engine.TradeContext) error {
			steamID := ctx.Offer.OtherSteamID
			cacheKey := "bptf_user_" + steamID.String()

			var user V1User

			if cachedData, ok := cache.Get(cacheKey); ok {
				user = cachedData.(V1User)
			} else {
				resp, err := bptfClient.GetUsersInfo(ctx, []id.ID{steamID})
				if err != nil {
					logger.Warn("Reputation API error, skipping safety check", log.Err(err))
					return next(ctx)
				}

				u, ok := resp.Users[steamID]
				if !ok {
					return next(ctx)
				}

				user = u
				cache.Set(cacheKey, user, 2*time.Hour)
			}

			if user.Bans != nil {
				ctx.Decline("Partner is banned on backpack.tf")
				return nil
			}

			return next(ctx)
		}
	}
}

// ValueTierMiddleware determines the value of the partner's inventory.
func ValueTierMiddleware(bptfClient *Client) engine.Middleware {
	return func(next engine.Handler) engine.Handler {
		return func(ctx *engine.TradeContext) error {
			val, err := bptfClient.GetInventoryValues(ctx, ctx.Offer.OtherSteamID)
			if err != nil {
				return next(ctx)
			}

			ctx.Set("partner_inv_value", val.Value)

			if val.Value > 500 {
				ctx.Set("is_whale", true)
			}

			return next(ctx)
		}
	}
}

// BptfFallbackPricerMiddleware requests the price from bptf if it has not been set previously.
func BptfFallbackPricerMiddleware(manager *PriceManager) engine.Middleware {
	return func(next engine.Handler) engine.Handler {
		return func(ctx *engine.TradeContext) error {
			existingPricesRaw, _ := ctx.Get("prices")
			priceMap := existingPricesRaw.(map[string]*pricedb.Price)

			allItems := make([]*trading.Item, len(ctx.Offer.ItemsToGive)+len(ctx.Offer.ItemsToReceive))
			copy(allItems, ctx.Offer.ItemsToGive)
			copy(allItems[len(ctx.Offer.ItemsToGive):], ctx.Offer.ItemsToReceive)

			for _, item := range allItems {
				if _, ok := priceMap[item.SKU]; ok {
					continue
				}

				if bptfPrice, ok := manager.GetPrice(item.SKU); ok {
					priceMap[item.SKU] = &pricedb.Price{
						Buy:  pricedb.Currencies{Keys: int(bptfPrice.Value), Metal: bptfPrice.ValueRaw},
						Sell: pricedb.Currencies{Keys: int(bptfPrice.Value), Metal: bptfPrice.ValueRaw},
					}
				}
			}

			return next(ctx)
		}
	}
}

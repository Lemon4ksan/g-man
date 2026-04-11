// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package engine

import (
	"errors"
	"fmt"
	"math"
	"slices"
	"time"

	"github.com/lemon4ksan/g-man/pkg/log"
	"github.com/lemon4ksan/g-man/pkg/steam/id"
	"github.com/lemon4ksan/g-man/pkg/tf2"
	"github.com/lemon4ksan/g-man/pkg/tf2/crafting"
	"github.com/lemon4ksan/g-man/pkg/tf2/currency"
	"github.com/lemon4ksan/g-man/pkg/tf2/pricedb"
	"github.com/lemon4ksan/g-man/pkg/trading/reason"
)

// RecoverMiddleware catches panics in the middleware chain and converts them to errors.
// This ensures one broken check doesn't crash the entire bot.
func RecoverMiddleware(logger log.Logger) Middleware {
	return func(next Handler) Handler {
		return func(ctx *TradeContext) (err error) {
			defer func() {
				if r := recover(); r != nil {
					err = fmt.Errorf("panic in trade engine: %v", r)
					logger.Error(
						"Trade engine recovered from panic",
						log.Any("panic", r),
						log.Uint64("offer_id", ctx.Offer.ID),
					)
					ctx.Review("Internal engine error (panic)")
				}
			}()

			return next(ctx)
		}
	}
}

// LoggerMiddleware measures and logs the time taken to process an offer,
// along with the final verdict.
func LoggerMiddleware(logger log.Logger) Middleware {
	return func(next Handler) Handler {
		return func(ctx *TradeContext) error {
			start := time.Now()

			err := next(ctx)
			duration := time.Since(start)

			logger.Info("Trade offer processed",
				log.Uint64("offer_id", ctx.Offer.ID),
				log.String("verdict", ctx.Verdict.Action.String()),
				log.String("reason", ctx.Verdict.Reason.String()),
				log.Duration("duration", duration),
			)

			return err
		}
	}
}

// BlacklistMiddleware rejects offers from specific SteamIDs.
func BlacklistMiddleware(blacklist []id.ID) Middleware {
	return func(next Handler) Handler {
		return func(ctx *TradeContext) error {
			// Check precondition
			if slices.Contains(blacklist, ctx.Offer.OtherSteamID) {
				// We found a match! Decline the offer and DO NOT call next(ctx).
				// This breaks the chain and returns immediately.
				ctx.Decline("User is blacklisted")
				return nil
			}

			// Precondition passed, continue down the chain
			return next(ctx)
		}
	}
}

// EmptyOfferMiddleware automatically declines offers where the partner
// asks for items but offers nothing in return (Begging).
func EmptyOfferMiddleware() Middleware {
	return func(next Handler) Handler {
		return func(ctx *TradeContext) error {
			gaveItems := len(ctx.Offer.ItemsToReceive) > 0 // We receive = they gave
			tookItems := len(ctx.Offer.ItemsToGive) > 0    // We give = they took

			if tookItems && !gaveItems {
				ctx.Decline("You are asking for free items")
				return nil
			}

			if gaveItems && !tookItems {
				ctx.Accept("Donation received, thank you!")
				return nil // Stop chain, accept immediately
			}

			return next(ctx)
		}
	}
}

// PricerMiddleware enriches trade context with prices from PriceDB.
func PricerMiddleware(db *pricedb.Client, logger log.Logger) Middleware {
	return func(next Handler) Handler {
		return func(ctx *TradeContext) error {
			skus := make(map[string]bool)
			for _, item := range append(ctx.Offer.ItemsToGive, ctx.Offer.ItemsToReceive...) {
				skus[item.SKU] = true
			}

			skuList := make([]string, 0, len(skus))
			for sku := range skus {
				skuList = append(skuList, sku)
			}

			prices, err := db.GetItemsBulk(ctx, skuList)
			if err != nil {
				logger.Warn("Failed to fetch prices, marking for review", log.Err(err))
				ctx.Review("Pricer API is down")

				return nil
			}

			priceMap := make(map[string]*pricedb.Price)
			for _, p := range prices {
				priceMap[p.SKU] = p
			}

			ctx.Set("prices", priceMap)

			return next(ctx)
		}
	}
}

// CounterOfferMiddleware automatically adds metal to the trade
// if the user gives us an item but forgets to put our currency in return.
func CounterOfferMiddleware(metalMgr *crafting.MetalManager, pricer *pricedb.Client, logger log.Logger) Middleware {
	return func(next Handler) Handler {
		return func(ctx *TradeContext) error {
			err := next(ctx)

			if ctx.Verdict.Action == ActionDecline {
				return err
			}

			valueDiffScrap := calculateValueDiff(ctx)

			if valueDiffScrap > 0 {
				changeIDs, err := metalMgr.SelectChange(valueDiffScrap)
				if err != nil {
					if errors.Is(err, crafting.ErrNotEnoughChange) {
						logger.Warn("Not enough metal for change, triggering auto-crafting...")
						// TODO: Smelt scrap / duplicates
						ctx.Decline("I don't have enough small metal for exact change right now.")

						return nil
					}

					return err
				}

				ctx.Verdict.Action = ActionCounter
				ctx.Verdict.Reason = "Added exact change automatically"
				ctx.Verdict.Data = changeIDs
			}

			return err
		}
	}
}

func ChangeMiddleware(metalMgr *crafting.MetalManager, craftingSvc *tf2.TF2, logger log.Logger) Middleware {
	return func(next Handler) Handler {
		return func(ctx *TradeContext) error {
			if err := next(ctx); err != nil {
				return err
			}

			if ctx.Verdict.Action != ActionUndecided {
				return nil
			}

			diffVar, ok := ctx.Get("value_diff_scrap")
			if !ok {
				return nil
			}

			diff := diffVar.(currency.Scrap)

			if diff == 0 {
				ctx.Accept("Correct value provided")
				return nil
			}

			if diff > 0 {
				ids, err := metalMgr.SelectChange(diff)
				if err != nil {
					if err := metalMgr.TryToSmeltForChange(ctx, diff); err == nil {
						return nil
					}

					ctx.Decline("I don't have enough small metal to give you change.")

					return nil
				}

				ctx.Verdict.Action = ActionCounter
				ctx.Verdict.Reason = "Added change automatically"
				ctx.Verdict.Data = ids
			} else {
				ctx.Decline(
					reason.TradeReason(
						fmt.Sprintf("You are missing %f", currency.ToRefined(currency.Scrap(math.Abs(float64(diff))))),
					),
				)
			}

			return nil
		}
	}
}

// calculateValueDiff calculates the difference in value between what we receive and what we give.
// Result > 0: We were overpaid (need change).
// Result < 0: We were underpaid (we should reject or request more).
func calculateValueDiff(ctx *TradeContext) currency.Scrap {
	pricesRaw, ok := ctx.Get("prices")
	if !ok {
		return 0
	}

	priceMap := pricesRaw.(map[string]*pricedb.Price)

	var keyPriceScrap currency.Scrap = 900
	if keyPrice, ok := priceMap[currency.SKUKey]; ok {
		// Convert key buy price to scrap
		keyPriceScrap = currency.ToScrap(keyPrice.Buy.Metal)
	}

	var ourTotal, theirTotal currency.Scrap

	for _, item := range ctx.Offer.ItemsToGive {
		if p, ok := priceMap[item.SKU]; ok {
			val := currency.Scrap(p.Sell.Keys)*keyPriceScrap + currency.ToScrap(p.Sell.Metal)
			ourTotal += currency.Scrap(val)
		}
	}

	for _, item := range ctx.Offer.ItemsToReceive {
		if p, ok := priceMap[item.SKU]; ok {
			val := currency.Scrap(p.Buy.Keys)*keyPriceScrap + currency.ToScrap(p.Buy.Metal)
			theirTotal += currency.Scrap(val)
		}
	}

	diff := currency.NewValueDiff(ourTotal, theirTotal, keyPriceScrap)

	ctx.Set("value_diff_scrap", diff.Diff())
	ctx.Set("is_profitable", diff.IsProfitable())

	return diff.Diff()
}

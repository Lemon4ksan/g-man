// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package engine

import (
	"fmt"
	"time"

	"github.com/lemon4ksan/miyako/generic"
	"github.com/lemon4ksan/miyako/log"

	"github.com/lemon4ksan/g-man/pkg/steam/id"
	"github.com/lemon4ksan/g-man/pkg/trading"
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
					logger.ErrorContext(
						ctx,
						"Trade engine recovered from panic",
						log.Any("panic", r),
						log.Uint64("offer_id", ctx.Offer.ID),
					)
					ctx.Review(reason.ReviewEngineError)
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

			logger.InfoContext(ctx, "Trade offer processed",
				log.Uint64("offer_id", ctx.Offer.ID),
				log.String("verdict", string(ctx.Verdict.Action)),
				log.String("reason", ctx.Verdict.Reason.String()),
				log.Duration("duration", duration),
			)

			return err
		}
	}
}

// BlacklistMiddleware rejects offers from specific SteamIDs.
func BlacklistMiddleware(blacklist generic.Set[id.ID]) Middleware {
	return func(next Handler) Handler {
		return func(ctx *TradeContext) error {
			if blacklist.Has(ctx.Offer.OtherSteamID) {
				ctx.Decline(reason.DeclineBlacklisted)
				return nil
			}

			// Precondition passed, continue down the chain
			return next(ctx)
		}
	}
}

// EmptyOfferMiddleware automatically declines offers where the partner
// asks for items but offers nothing in return (Begging).
// It also handles donations (where we take items for free), optionally filtering "junk".
func EmptyOfferMiddleware(isJunk func(*trading.Item) bool) Middleware {
	return func(next Handler) Handler {
		return func(ctx *TradeContext) error {
			gaveItems := len(ctx.Offer.ItemsToReceive) > 0 // We receive = they gave
			tookItems := len(ctx.Offer.ItemsToGive) > 0    // We give = they took

			if tookItems && !gaveItems {
				ctx.Decline(reason.DeclineBegging)
				return nil
			}

			if gaveItems && !tookItems {
				allJunk := true
				for _, it := range ctx.Offer.ItemsToReceive {
					if !it.Tradable {
						ctx.Decline(reason.DeclineBegging) // Don't even review untradable junk
						return nil
					}

					// If we have a validator, use it to check for junk
					if isJunk != nil {
						if !isJunk(it) {
							allJunk = false
						}
					} else {
						// Default: if it has a SKU, it's probably not junk
						if it.SKU != "" {
							allJunk = false
						}
					}
				}

				if allJunk {
					ctx.Decline(reason.DeclineJunkDonation)
					return nil
				}

				ctx.Accept(reason.AcceptDonation)

				return nil // Stop chain, accept immediately
			}

			return next(ctx)
		}
	}
}

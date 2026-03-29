// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package engine

import (
	"fmt"
	"slices"
	"time"

	"github.com/lemon4ksan/g-man/pkg/log"
)

// RecoverMiddleware catches panics in the middleware chain and converts them to errors.
// This ensures one broken check doesn't crash the entire bot.
func RecoverMiddleware(logger log.Logger) Middleware {
	return func(next Handler) Handler {
		return func(ctx *TradeContext) (err error) {
			defer func() {
				if r := recover(); r != nil {
					err = fmt.Errorf("panic in trade engine: %v", r)
					logger.Error("Trade engine recovered from panic", log.Any("panic", r), log.Uint64("offer_id", ctx.Offer.ID))
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
func BlacklistMiddleware(blacklist []uint64) Middleware {
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

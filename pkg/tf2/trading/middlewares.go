// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package trading

import (
	"context"
	"errors"
	"fmt"
	"maps"

	"github.com/lemon4ksan/g-man/pkg/log"
	"github.com/lemon4ksan/g-man/pkg/steam/id"
	"github.com/lemon4ksan/g-man/pkg/tf2/backpack"
	"github.com/lemon4ksan/g-man/pkg/tf2/bptf"
	"github.com/lemon4ksan/g-man/pkg/tf2/crafting"
	"github.com/lemon4ksan/g-man/pkg/tf2/currency"
	"github.com/lemon4ksan/g-man/pkg/tf2/pricedb"
	tf2reason "github.com/lemon4ksan/g-man/pkg/tf2/reason"
	"github.com/lemon4ksan/g-man/pkg/tf2/rep"
	"github.com/lemon4ksan/g-man/pkg/tf2/sku"
	"github.com/lemon4ksan/g-man/pkg/trading"
	"github.com/lemon4ksan/g-man/pkg/trading/engine"
	"github.com/lemon4ksan/g-man/pkg/trading/reason"
)

// PartnerInventoryProvider is an interface that allows fetching inventory of a trade partner.
type PartnerInventoryProvider interface {
	GetPartnerInventory(ctx context.Context, partnerID id.ID) ([]*trading.Item, error)
}

// StockConfig defines the limits for the inventory.
type StockConfig struct {
	MaxTotal   int            // Global limit (e.g. 3000)
	MaxPerSKU  map[string]int // Per-SKU limit
	DefaultMax int            // Default limit for any SKU not in the map
}

// StockLimitMiddleware checks if the trade would exceed inventory limits.
func StockLimitMiddleware(bp *backpack.Backpack, cfg StockConfig, logger log.Logger) engine.Middleware {
	return func(next engine.Handler) engine.Handler {
		return func(ctx *engine.TradeContext) error {
			if len(ctx.Offer.ItemsToReceive) == 0 {
				return next(ctx)
			}

			currentTotal := bp.GetTotalCount()
			incomingCount := len(ctx.Offer.ItemsToReceive)
			outgoingCount := len(ctx.Offer.ItemsToGive)

			if currentTotal+incomingCount-outgoingCount > cfg.MaxTotal {
				logger.Warn("Trade would exceed total inventory limit",
					log.Int("current", currentTotal),
					log.Int("incoming", incomingCount),
					log.Int("max", cfg.MaxTotal),
				)
				ctx.Decline(reason.ReviewOverstocked)

				return nil
			}

			incomingPerSKU := make(map[string]int)
			for _, it := range ctx.Offer.ItemsToReceive {
				sku := it.SKU
				incomingPerSKU[sku]++
			}

			for sku, count := range incomingPerSKU {
				max, ok := cfg.MaxPerSKU[sku]
				if !ok {
					max = cfg.DefaultMax
				}

				if max <= 0 {
					continue // No limit for this SKU
				}

				currentStock := bp.GetStock(sku)
				if currentStock+count > max {
					logger.Warn("Trade would exceed SKU stock limit",
						log.String("sku", sku),
						log.Int("current", currentStock),
						log.Int("incoming", count),
						log.Int("max", max),
					)
					ctx.Decline(reason.DeclineOverstocked)

					return nil
				}
			}

			return next(ctx)
		}
	}
}

// PricerMiddleware enriches trade context with prices from PriceDB.
// It acts as the primary price authority:
// 1. Checks local cache for prices.
// 2. If missing, requests them from the PriceDB microservice.
// 3. Flags the trade for manual review if any item remains unpriced.
func PricerMiddleware(mgr *pricedb.Manager, logger log.Logger) engine.Middleware {
	return func(next engine.Handler) engine.Handler {
		return func(ctx *engine.TradeContext) error {
			skus := make(map[string]bool)
			for _, item := range append(ctx.Offer.ItemsToGive, ctx.Offer.ItemsToReceive...) {
				skus[item.SKU] = true
			}

			skuList := make([]string, 0)
			priceMap := make(map[string]*pricedb.Price)

			for sku := range skus {
				if p, ok := mgr.GetPrice(sku); ok {
					priceMap[sku] = p
				} else {
					skuList = append(skuList, sku)
					// Automatically watch SKUs encountered in trades
					mgr.Watch(sku)
				}
			}

			if len(skuList) > 0 {
				fetched, err := mgr.Fetch(ctx, skuList)
				if err != nil {
					logger.Warn("Failed to fetch prices from PriceDB", log.Err(err))
					ctx.Review(tf2reason.ReviewPricerDown)
					return err
				}

				maps.Copy(priceMap, fetched)
			}

			ctx.Set("prices", priceMap)

			// Check if all items in the trade are priced
			for _, item := range append(ctx.Offer.ItemsToGive, ctx.Offer.ItemsToReceive...) {
				if _, ok := priceMap[item.SKU]; !ok {
					logger.Warn("Item in trade is not priced", log.String("sku", item.SKU))
					ctx.Review(tf2reason.ReviewUnpricedItem)
					return errors.New("unpriced item in trade")
				}
			}

			return next(ctx)
		}
	}
}

// EscrowChecker is an interface for checking if a trade offer has a trade hold (escrow).
type EscrowChecker interface {
	CheckEscrow(ctx context.Context, offer *trading.TradeOffer) (bool, error)
}

// EscrowMiddleware checks if there is a trade hold (escrow) on the offer.
// If either party has a trade hold, the offer is declined.
func EscrowMiddleware(checker EscrowChecker, logger log.Logger) engine.Middleware {
	return func(next engine.Handler) engine.Handler {
		return func(ctx *engine.TradeContext) error {
			hasEscrow, err := checker.CheckEscrow(ctx, ctx.Offer)
			if err != nil {
				logger.Warn("Failed to check escrow", log.Err(err))
				// It's safer to review if we can't determine escrow status
				ctx.Review(reason.ReviewEscrowCheckFailed)

				return nil
			}

			if hasEscrow {
				logger.Warn("Trade has escrow (trade hold)", log.Uint64("offerID", ctx.Offer.ID))
				ctx.Decline(reason.DeclineEscrow)
				return nil
			}

			return next(ctx)
		}
	}
}

// DupeCheckMiddleware checks the history of high-value items to identify duplicates.
func DupeCheckMiddleware(checker *bptf.BackpackTFChecker, logger log.Logger) engine.Middleware {
	return func(next engine.Handler) engine.Handler {
		return func(ctx *engine.TradeContext) error {
			// Only check history for items we RECEIVE
			for _, item := range ctx.Offer.ItemsToReceive {
				// We only check history for high-value items (Unusuals)
				// to avoid excessive scraping/API calls.
				if item.SKU == "" {
					continue
				}

				// Basic check: is it Unusual?
				// SKU format: 5021;5;... (5 is quality unusual)
				if isUnusual(item.SKU) {
					logger.Info(
						"Checking history for Unusual item",
						log.String("sku", item.SKU),
						log.Uint64("assetid", item.AssetID),
					)

					status, err := checker.CheckHistory(ctx, item.AssetID)
					if err != nil {
						logger.Warn("Failed to check item history", log.Err(err))
						continue // Proceed if check fails, but maybe Review is safer?
					}

					if status.Recorded && status.IsDuped {
						logger.Warn("Item is DUPED!", log.Uint64("assetid", item.AssetID))
						ctx.Review(tf2reason.ReviewDupedItems)
						// We don't return nil here, we just mark for review
						// and let subsequent middlewares decide if they want to decline or continue.
					}
				}
			}

			return next(ctx)
		}
	}
}

// BanCheckMiddleware checks the trade partner against various ban lists.
func BanCheckMiddleware(bans *rep.BansManager, logger log.Logger) engine.Middleware {
	return func(next engine.Handler) engine.Handler {
		return func(ctx *engine.TradeContext) error {
			res, err := bans.CheckBans(ctx, ctx.Offer.OtherSteamID)
			if err != nil {
				logger.Warn("Failed to check partner bans", log.Err(err))
				// If check fails, we proceed but maybe we should Review?
				// To be safe, let's just proceed to next middleware.
				return next(ctx)
			}

			if res.IsBanned {
				logger.Warn("Partner is banned!",
					log.String("steamid", ctx.Offer.OtherSteamID.String()),
					log.Any("details", res.Details),
				)

				if _, ok := res.Details["steamrep.com"]; ok {
					ctx.Decline(reason.DeclineBanned)
				} else {
					ctx.Decline(tf2reason.DeclineBannedBptf)
				}

				return nil
			}

			return next(ctx)
		}
	}
}

// SmartCounterMiddleware automatically adjusts the trade if there's a value mismatch.
// This is the core "settlement" logic of the bot:
// 1. Overpaid: Bot adds metal change to our side (using MetalManager).
// 2. Underpaid: Bot scans partner's inventory for missing currency to balance the trade.
// 3. Exact: Trade is accepted as correct.
func SmartCounterMiddleware(
	metalMgr *crafting.MetalManager,
	bp *backpack.Backpack,
	invProvider PartnerInventoryProvider,
	logger log.Logger,
) engine.Middleware {
	return func(next engine.Handler) engine.Handler {
		return func(ctx *engine.TradeContext) error {
			if err := next(ctx); err != nil {
				return err
			}

			// If a verdict is already reached, don't intervene
			if ctx.Verdict.Action != engine.ActionUndecided {
				return nil
			}

			diff, err := calculateValueDiff(ctx)
			if err != nil {
				// If we can't calculate value (missing prices), calculation logic already set Review status.
				return nil //nolint:nilerr
			}

			if diff == 0 {
				ctx.Accept(reason.AcceptCorrectValue)
				return nil
			}

			if diff > 0 {
				// We were overpaid -> give change
				changeIDs, err := metalMgr.SelectChange(diff)
				if err != nil {
					if errors.Is(err, crafting.ErrNotEnoughChange) {
						logger.Warn("Not enough metal for change, triggering auto-crafting...")

						if smeltErr := metalMgr.TryToSmeltForChange(ctx, diff); smeltErr == nil {
							// Smelting successful, it will be handled in a retry or next run
							return nil
						}

						ctx.Decline(tf2reason.DeclineNoChange)

						return nil
					}

					return err
				}

				ctx.Counter(reason.AcceptCorrectValue, &trading.CounterParams{
					ItemsToGive:    append(ctx.Offer.ItemsToGive, mapIDsToItems(bp, changeIDs)...),
					ItemsToReceive: ctx.Offer.ItemsToReceive,
					Message:        "I've added the necessary change for you!",
				})
			} else if diff < 0 {
				// We were underpaid -> try to find their change
				partnerInv, err := invProvider.GetPartnerInventory(ctx, ctx.Offer.OtherSteamID)
				if err != nil {
					logger.Warn("Failed to fetch partner inventory for smart countering", log.Err(err))
					ctx.Review(reason.ReviewPartnerInventoryFetchFailed)
					return nil
				}

				keyPriceVar, _ := ctx.Get("key_price_scrap")
				keyPrice, _ := keyPriceVar.(currency.Scrap)

				needed := -diff

				toAdd, ok := FindPartnerCurrency(partnerInv, needed, keyPrice)
				if ok {
					logger.Info("Smart countering: found missing currency in partner inventory",
						log.Int("needed_scrap", int(needed)),
						log.Int("found_items", len(toAdd)),
					)

					ctx.Counter(reason.AcceptCorrectValue, &trading.CounterParams{
						ItemsToGive:    ctx.Offer.ItemsToGive,
						ItemsToReceive: append(ctx.Offer.ItemsToReceive, toAdd...),
						Message:        "You were missing some change, I've added it for you!",
					})
				} else {
					ctx.Decline(tf2reason.DeclineUnderpaid)
				}
			}

			return nil
		}
	}
}

// calculateValueDiff calculates the difference in value between what we receive and what we give.
// Result > 0: We were overpaid (need change).
// Result < 0: We were underpaid (we should reject or request more).
func calculateValueDiff(ctx *engine.TradeContext) (currency.Scrap, error) {
	pricesRaw, ok := ctx.Get("prices")
	if !ok {
		return 0, errors.New("prices not found in context")
	}

	priceMap := pricesRaw.(map[string]*pricedb.Price)

	var keyPriceScrap currency.Scrap
	if keyPrice, ok := priceMap[currency.SKUKey]; ok {
		keyPriceScrap = currency.ToScrap(keyPrice.Buy.Metal)
	}

	if keyPriceScrap <= 0 {
		ctx.Review(tf2reason.ReviewInvalidKeyPrice)
		return 0, errors.New("invalid or missing key price in pricelist")
	}

	ctx.Set("key_price_scrap", keyPriceScrap)

	var ourTotal, theirTotal currency.Scrap

	for _, item := range ctx.Offer.ItemsToGive {
		p, ok := priceMap[item.SKU]
		if !ok {
			ctx.Review(tf2reason.ReviewUnpricedItem)
			return 0, fmt.Errorf("unpriced item in 'give' side: %s", item.SKU)
		}

		val := currency.Scrap(p.Sell.Keys)*keyPriceScrap + currency.ToScrap(p.Sell.Metal)
		ourTotal += val
	}

	for _, item := range ctx.Offer.ItemsToReceive {
		p, ok := priceMap[item.SKU]
		if !ok {
			ctx.Review(tf2reason.ReviewUnpricedItem)
			return 0, fmt.Errorf("unpriced item in 'receive' side: %s", item.SKU)
		}

		val := currency.Scrap(p.Buy.Keys)*keyPriceScrap + currency.ToScrap(p.Buy.Metal)
		theirTotal += val
	}

	diff := currency.NewValueDiff(ourTotal, theirTotal, keyPriceScrap)

	ctx.Set("value_diff_scrap", diff.Diff())
	ctx.Set("is_profitable", diff.IsProfitable())

	return diff.Diff(), nil
}

// FindPartnerCurrency tries to find a combination of currency items in partner's inventory to cover the debt.
func FindPartnerCurrency(items []*trading.Item, needed, keyPrice currency.Scrap) ([]*trading.Item, bool) {
	var (
		keys      []*trading.Item
		refined   []*trading.Item
		reclaimed []*trading.Item
		scrap     []*trading.Item
	)

	for _, it := range items {
		switch it.MarketHashName {
		case "Mann Co. Supply Crate Key":
			keys = append(keys, it)
		case "Refined Metal":
			refined = append(refined, it)
		case "Reclaimed Metal":
			reclaimed = append(reclaimed, it)
		case "Scrap Metal":
			scrap = append(scrap, it)
		}
	}

	var result []*trading.Item

	remaining := needed

	// 1. Take keys if needed
	if keyPrice > 0 {
		for len(keys) > 0 && remaining >= keyPrice {
			result = append(result, keys[0])
			keys = keys[1:]
			remaining -= keyPrice
		}
	}

	// 2. Take refined
	for len(refined) > 0 && remaining >= currency.ScrapInRef {
		result = append(result, refined[0])
		refined = refined[1:]
		remaining -= currency.ScrapInRef
	}

	// 3. Take reclaimed
	for len(reclaimed) > 0 && remaining >= currency.ScrapInRec {
		result = append(result, reclaimed[0])
		reclaimed = reclaimed[1:]
		remaining -= currency.ScrapInRec
	}

	// 4. Take scrap
	for len(scrap) > 0 && remaining >= 1 {
		result = append(result, scrap[0])
		scrap = scrap[1:]
		remaining -= 1
	}

	return result, remaining == 0
}

func mapIDsToItems(bp *backpack.Backpack, ids []uint64) []*trading.Item {
	var items []*trading.Item
	for _, id := range ids {
		if it, ok := bp.GetItem(id); ok {
			items = append(items, it.ToEconItem())
		}
	}

	return items
}

func isUnusual(target string) bool {
	it, err := sku.FromString(target)
	if err != nil {
		return false
	}

	return it.Quality == 5
}

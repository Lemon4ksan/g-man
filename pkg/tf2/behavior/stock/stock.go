// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package stock provides stock control behavior for the TF2 trading bot.
package stock

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/lemon4ksan/g-man/pkg/behavior"
	"github.com/lemon4ksan/g-man/pkg/log"
	"github.com/lemon4ksan/g-man/pkg/tf2"
	"github.com/lemon4ksan/g-man/pkg/tf2/backpack"
	"github.com/lemon4ksan/g-man/pkg/tf2/bptf"
	"github.com/lemon4ksan/g-man/pkg/tf2/currency"
	"github.com/lemon4ksan/g-man/pkg/tf2/ecp"
	"github.com/lemon4ksan/g-man/pkg/tf2/pricedb"
	"github.com/lemon4ksan/g-man/pkg/tf2/sku"
	"github.com/lemon4ksan/g-man/pkg/tf2/trading"
)

// BehaviorName is the name of the stock control behavior.
const BehaviorName = "stock_control"

// Control returns an option that registers the stock control behavior with the orchestrator.
func Control(
	bp *backpack.Backpack,
	mgr *bptf.ListingManager,
	priceMgr *pricedb.Manager,
	cfgMgr *trading.ConfigManager,
) behavior.Option {
	return func(o *behavior.Orchestrator) {
		o.Register(New(bp, mgr, priceMgr, o.Logger(), cfgMgr))
	}
}

// Stock manages listings based on current stock levels and limits.
type Stock struct {
	bp         *backpack.Backpack
	listingMgr *bptf.ListingManager
	priceMgr   *pricedb.Manager
	logger     log.Logger
	cfgMgr     *trading.ConfigManager
	interval   time.Duration
	ecp        *ecp.EasyCopyPaste
}

// New creates a new stock management behavior.
func New(
	bp *backpack.Backpack,
	mgr *bptf.ListingManager,
	priceMgr *pricedb.Manager,
	logger log.Logger,
	cfgMgr *trading.ConfigManager,
) *Stock {
	e := ecp.New()
	e.SetUseBoldChars(true)
	e.SetUseWordSwap(true)

	return &Stock{
		bp:         bp,
		listingMgr: mgr,
		priceMgr:   priceMgr,
		logger:     logger.With(log.Module(BehaviorName)),
		cfgMgr:     cfgMgr,
		interval:   5 * time.Minute,
		ecp:        e,
	}
}

// Name returns the unique name of the behavior.
func (s *Stock) Name() string {
	return BehaviorName
}

// Run starts the automated stock balancing loop.
func (s *Stock) Run(ctx context.Context) error {
	s.logger.Info("Stock Control behavior started", log.Duration("interval", s.interval))

	// Start auto-watching the configuration file for changes
	s.cfgMgr.StartWatching(ctx, 5*time.Second, s.logger)

	sub := s.bp.Bus.Subscribe(&tf2.BackpackLoadedEvent{})
	defer sub.Unsubscribe()

	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()

	// Initial check
	if err := s.rebalance(ctx); err != nil {
		s.logger.Error("Initial rebalance failed", log.Err(err))
	}

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case ev := <-sub.C():
			if _, ok := ev.(*tf2.BackpackLoadedEvent); ok {
				s.seedPrices(ctx)
			}
		case <-ticker.C:
			if err := s.rebalance(ctx); err != nil {
				s.logger.Error("Rebalance failed", log.Err(err))
			}
		}
	}
}

func (s *Stock) seedPrices(ctx context.Context) {
	s.logger.Info("Seeding prices from backpack...")

	skus := make(map[string]struct{})
	for _, item := range s.bp.Cache().GetItems() {
		sku := item.GetSKU(s.bp.Schema().Get())
		if sku != "" {
			skus[sku] = struct{}{}
		}
	}

	skuList := make([]string, 0, len(skus))
	for sku := range skus {
		skuList = append(skuList, sku)
	}

	if err := s.priceMgr.SeedFromBackpack(ctx, skuList); err != nil {
		s.logger.Error("Failed to seed prices", log.Err(err))
	}
}

func (s *Stock) rebalance(ctx context.Context) error {
	s.logger.Debug("Rebalancing stock listings...")

	if err := s.listingMgr.Sync(ctx); err != nil {
		return fmt.Errorf("failed to sync listings: %w", err)
	}

	watchedSKUs := s.priceMgr.GetWatchedSKUs()

	for _, sku := range watchedSKUs {
		price, ok := s.priceMgr.GetPrice(sku)
		if !ok || price.Buy.IsZero() || price.Sell.IsZero() {
			continue
		}

		currentStock := s.bp.GetStock(sku)
		maxStock := s.getMaxStock(sku)

		enableBuy := true
		enableSell := true

		if s.cfgMgr != nil {
			if item, ok := s.cfgMgr.GetItemConfig(sku); ok {
				enableBuy = item.EnableBuy
				enableSell = item.EnableSell
			}
		}

		// Buy listings
		if enableBuy && currentStock < maxStock {
			if err := s.ensureListing(ctx, sku, price, "buy"); err != nil {
				s.logger.Error("Failed to update buy listing", log.String("sku", sku), log.Err(err))
			}
		} else {
			s.removeListing(ctx, sku, "buy")
		}

		// Sell listings
		if enableSell && currentStock > 0 {
			if err := s.ensureListing(ctx, sku, price, "sell"); err != nil {
				s.logger.Error("Failed to update sell listing", log.String("sku", sku), log.Err(err))
			}
		} else {
			s.removeListing(ctx, sku, "sell")
		}
	}

	return nil
}

func (s *Stock) toScrap(keys, metal, keyPriceRef float64) currency.Scrap {
	totalRef := keys*keyPriceRef + metal
	return currency.ToScrap(totalRef)
}

func (s *Stock) getUndercutPrice(
	ctx context.Context,
	sku string,
	intent string,
	targetPriceScrap currency.Scrap,
	keyPriceRef float64,
) (currency.Scrap, error) {
	if s.cfgMgr == nil {
		return targetPriceScrap, nil
	}

	itemCfg, ok := s.cfgMgr.GetItemConfig(sku)
	if !ok {
		return targetPriceScrap, nil
	}

	// Fetch active listings for this item from backpack.tf to analyze competitors.
	resp, err := s.listingMgr.Client().SearchClassifieds(ctx, sku, intent)
	if err != nil {
		s.logger.Warn(
			"Failed to search competitors on backpack.tf, using target price",
			log.String("sku", sku),
			log.Err(err),
		)

		return targetPriceScrap, nil
	}

	existing := s.listingMgr.FindListingBySKU(sku, intent)

	if intent == "sell" {
		return s.getSellUndercut(itemCfg, targetPriceScrap, keyPriceRef, resp.Listings, existing)
	} else {
		return s.getBuyUndercut(itemCfg, targetPriceScrap, keyPriceRef, resp.Listings, existing)
	}
}

func (s *Stock) getSellUndercut(
	itemCfg trading.ItemConfig,
	targetPriceScrap currency.Scrap,
	keyPriceRef float64,
	listings []bptf.ListingResponse,
	existing *bptf.ListingResponse,
) (currency.Scrap, error) {
	minSellScrap, _ := itemCfg.MinSellPrice.ToValue(keyPriceRef)
	maxSellScrap, _ := itemCfg.MaxSellPrice.ToValue(keyPriceRef)
	cfg := s.cfgMgr.GetConfig()

	var (
		ourListingID string
		ourSteamID   string
	)
	if existing != nil {
		ourListingID = existing.ID
		ourSteamID = existing.SteamID
	}

	// Find the lowest competitor price
	var minCompScrap currency.Scrap
	for _, l := range listings {
		// Skip our own listings
		if l.ID == ourListingID || (ourSteamID != "" && l.SteamID == ourSteamID) {
			continue
		}

		if s.shouldIgnoreListing(l, cfg) {
			continue
		}

		compKeys := l.Currencies["keys"]
		compMetal := l.Currencies["metal"]
		compScrap := s.toScrap(compKeys, compMetal, keyPriceRef)

		if compScrap <= 0 {
			continue
		}

		if minCompScrap == 0 || compScrap < minCompScrap {
			minCompScrap = compScrap
		}
	}

	// If no competitors are found, default to maxSellScrap or targetPriceScrap
	if minCompScrap == 0 {
		if maxSellScrap > 0 {
			return maxSellScrap, nil
		}

		return targetPriceScrap, nil
	}

	// Undercut the competitor's price by 1 scrap (0.11 ref)
	undercutPrice := minCompScrap - 1

	// Apply floor bounds (MinSellPrice)
	if minSellScrap > 0 && undercutPrice < minSellScrap {
		undercutPrice = minSellScrap
	}

	// Apply ceiling bounds (MaxSellPrice)
	if maxSellScrap > 0 && undercutPrice > maxSellScrap {
		undercutPrice = maxSellScrap
	}

	// Apply price swing limits (MaxSellDecrease)
	if existing != nil && cfg.PriceSwingLimits.MaxSellDecrease > 0 {
		prevPriceScrap := s.toScrap(existing.Currencies["keys"], existing.Currencies["metal"], keyPriceRef)
		if prevPriceScrap > 0 && undercutPrice < prevPriceScrap {
			maxDecreaseScrap := currency.Scrap(float64(prevPriceScrap) * cfg.PriceSwingLimits.MaxSellDecrease)

			minAllowedPrice := prevPriceScrap - maxDecreaseScrap
			if undercutPrice < minAllowedPrice {
				s.logger.Info("Sell price undercut exceeded swing limit, capping decrease",
					log.String("prev", currency.FormatRefined(prevPriceScrap)),
					log.String("calculated", currency.FormatRefined(undercutPrice)),
					log.String("capped", currency.FormatRefined(minAllowedPrice)),
				)
				undercutPrice = minAllowedPrice
			}
		}
	}

	return undercutPrice, nil
}

func (s *Stock) getBuyUndercut(
	itemCfg trading.ItemConfig,
	targetPriceScrap currency.Scrap,
	keyPriceRef float64,
	listings []bptf.ListingResponse,
	existing *bptf.ListingResponse,
) (currency.Scrap, error) {
	minBuyScrap, _ := itemCfg.MinBuyPrice.ToValue(keyPriceRef)
	maxBuyScrap, _ := itemCfg.MaxBuyPrice.ToValue(keyPriceRef)
	cfg := s.cfgMgr.GetConfig()

	var (
		ourListingID string
		ourSteamID   string
	)
	if existing != nil {
		ourListingID = existing.ID
		ourSteamID = existing.SteamID
	}

	// Find the highest competitor price
	var maxCompScrap currency.Scrap
	for _, l := range listings {
		// Skip our own listings
		if l.ID == ourListingID || (ourSteamID != "" && l.SteamID == ourSteamID) {
			continue
		}

		if s.shouldIgnoreListing(l, cfg) {
			continue
		}

		compKeys := l.Currencies["keys"]
		compMetal := l.Currencies["metal"]
		compScrap := s.toScrap(compKeys, compMetal, keyPriceRef)

		if compScrap <= 0 {
			continue
		}

		if maxCompScrap == 0 || compScrap > maxCompScrap {
			maxCompScrap = compScrap
		}
	}

	// If no competitors are found, default to minBuyScrap or targetPriceScrap
	if maxCompScrap == 0 {
		if minBuyScrap > 0 {
			return minBuyScrap, nil
		}

		return targetPriceScrap, nil
	}

	// Overbid the competitor's price by 1 scrap (0.11 ref)
	overbidPrice := maxCompScrap + 1

	// Apply ceiling bounds (MaxBuyPrice)
	if maxBuyScrap > 0 && overbidPrice > maxBuyScrap {
		overbidPrice = maxBuyScrap
	}

	// Apply floor bounds (MinBuyPrice)
	if minBuyScrap > 0 && overbidPrice < minBuyScrap {
		overbidPrice = minBuyScrap
	}

	// Apply price swing limits (MaxBuyIncrease)
	if existing != nil && cfg.PriceSwingLimits.MaxBuyIncrease > 0 {
		prevPriceScrap := s.toScrap(existing.Currencies["keys"], existing.Currencies["metal"], keyPriceRef)
		if prevPriceScrap > 0 && overbidPrice > prevPriceScrap {
			maxIncreaseScrap := currency.Scrap(float64(prevPriceScrap) * cfg.PriceSwingLimits.MaxBuyIncrease)

			maxAllowedPrice := prevPriceScrap + maxIncreaseScrap
			if overbidPrice > maxAllowedPrice {
				s.logger.Info("Buy price overbid exceeded swing limit, capping increase",
					log.String("prev", currency.FormatRefined(prevPriceScrap)),
					log.String("calculated", currency.FormatRefined(overbidPrice)),
					log.String("capped", currency.FormatRefined(maxAllowedPrice)),
				)
				overbidPrice = maxAllowedPrice
			}
		}
	}

	return overbidPrice, nil
}

func (s *Stock) shouldIgnoreListing(l bptf.ListingResponse, cfg trading.Config) bool {
	// Skip excluded SteamIDs
	for _, id := range cfg.ExcludedSteamIDs {
		if l.SteamID == id {
			return true
		}
	}

	// Skip if details contain any excluded description
	if l.Details != "" {
		detailsLower := strings.ToLower(l.Details)
		for _, desc := range cfg.ExcludedListingDescriptions {
			if desc != "" && strings.Contains(detailsLower, strings.ToLower(desc)) {
				return true
			}
		}
	}

	return false
}

func (s *Stock) ensureListing(ctx context.Context, sku string, price *pricedb.Price, intent string) error {
	existing := s.listingMgr.FindListingBySKU(sku, intent)

	// Fetch current key price in metal (ref)
	keyPriceRef := 0.0
	if kp, ok := s.priceMgr.GetPrice("5021;6"); ok {
		keyPriceRef = kp.Sell.ToMetal(0)
	}

	targetPrice := price.Buy
	if intent == "sell" {
		targetPrice = price.Sell
	}

	targetPriceScrap := s.toScrap(float64(targetPrice.Keys), targetPrice.Metal, keyPriceRef)

	// Compute optimal undercutting price
	finalPriceScrap, err := s.getUndercutPrice(ctx, sku, intent, targetPriceScrap, keyPriceRef)
	if err != nil {
		s.logger.Warn("Failed to compute undercut price, using target price", log.String("sku", sku), log.Err(err))

		finalPriceScrap = targetPriceScrap
	}

	finalCurrency := currency.ScrapToCurrencies(finalPriceScrap, keyPriceRef)

	if existing != nil {
		if s.isPriceSame(existing.Currencies, finalCurrency) {
			return nil
		}

		if err := s.listingMgr.Delete(ctx, existing.ID); err != nil {
			return err
		}
	}

	currencies := map[string]float64{
		"metal": finalCurrency.Metal,
	}
	if finalCurrency.Keys > 0 {
		currencies["keys"] = finalCurrency.Keys
	}

	details := s.getListingDetails(sku, intent, finalCurrency)

	_, err = s.listingMgr.Upsert(ctx, bptf.ListingResolvable{
		Item:       sku,
		Intent:     intent,
		Currencies: currencies,
		Details:    details,
	})

	return err
}

func (s *Stock) getListingDetails(sku, intent string, price *currency.Currency) string {
	template := "⚡ Instantly Accept 📈 Stock: {stock}/{max_stock}"
	if s.cfgMgr != nil {
		cfg := s.cfgMgr.GetConfig()
		if cfg.ListingCommentTemplate != "" {
			template = cfg.ListingCommentTemplate
		}
	}

	currentStock := s.bp.GetStock(sku)
	maxStock := s.getMaxStock(sku)

	amountTrade := currentStock
	if intent == "buy" {
		amountTrade = maxStock - currentStock
		if amountTrade < 0 {
			amountTrade = 0
		}
	}

	itemName := s.getItemName(sku)

	ecpCmd, err := s.ecp.ToEcpString(itemName, intent)
	if err != nil {
		s.logger.Warn("Failed to encode ECP string, falling back to plaintext", log.Err(err))

		if intent == "buy" {
			ecpCmd = "!sell " + itemName
		} else {
			ecpCmd = "!buy " + itemName
		}
	}

	details := template
	details = strings.ReplaceAll(details, "{sku}", sku)
	details = strings.ReplaceAll(details, "{price}", price.String())
	details = strings.ReplaceAll(details, "{intent}", intent)
	details = strings.ReplaceAll(details, "{stock}", strconv.Itoa(currentStock))
	details = strings.ReplaceAll(details, "{max_stock}", strconv.Itoa(maxStock))
	details = strings.ReplaceAll(details, "{amount_trade}", strconv.Itoa(amountTrade))
	details = strings.ReplaceAll(details, "{ecp_item}", ecpCmd)

	return details
}

func (s *Stock) getItemName(skuStr string) string {
	sch := s.bp.Schema().Get()
	if sch == nil {
		return skuStr
	}

	item, err := sku.FromString(skuStr)
	if err != nil {
		return skuStr
	}

	return sch.ItemName(item, true, false, false)
}

func (s *Stock) removeListing(ctx context.Context, sku, intent string) {
	if existing := s.listingMgr.FindListingBySKU(sku, intent); existing != nil {
		if err := s.listingMgr.Delete(ctx, existing.ID); err != nil {
			s.logger.Error("Failed to remove listing", log.String("sku", sku), log.Err(err))
		}
	}
}

func (s *Stock) isPriceSame(current map[string]float64, target *currency.Currency) bool {
	return current["keys"] == target.Keys && current["metal"] == target.Metal
}

func (s *Stock) getMaxStock(sku string) int {
	if s.cfgMgr != nil {
		if item, ok := s.cfgMgr.GetItemConfig(sku); ok {
			return item.MaxStock
		}

		return s.cfgMgr.GetConfig().DefaultMaxStock
	}

	return 5 // Safe fallback limit
}

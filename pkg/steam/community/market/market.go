// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package market

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"

	"github.com/PuerkitoBio/goquery"
	"github.com/lemon4ksan/aoni"
	"github.com/lemon4ksan/miyako/generic"

	"github.com/lemon4ksan/g-man/pkg/log"
	"github.com/lemon4ksan/g-man/pkg/steam"
	"github.com/lemon4ksan/g-man/pkg/steam/community"
	"github.com/lemon4ksan/g-man/pkg/steam/id"
	"github.com/lemon4ksan/g-man/pkg/steam/module"
)

var (
	rxBoosterCreator = regexp.MustCompile(`(?s)CBoosterCreatorPage\.Init\(\s*(.*?),\s*(\d+),\s*(\d+),\s*(\d+),\s*\[`)
	rxMarketApps     = regexp.MustCompile(`https?://steamcommunity.com/market/search\?appid=(\d+)`)
)

// ModuleName is the unique identifier for the market module.
const ModuleName string = "market"

// WithModule returns a steam.Option that registers the market module in the client.
func WithModule(cfg Config) steam.Option {
	return steam.WithModule(New(cfg))
}

// From returns the market module from the client.
func From(c *steam.Client) *Market {
	return steam.GetModule[*Market](c)
}

// Config contains settings for requests to the Trading Platform.
type Config struct {
	// Currency is the standard currency code used for pricing.
	Currency CurrencyCode
	// Country is the ISO two-letter country code (for example, "US").
	Country string
	// Language is the localization language name (for example, "english").
	Language string
}

// DefaultConfig returns the default settings (USD, US, english).
func DefaultConfig() Config {
	return Config{
		Currency: CurrencyCodeUSD,
		Country:  "US",
		Language: "english",
	}
}

// Market manages interactions with the Steam Community Market.
// Create new instances of Market using the [New] constructor.
type Market struct {
	module.Base

	config       Config
	marketClient community.Requester

	mu      sync.RWMutex
	steamID id.ID
}

// New creates a new Market module instance.
func New(cfg Config) *Market {
	return &Market{
		Base:   module.New(ModuleName),
		config: cfg,
	}
}

// StartAuthed is called when a community session is established.
func (m *Market) StartAuthed(ctx context.Context, auth module.AuthContext) error {
	m.mu.Lock()
	m.steamID = auth.SteamID()
	m.marketClient = community.Decorate(auth.Community(),
		aoni.WithHeader("X-Requested-With", "XMLHttpRequest"),
		aoni.WithHeader("X-Prototype-Version", "1.7"),
	)
	m.mu.Unlock()

	m.Logger.Info("Market module ready",
		log.Int("currency", int(m.config.Currency)),
		log.SteamID(m.steamID.Uint64()),
	)

	return nil
}

// CreateSellOrder places an item from the user's inventory onto the market.
// The price should be in the smallest currency unit (e.g., cents/kopecks)
// and represents the amount the seller receives.
func (m *Market) CreateSellOrder(ctx context.Context, opts CreateSellOrderOptions) (*CreateSellOrder, error) {
	client, myID, err := m.getAuthenticatedClient()
	if err != nil {
		return nil, err
	}

	req := struct {
		AppID     uint32 `url:"appid"`
		ContextID int64  `url:"contextid"`
		AssetID   uint64 `url:"assetid"`
		Amount    int    `url:"amount"`
		Price     int    `url:"price"`
	}{opts.AppID, opts.ContextID, opts.AssetID, opts.Amount, opts.Price}

	resp, err := community.PostForm[CreateSellOrderResponse](
		ctx, client, "market/sellitem", req,
		aoni.WithHeader("Referer", fmt.Sprintf("%sprofiles/%d/inventory?modal=1&market=1", community.BaseURL, myID)),
	)
	if err != nil {
		return nil, fmt.Errorf("market: sell order failed: %w", err)
	}

	return &CreateSellOrder{
		Success:                 resp.Success,
		RequiresConfirmation:    resp.RequiresConfirmation == 1,
		NeedsMobileConfirmation: resp.NeedsMobileConfirmation,
		NeedsEmailConfirmation:  resp.NeedsEmailConfirmation,
		EmailDomain:             resp.EmailDomain,
	}, nil
}

// CreateBuyOrder creates a buy order (buy order) for a specific item.
func (m *Market) CreateBuyOrder(ctx context.Context, opts CreateBuyOrderOptions) (*CreateBuyOrderResponse, error) {
	client, _, err := m.getAuthenticatedClient()
	if err != nil {
		return nil, err
	}

	totalCents := opts.Price * opts.Amount
	priceTotal := formatCurrencyDecimal(totalCents, m.config.Currency)

	req := struct {
		AppID          uint32       `url:"appid"`
		Currency       CurrencyCode `url:"currency"`
		MarketHashName string       `url:"market_hash_name"`
		PriceTotal     string       `url:"price_total"`
		Quantity       int          `url:"quantity"`
		BillingState   string       `url:"billing_state"`
		SaveMyAddress  string       `url:"save_my_address"`
	}{opts.AppID, m.config.Currency, opts.MarketHashName, priceTotal, opts.Amount, "", "0"}

	resp, err := community.PostForm[CreateBuyOrderResponse](
		ctx, client, "market/createbuyorder", req,
		aoni.WithHeader("Referer", fmt.Sprintf(
			community.BaseURL+"market/listings/%d/%s",
			opts.AppID, url.PathEscape(opts.MarketHashName),
		)),
	)
	if err != nil {
		return nil, fmt.Errorf("market: buy order failed: %w", err)
	}

	return resp, nil
}

// CancelBuyOrder cancels an existing active buy order.
func (m *Market) CancelBuyOrder(ctx context.Context, buyOrderID uint64) error {
	client, _, err := m.getAuthenticatedClient()
	if err != nil {
		return err
	}

	req := struct {
		BuyOrderID uint64 `url:"buy_orderid"`
	}{buyOrderID}

	type respType struct {
		Success bool `json:"success"`
	}

	resp, err := community.PostForm[respType](ctx, client, "market/cancelbuyorder", req)
	if err != nil {
		return err
	}

	if !resp.Success {
		return errors.New("market: cancel buy order request unsuccessful")
	}

	return nil
}

// CancelSellOrder removes an item from sale on the market.
func (m *Market) CancelSellOrder(ctx context.Context, listingID uint64) error {
	client, _, err := m.getAuthenticatedClient()
	if err != nil {
		return err
	}

	type respType struct {
		Success bool `json:"success"`
	}

	resp, err := community.PostForm[respType](
		ctx, client, "market/removelisting/{listingID}", nil,
		aoni.WithVar("listingID", listingID),
	)
	if err != nil {
		return err
	}

	if !resp.Success {
		return errors.New("market: cancel sell order request unsuccessful")
	}

	return nil
}

// Search searches for items on the marketplace.
func (m *Market) Search(ctx context.Context, appID uint32, opts SearchOptions) (*SearchResponse, error) {
	return community.GetJSON[SearchResponse](
		ctx, m.marketClient, "market/search/render",
		aoni.WithQuery(opts),
		aoni.WithHeader("Referer", fmt.Sprintf(community.BaseURL+"market/search?appid=%d", appID)),
	)
}

// GetPriceOverview gets a quick summary of the item's price.
func (m *Market) GetPriceOverview(
	ctx context.Context,
	appID uint32,
	marketHashName string,
) (*PriceOverviewResponse, error) {
	req := struct {
		AppID          uint32       `url:"appid"`
		Currency       CurrencyCode `url:"currency"`
		MarketHashName string       `url:"market_hash_name"`
	}{appID, m.config.Currency, marketHashName}

	return community.GetJSON[PriceOverviewResponse](
		ctx, m.marketClient, "market/priceoverview",
		aoni.WithQuery(req),
	)
}

// GetItemOrdersHistogram gets a histogram of active buy and sell orders.
func (m *Market) GetItemOrdersHistogram(
	ctx context.Context,
	appID uint32,
	marketHashName string,
	itemNameID uint64,
) (*ItemOrdersHistogram, error) {
	req := struct {
		Country    string       `url:"country"`
		Language   string       `url:"language"`
		Currency   CurrencyCode `url:"currency"`
		ItemNameID uint64       `url:"item_nameid"`
		TwoFactor  int          `url:"two_factor"`
	}{m.config.Country, m.config.Language, m.config.Currency, itemNameID, 0}

	resp, err := community.GetJSON[ItemOrdersHistogramResponse](
		ctx, m.marketClient, "market/itemordershistogram",
		aoni.WithQuery(req),
		aoni.WithHeader(
			"Referer",
			fmt.Sprintf(community.BaseURL+"market/listings/%d/%s", appID, url.PathEscape(marketHashName)),
		),
	)
	if err != nil {
		return nil, err
	}

	return buildItemOrdersHistogram(resp), nil
}

// GetMyListings gets the active lots and orders of an account.
func (m *Market) GetMyListings(ctx context.Context, start, count int) (*MyListingsResponse, error) {
	req := struct {
		Start    int `url:"start"`
		Count    int `url:"count" default:"100"`
		NoRender int `url:"norender"`
	}{start, count, 1}

	return community.GetJSON[MyListingsResponse](
		ctx, m.marketClient, "market/mylistings",
		aoni.WithQuery(req),
	)
}

// GetMarketApps retrieves all apps listed on the Steam Community Market.
func (m *Market) GetMarketApps(ctx context.Context) (map[uint32]string, error) {
	client, _, err := m.getAuthenticatedClient()
	if err != nil {
		return nil, err
	}

	html, err := community.GetHTML(ctx, client, "market")
	if err != nil {
		return nil, fmt.Errorf("market: failed to fetch market page: %w", err)
	}
	defer html.Close()

	doc, err := goquery.NewDocumentFromReader(html)
	if err != nil {
		return nil, fmt.Errorf("market: failed to parse HTML: %w", err)
	}

	apps := make(map[uint32]string)
	doc.Find(".market_search_game_button_group a.game_button").Each(func(_ int, buttonSel *goquery.Selection) {
		href, exists := buttonSel.Attr("href")
		if !exists {
			return
		}

		appID, ok := parseAppIDFromHref(href)
		if !ok {
			return
		}

		name := strings.TrimSpace(buttonSel.Find(".game_button_game_name").Text())
		apps[appID] = name
	})

	if len(apps) == 0 {
		return nil, errors.New("market: failed to parse any market apps")
	}

	return apps, nil
}

// GetGemValue checks if an item is eligible to be turned into gems and gets its gem value.
func (m *Market) GetGemValue(ctx context.Context, appID uint32, assetID uint64) (*GemValue, error) {
	client, _, err := m.getAuthenticatedClient()
	if err != nil {
		return nil, err
	}

	req := struct {
		AppID     uint32 `url:"appid"`
		ContextID int64  `url:"contextid"`
		AssetID   uint64 `url:"assetid"`
	}{appID, 6, assetID}

	resp, err := community.GetJSON[gemValueResponse](
		ctx, client, "ajaxgetgoovalue",
		aoni.WithQuery(req),
	)
	if err != nil {
		return nil, err
	}

	if resp.Success != 1 {
		return nil, fmt.Errorf("steam error: %s (success=%d)", resp.Message, resp.Success)
	}

	gemVal, _ := strconv.Atoi(resp.GooValue)

	return &GemValue{
		PromptTitle: resp.StrTitle,
		GemValue:    gemVal,
	}, nil
}

// TurnItemIntoGems converts an eligible item into gems.
func (m *Market) TurnItemIntoGems(
	ctx context.Context,
	appID uint32,
	assetID uint64,
	expectedGemsValue int,
) (*GemsResult, error) {
	client, _, err := m.getAuthenticatedClient()
	if err != nil {
		return nil, err
	}

	req := struct {
		AppID            uint32 `url:"appid"`
		ContextID        int64  `url:"contextid"`
		AssetID          uint64 `url:"assetid"`
		GooValueExpected int    `url:"goo_value_expected"`
	}{appID, 6, assetID, expectedGemsValue}

	resp, err := community.PostForm[grindGooResponse](ctx, client, "ajaxgrindintogoo", req)
	if err != nil {
		return nil, err
	}

	if resp.Success != 1 {
		return nil, fmt.Errorf("steam error: %s (success=%d)", resp.Message, resp.Success)
	}

	received, _ := strconv.Atoi(resp.GooValueReceived)
	total, _ := strconv.Atoi(resp.GooValueTotal)

	return &GemsResult{
		GemsReceived: received,
		TotalGems:    total,
	}, nil
}

// OpenBoosterPack unpacks a game booster pack into trading cards.
func (m *Market) OpenBoosterPack(ctx context.Context, appID uint32, assetID uint64) ([]any, error) {
	client, _, err := m.getAuthenticatedClient()
	if err != nil {
		return nil, err
	}

	req := struct {
		AppID         uint32 `url:"appid"`
		CommunityItem uint64 `url:"communityitemid"`
	}{appID, assetID}

	resp, err := community.PostForm[unpackBoosterResponse](ctx, client, "ajaxunpackbooster", req)
	if err != nil {
		return nil, err
	}

	if resp.Success != 1 {
		return nil, fmt.Errorf("steam error: %s (success=%d)", resp.Message, resp.Success)
	}

	return resp.RgItems, nil
}

// GetBoosterPackCatalog retrieves the user's gem count and booster pack creator list.
func (m *Market) GetBoosterPackCatalog(ctx context.Context) (*BoosterCatalog, error) {
	client, _, err := m.getAuthenticatedClient()
	if err != nil {
		return nil, err
	}

	html, err := community.GetHTML(ctx, client, "tradingcards/boostercreator")
	if err != nil {
		return nil, fmt.Errorf("market: failed to fetch booster creator page: %w", err)
	}
	defer html.Close()

	bodyBytes, err := io.ReadAll(html)
	if err != nil {
		return nil, err
	}

	return parseBoosterCatalog(bodyBytes)
}

// CreateBoosterPack crafts a booster pack using gems.
func (m *Market) CreateBoosterPack(ctx context.Context, appID uint32, useUntradableGems bool) (*BoosterResult, error) {
	client, _, err := m.getAuthenticatedClient()
	if err != nil {
		return nil, err
	}

	tradability := 2
	if useUntradableGems {
		tradability = 3
	}

	req := struct {
		AppID                 uint32 `url:"appid"`
		Series                int    `url:"series"`
		TradabilityPreference int    `url:"tradability_preference"`
	}{appID, 1, tradability}

	resp, err := community.PostForm[createBoosterResponse](ctx, client, "tradingcards/ajaxcreatebooster", req)
	if err != nil {
		return nil, err
	}

	if resp.PurchaseEResult != 1 {
		return nil, fmt.Errorf("steam purchase error: eresult=%d", resp.PurchaseEResult)
	}

	total, err := strconv.Atoi(resp.GooAmount)
	if err != nil {
		return nil, err
	}

	tradable, err := strconv.Atoi(resp.TradableGooAmount)
	if err != nil {
		return nil, err
	}

	untradable, err := strconv.Atoi(resp.UntradableGooAmount)
	if err != nil {
		return nil, err
	}

	return &BoosterResult{
		TotalGems:      total,
		TradableGems:   tradable,
		UntradableGems: untradable,
		ResultItem:     resp.PurchaseResult,
	}, nil
}

// GetGiftDetails gets information about a gift package in inventory.
func (m *Market) GetGiftDetails(ctx context.Context, giftID uint64) (*GiftDetails, error) {
	client, _, err := m.getAuthenticatedClient()
	if err != nil {
		return nil, err
	}

	resp, err := community.PostForm[giftDetailsResponse](
		ctx, client, "gifts/{giftID}/validateunpack", nil,
		aoni.WithVar("giftID", giftID),
	)
	if err != nil {
		return nil, err
	}

	if resp.Success != 1 {
		return nil, fmt.Errorf("steam error: %s (success=%d)", resp.Message, resp.Success)
	}

	pkgID, _ := strconv.Atoi(resp.PackageID)

	return &GiftDetails{
		GiftName:  resp.GiftName,
		PackageID: pkgID,
		Owned:     resp.Owned,
	}, nil
}

// RedeemGift unpacks a gift in inventory to the user's library.
func (m *Market) RedeemGift(ctx context.Context, giftID uint64) error {
	client, _, err := m.getAuthenticatedClient()
	if err != nil {
		return err
	}

	resp, err := community.PostForm[redeemGiftResponse](
		ctx, client, "gifts/{giftID}/unpack", nil,
		aoni.WithVar("giftID", giftID),
	)
	if err != nil {
		return err
	}

	if resp.Success != 1 {
		return fmt.Errorf("steam error: %s (success=%d)", resp.Message, resp.Success)
	}

	return nil
}

// GemExchange exchanges or packs/unpacks gems.
func (m *Market) GemExchange(ctx context.Context, assetID uint64, denomIn, denomOut, qtyIn, qtyOutExpected int) error {
	client, _, err := m.getAuthenticatedClient()
	if err != nil {
		return err
	}

	req := struct {
		AppID                uint32 `url:"appid"`
		AssetID              uint64 `url:"assetid"`
		GooDenominationIn    int    `url:"goo_denomination_in"`
		GooAmountIn          int    `url:"goo_amount_in"`
		GooDenominationOut   int    `url:"goo_denomination_out"`
		GooAmountOutExpected int    `url:"goo_amount_out_expected"`
	}{753, assetID, denomIn, qtyIn, denomOut, qtyOutExpected}

	resp, err := community.PostForm[gemExchangeResponse](ctx, client, "ajaxexchangegoo", req)
	if err != nil {
		return err
	}

	if resp.Success != 1 {
		return fmt.Errorf("steam error: %s (success=%d)", resp.Message, resp.Success)
	}

	return nil
}

// PackGemSacks packs raw gems into sacks of gems (1 sack = 1000 gems).
func (m *Market) PackGemSacks(ctx context.Context, assetID uint64, sackCount int) error {
	return m.GemExchange(ctx, assetID, 1, 1000, sackCount*1000, sackCount)
}

// UnpackGemSacks unpacks sacks of gems into raw gems (1 sack = 1000 gems).
func (m *Market) UnpackGemSacks(ctx context.Context, assetID uint64, sackCount int) error {
	return m.GemExchange(ctx, assetID, 1000, 1, sackCount, sackCount*1000)
}

func (m *Market) getAuthenticatedClient() (community.Requester, id.ID, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if m.marketClient == nil {
		return nil, 0, module.ErrNotAuthenticated
	}

	return m.marketClient, m.steamID, nil
}

func parseAppIDFromHref(href string) (uint32, bool) {
	match := rxMarketApps.FindStringSubmatch(href)
	if len(match) != 2 {
		return 0, false
	}

	appID, err := strconv.ParseUint(match[1], 10, 32)
	if err != nil {
		return 0, false
	}

	return uint32(appID), true
}

func buildItemOrdersHistogram(resp *ItemOrdersHistogramResponse) *ItemOrdersHistogram {
	histogram := &ItemOrdersHistogram{
		SellOrderTable:   resp.SellOrderTable,
		SellOrderSummary: resp.SellOrderSummary,
		BuyOrderTable:    resp.BuyOrderTable,
		BuyOrderSummary:  resp.BuyOrderSummary,
		BuyOrderGraph:    resp.BuyOrderGraph,
		SellOrderGraph:   resp.SellOrderGraph,
		GraphMaxY:        resp.GraphMaxY,
		GraphMinX:        resp.GraphMinX,
		GraphMaxX:        resp.GraphMaxX,
		PricePrefix:      resp.PricePrefix,
		PriceSuffix:      resp.PriceSuffix,
	}

	if resp.HighestBuyOrder != "" {
		histogram.HighestBuyOrder, _ = strconv.ParseFloat(resp.HighestBuyOrder, 64)
	}

	if resp.LowestSellOrder != "" {
		histogram.LowestSellOrder, _ = strconv.ParseFloat(resp.LowestSellOrder, 64)
	}

	return histogram
}

func parseBoosterCatalog(bodyBytes []byte) (*BoosterCatalog, error) {
	match := rxBoosterCreator.FindSubmatch(bodyBytes)
	if len(match) != 5 {
		return nil, errors.New("market: failed to parse booster creator catalog from JS")
	}

	var catalogList []*BoosterPackInfo
	if err := json.Unmarshal(match[1], &catalogList); err != nil {
		return nil, fmt.Errorf("market: failed to parse catalog JSON: %w", err)
	}

	totalGems, _ := strconv.Atoi(string(match[2]))
	tradableGems, _ := strconv.Atoi(string(match[3]))
	untradableGems, _ := strconv.Atoi(string(match[4]))

	catalogMap := generic.IndexBy(catalogList, func(app *BoosterPackInfo) uint32 {
		return app.AppID
	})

	return &BoosterCatalog{
		TotalGems:      totalGems,
		TradableGems:   tradableGems,
		UntradableGems: untradableGems,
		Catalog:        catalogMap,
	}, nil
}

func formatCurrencyDecimal(cents int, currency CurrencyCode) string {
	switch currency {
	case CurrencyCodeJPY, CurrencyCodeKRW, CurrencyCodeVND:
		return strconv.Itoa(cents)
	default:
		return fmt.Sprintf("%.2f", float64(cents)/100.0)
	}
}

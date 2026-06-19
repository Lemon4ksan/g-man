// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package market

import (
	"bytes"
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
	return func(c *steam.Client) {
		c.RegisterModule(New(cfg))
	}
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
//
// It handles buy and sell orders, cancellations, price history, and item crafting.
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

// Init initializes the module dependencies.
func (m *Market) Init(init module.InitContext) error {
	return m.Base.Init(init)
}

// StartAuthed is called when a community session is established.
// It captures the authenticated community requester and the user's SteamID.
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

// Close ensures the module is shut down correctly.
func (m *Market) Close() error {
	return m.Base.Close()
}

// CreateSellOrder places an item from the user's inventory onto the market.
// The price should be in the smallest currency unit (e.g., cents/kopecks)
// and represents the amount the seller receives.
//
// It returns an error if the request fails or is rejected by Steam Community.
func (m *Market) CreateSellOrder(ctx context.Context, opts CreateSellOrderOptions) (*CreateSellOrder, error) {
	m.mu.RLock()
	comm := m.marketClient
	myID := m.steamID
	m.mu.RUnlock()

	if comm == nil {
		return nil, module.ErrNotAuthenticated
	}

	req := struct {
		SessionID string `url:"sessionid"`
		AppID     uint32 `url:"appid"`
		ContextID int64  `url:"contextid"`
		AssetID   uint64 `url:"assetid"`
		Amount    int    `url:"amount"`
		Price     int    `url:"price"`
	}{comm.SessionID(community.BaseURL), opts.AppID, opts.ContextID, opts.AssetID, opts.Amount, opts.Price}

	resp, err := community.PostForm[CreateSellOrderResponse](ctx, comm, "market/sellitem", req,
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
//
// It returns an error if the request fails or is rejected by Steam Community.
func (m *Market) CreateBuyOrder(ctx context.Context, opts CreateBuyOrderOptions) (*CreateBuyOrderResponse, error) {
	m.mu.RLock()
	comm := m.marketClient
	m.mu.RUnlock()

	if comm == nil {
		return nil, module.ErrNotAuthenticated
	}

	// Format price to Steam's expected decimal string (e.g., "1.50")
	totalCents := opts.Price * opts.Amount

	req := struct {
		SessionID      string       `url:"sessionid"`
		AppID          uint32       `url:"appid"`
		Currency       CurrencyCode `url:"currency"`
		MarketHashName string       `url:"market_hash_name"`
		PriceTotal     string       `url:"price_total"`
		Quantity       int          `url:"quantity"`
		BillingState   string       `url:"billing_state"`
		SaveMyAddress  string       `url:"save_my_address"`
	}{
		SessionID:      comm.SessionID(community.BaseURL),
		AppID:          opts.AppID,
		Currency:       m.config.Currency,
		MarketHashName: opts.MarketHashName,
		PriceTotal:     formatCurrencyDecimal(totalCents, m.config.Currency),
		Quantity:       opts.Amount,
		BillingState:   "",
		SaveMyAddress:  "0",
	}

	resp, err := community.PostForm[CreateBuyOrderResponse](ctx, comm, "market/createbuyorder", req,
		aoni.WithHeader("Referer", fmt.Sprintf(
			"%smarket/listings/%d/%s",
			community.BaseURL,
			opts.AppID,
			url.PathEscape(opts.MarketHashName),
		)),
	)
	if err != nil {
		return nil, fmt.Errorf("market: buy order failed: %w", err)
	}

	return resp, nil
}

// CancelBuyOrder cancels an existing active buy order.
//
// It returns an error if the request fails or is rejected by Steam Community.
func (m *Market) CancelBuyOrder(ctx context.Context, buyOrderID uint64) error {
	m.mu.RLock()
	comm := m.marketClient
	m.mu.RUnlock()

	if comm == nil {
		return module.ErrNotAuthenticated
	}

	req := struct {
		SessionID  string `url:"sessionid"`
		BuyOrderID uint64 `url:"buy_orderid"`
	}{comm.SessionID(community.BaseURL), buyOrderID}

	_, err := community.PostForm[aoni.NoResponse](ctx, comm, "market/cancelbuyorder", req)

	return err
}

// CancelSellOrder removes an item from sale on the market.
//
// It returns an error if the request fails or is rejected by Steam Community.
func (m *Market) CancelSellOrder(ctx context.Context, listingID uint64) error {
	m.mu.RLock()
	comm := m.marketClient
	m.mu.RUnlock()

	if comm == nil {
		return module.ErrNotAuthenticated
	}

	req := struct {
		SessionID string `url:"sessionid"`
	}{comm.SessionID(community.BaseURL)}

	_, err := community.PostForm[aoni.NoResponse](ctx, comm, "market/removelisting/{listingID}", req,
		aoni.WithVar("listingID", listingID),
	)

	return err
}

// Search searches for items on the marketplace.
//
// It returns the search results or an error if the request fails.
func (m *Market) Search(ctx context.Context, appID uint32, opts SearchOptions) (*SearchResponse, error) {
	return community.Get[SearchResponse](ctx, m.marketClient, "market/search/render", opts,
		aoni.WithHeader("Referer", fmt.Sprintf("https://steamcommunity.com/market/search?appid=%d", appID)),
	)
}

// GetPriceOverview gets a quick summary of the item's price.
//
// It returns the price summary or an error if the request fails.
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

	return community.Get[PriceOverviewResponse](ctx, m.marketClient, "market/priceoverview", req)
}

// GetItemOrdersHistogram gets a histogram of active buy and sell orders.
// The itemNameID can be obtained by parsing the lot page (usually cached by the bot).
//
// It returns the histogram details or an error if the request fails.
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

	resp, err := community.Get[ItemOrdersHistogramResponse](
		ctx,
		m.marketClient,
		"market/itemordershistogram",
		req,
		aoni.WithHeader(
			"Referer",
			fmt.Sprintf("https://steamcommunity.com/market/listings/%d/%s", appID, url.PathEscape(marketHashName)),
		),
	)
	if err != nil {
		return nil, err
	}

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

	return histogram, nil
}

// GetMyListings gets the active lots and orders of an account.
//
// It returns the active listings or an error if the request fails.
func (m *Market) GetMyListings(ctx context.Context, start, count int) (*MyListingsResponse, error) {
	req := struct {
		Start    int `url:"start"`
		Count    int `url:"count" default:"100"`
		NoRender int `url:"norender"`
	}{start, count, 1}

	return community.Get[MyListingsResponse](ctx, m.marketClient, "market/mylistings", req)
}

func formatCurrencyDecimal(cents int, currency CurrencyCode) string {
	// Some currencies don't use decimals (JPY, KRW, VND)
	switch currency {
	case CurrencyCodeJPY, CurrencyCodeKRW, CurrencyCodeVND:
		return strconv.Itoa(cents)
	default:
		return fmt.Sprintf("%.2f", float64(cents)/100.0)
	}
}

// GetMarketApps retrieves all apps listed on the Steam Community Market.
//
// It returns the apps mapped by AppID or an error if parsing fails.
func (m *Market) GetMarketApps(ctx context.Context) (map[uint32]string, error) {
	m.mu.RLock()
	comm := m.marketClient
	m.mu.RUnlock()

	if comm == nil {
		return nil, module.ErrNotAuthenticated
	}

	html, err := community.GetHTML(ctx, comm, "market")
	if err != nil {
		return nil, fmt.Errorf("market: failed to fetch market page: %w", err)
	}
	defer html.Close()

	doc, err := goquery.NewDocumentFromReader(html)
	if err != nil {
		return nil, fmt.Errorf("market: failed to parse HTML: %w", err)
	}

	apps := make(map[uint32]string)
	doc.Find(".market_search_game_button_group a.game_button").Each(func(_ int, s *goquery.Selection) {
		name := strings.TrimSpace(s.Find(".game_button_game_name").Text())

		href, exists := s.Attr("href")
		if exists {
			match := rxMarketApps.FindStringSubmatch(href)
			if len(match) == 2 {
				appID, _ := strconv.ParseUint(match[1], 10, 32)
				apps[uint32(appID)] = name
			}
		}
	})

	if len(apps) == 0 {
		return nil, errors.New("market: failed to parse any market apps")
	}

	return apps, nil
}

// GetGemValue checks if an item is eligible to be turned into gems and gets its gem value.
//
// It returns the gem value details or an error if Steam rejects the query.
func (m *Market) GetGemValue(ctx context.Context, appID uint32, assetID uint64) (*GemValue, error) {
	m.mu.RLock()
	comm := m.marketClient
	m.mu.RUnlock()

	if comm == nil {
		return nil, module.ErrNotAuthenticated
	}

	req := url.Values{
		"sessionid": {comm.SessionID(community.BaseURL)},
		"appid":     {strconv.FormatUint(uint64(appID), 10)},
		"contextid": {"6"},
		"assetid":   {strconv.FormatUint(assetID, 10)},
	}

	type response struct {
		Success  int    `json:"success"`
		Message  string `json:"message"`
		GooValue string `json:"goo_value"`
		StrTitle string `json:"strTitle"`
	}

	resp, err := community.Get[response](ctx, comm, "ajaxgetgoovalue", req)
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
//
// It returns the gems received or an error if Steam rejects the transaction.
func (m *Market) TurnItemIntoGems(
	ctx context.Context,
	appID uint32,
	assetID uint64,
	expectedGemsValue int,
) (*GemsResult, error) {
	m.mu.RLock()
	comm := m.marketClient
	m.mu.RUnlock()

	if comm == nil {
		return nil, module.ErrNotAuthenticated
	}

	req := url.Values{
		"sessionid":          {comm.SessionID(community.BaseURL)},
		"appid":              {strconv.FormatUint(uint64(appID), 10)},
		"contextid":          {"6"},
		"assetid":            {strconv.FormatUint(assetID, 10)},
		"goo_value_expected": {strconv.Itoa(expectedGemsValue)},
	}

	type response struct {
		Success          int    `json:"success"`
		Message          string `json:"message"`
		GooValueReceived string `json:"goo_value_received "` // Yes, there is a space in the JSON key (valve lol)
		GooValueTotal    string `json:"goo_value_total"`
	}

	resp, err := community.PostForm[response](ctx, comm, "ajaxgrindintogoo", req)
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
//
// It returns the unpacked items details or an error if the request is rejected.
func (m *Market) OpenBoosterPack(ctx context.Context, appID uint32, assetID uint64) ([]any, error) {
	m.mu.RLock()
	comm := m.marketClient
	m.mu.RUnlock()

	if comm == nil {
		return nil, module.ErrNotAuthenticated
	}

	req := url.Values{
		"sessionid":       {comm.SessionID(community.BaseURL)},
		"appid":           {strconv.FormatUint(uint64(appID), 10)},
		"communityitemid": {strconv.FormatUint(assetID, 10)},
	}

	type response struct {
		Success int    `json:"success"`
		Message string `json:"message"`
		RgItems []any  `json:"rgItems"`
	}

	resp, err := community.PostForm[response](ctx, comm, "ajaxunpackbooster", req)
	if err != nil {
		return nil, err
	}

	if resp.Success != 1 {
		return nil, fmt.Errorf("steam error: %s (success=%d)", resp.Message, resp.Success)
	}

	return resp.RgItems, nil
}

// GetBoosterPackCatalog retrieves the user's gem count and booster pack creator list.
//
// It returns the catalog details or an error if parsing from JS config block fails.
func (m *Market) GetBoosterPackCatalog(ctx context.Context) (*BoosterCatalog, error) {
	m.mu.RLock()
	comm := m.marketClient
	m.mu.RUnlock()

	if comm == nil {
		return nil, module.ErrNotAuthenticated
	}

	html, err := community.GetHTML(ctx, comm, "tradingcards/boostercreator")
	if err != nil {
		return nil, fmt.Errorf("market: failed to fetch booster creator page: %w", err)
	}
	defer html.Close()

	var buf bytes.Buffer
	if _, err := io.Copy(&buf, html); err != nil {
		return nil, err
	}

	match := rxBoosterCreator.FindSubmatch(buf.Bytes())
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

// CreateBoosterPack crafts a booster pack using gems.
//
// It returns the resulting gem balances and item details, or an error if purchase fails.
func (m *Market) CreateBoosterPack(ctx context.Context, appID uint32, useUntradableGems bool) (*BoosterResult, error) {
	m.mu.RLock()
	comm := m.marketClient
	m.mu.RUnlock()

	if comm == nil {
		return nil, module.ErrNotAuthenticated
	}

	tradability := 2 // Prefer tradable gems only
	if useUntradableGems {
		tradability = 3 // Prefer untradable gems
	}

	req := struct {
		SessionID             string `url:"sessionid"`
		AppID                 uint32 `url:"appid"`
		Series                int    `url:"series"`
		TradabilityPreference int    `url:"tradability_preference"`
	}{comm.SessionID(community.BaseURL), appID, 1, tradability}

	type response struct {
		PurchaseEResult     int    `json:"purchase_eresult"`
		GooAmount           string `json:"goo_amount"`
		TradableGooAmount   string `json:"tradable_goo_amount"`
		UntradableGooAmount string `json:"untradable_goo_amount"`
		PurchaseResult      any    `json:"purchase_result"`
	}

	resp, err := community.PostForm[response](ctx, comm, "tradingcards/ajaxcreatebooster", req)
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
//
// It returns the gift details or an error if validation fails.
func (m *Market) GetGiftDetails(ctx context.Context, giftID uint64) (*GiftDetails, error) {
	m.mu.RLock()
	comm := m.marketClient
	m.mu.RUnlock()

	if comm == nil {
		return nil, module.ErrNotAuthenticated
	}

	req := struct {
		SessionID string `url:"sessionid"`
	}{comm.SessionID(community.BaseURL)}

	type response struct {
		Success   int    `json:"success"`
		Message   string `json:"message"`
		PackageID string `json:"packageid"`
		GiftName  string `json:"gift_name"`
		Owned     bool   `json:"owned"`
	}

	resp, err := community.PostForm[response](ctx, comm, "gifts/{giftID}/validateunpack", req,
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
//
// It returns an error if unpacking fails.
func (m *Market) RedeemGift(ctx context.Context, giftID uint64) error {
	m.mu.RLock()
	comm := m.marketClient
	m.mu.RUnlock()

	if comm == nil {
		return module.ErrNotAuthenticated
	}

	req := struct {
		SessionID string `url:"sessionid"`
	}{comm.SessionID(community.BaseURL)}

	type response struct {
		Success int    `json:"success"`
		Message string `json:"message"`
	}

	resp, err := community.PostForm[response](ctx, comm, "gifts/{giftID}/unpack", req,
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
//
// It returns an error if exchange fails.
func (m *Market) GemExchange(ctx context.Context, assetID uint64, denomIn, denomOut, qtyIn, qtyOutExpected int) error {
	m.mu.RLock()
	comm := m.marketClient
	m.mu.RUnlock()

	if comm == nil {
		return module.ErrNotAuthenticated
	}

	req := struct {
		AppID                uint32 `url:"appid"`
		AssetID              uint64 `url:"assetid"`
		GooDenominationIn    int    `url:"goo_denomination_in"`
		GooAmountIn          int    `url:"goo_amount_in"`
		GooDenominationOut   int    `url:"goo_denomination_out"`
		GooAmountOutExpected int    `url:"goo_amount_out_expected"`
		SessionID            string `url:"sessionid"`
	}{753, assetID, denomIn, qtyIn, denomOut, qtyOutExpected, comm.SessionID(community.BaseURL)}

	type response struct {
		Success int    `json:"success"`
		Message string `json:"message"`
	}

	resp, err := community.PostForm[response](ctx, comm, "ajaxexchangegoo", req)
	if err != nil {
		return err
	}

	if resp.Success != 1 {
		return fmt.Errorf("steam error: %s (success=%d)", resp.Message, resp.Success)
	}

	return nil
}

// PackGemSacks packs raw gems into sacks of gems (1 sack = 1000 gems).
//
// It returns an error if packing fails.
func (m *Market) PackGemSacks(ctx context.Context, assetID uint64, sackCount int) error {
	return m.GemExchange(ctx, assetID, 1, 1000, sackCount*1000, sackCount)
}

// UnpackGemSacks unpacks sacks of gems into raw gems (1 sack = 1000 gems).
//
// It returns an error if unpacking fails.
func (m *Market) UnpackGemSacks(ctx context.Context, assetID uint64, sackCount int) error {
	return m.GemExchange(ctx, assetID, 1000, 1, sackCount, sackCount*1000)
}

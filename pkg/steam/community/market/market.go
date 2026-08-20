// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package market implements interactions with the Steam Community Market, including buy/sell orders, item pricing, and gem crafting.
package market

import (
	"context"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strconv"
	"strings"
	"sync"

	json "github.com/goccy/go-json"
	"github.com/lemon4ksan/aoni/mod"
	"github.com/lemon4ksan/foundation/generic"
	"github.com/lemon4ksan/foundation/async/log"

	"github.com/lemon4ksan/g-man/pkg/steam/client"
	"github.com/lemon4ksan/g-man/pkg/steam/community"
	"github.com/lemon4ksan/g-man/pkg/steam/id"
	"github.com/lemon4ksan/g-man/pkg/steam/module"
)

var (
	rxBoosterCreator = regexp.MustCompile(`(?s)CBoosterCreatorPage\.Init\(\s*(.*?),\s*(\d+),\s*(\d+),\s*(\d+),\s*\[`)
	rxMarketApps     = regexp.MustCompile(`https?://steamcommunity.com/market/search\?appid=(\d+)`)
	rxGameAnchor     = regexp.MustCompile(`(?s)<a\s+([^>]*class="[^"]*game_button[^"]*"[^>]*)>(.*?)</a>`)
	rxHref           = regexp.MustCompile(`href="([^"]*)"`)
	rxGameName       = regexp.MustCompile(
		`(?s)<span[^>]*class="[^"]*game_button_game_name[^"]*"[^>]*>\s*(.*?)\s*</span>`,
	)
)

var (
	// ErrCancelBuyOrderFailed indicates the cancel buy order request was rejected by Steam.
	ErrCancelBuyOrderFailed = errors.New("market: cancel buy order request unsuccessful")
	// ErrCancelSellOrderFailed indicates the remove listing request was rejected by Steam.
	ErrCancelSellOrderFailed = errors.New("market: cancel sell order request unsuccessful")
	// ErrParseAppsFailed indicates no valid game apps could be parsed from the market HTML page.
	ErrParseAppsFailed = errors.New("market: failed to parse any market apps")
	// ErrBoosterCatalogJS indicates booster creator JS data could not be parsed from HTML.
	ErrBoosterCatalogJS = errors.New("market: failed to parse booster creator catalog from JS")
)

const ModuleName string = "market"

// WithModule registers the Market module in the client.
func WithModule(cfg Config) client.Option {
	return client.WithModule(New(cfg))
}

// From retrieves the Market module instance from the client.
func From(c *client.Client) *Market {
	return client.GetModule[*Market](c)
}

// Config configures default market request parameters.
type Config struct {
	Currency CurrencyCode
	Country  string
	Language string
}

// DefaultConfig builds default Market settings (USD, US, english).
func DefaultConfig() Config {
	return Config{
		Currency: CurrencyCodeUSD,
		Country:  "US",
		Language: "english",
	}
}

// Market manages market interactions, buy/sell orders, and inventory conversion.
//
// Thread Safety:
//   - Safe for concurrent use across goroutines.
type Market struct {
	module.Base

	mu     sync.RWMutex
	config Config
	client community.Requester
	api    API
}

// New constructs a Market module.
func New(cfg Config) *Market {
	return &Market{
		Base:   module.New(ModuleName),
		config: cfg,
	}
}

// NewWithClient constructs a Market module with an explicit community requester.
func NewWithClient(cfg Config, client community.Requester) *Market {
	var api API
	if client != nil {
		api = MustNewAPI(client)
	}

	return &Market{
		Base:   module.New(ModuleName),
		config: cfg,
		client: client,
		api:    api,
	}
}

// StartAuthed configures community client headers upon successful session authorization.
func (m *Market) StartAuthed(ctx context.Context, auth module.AuthContext) error {
	decorated := community.Decorate(auth.Community(),
		mod.WithHeader("X-Requested-With", "XMLHttpRequest"),
		mod.WithHeader("X-Prototype-Version", "1.7"),
	)
	api := MustNewAPI(decorated)

	m.mu.Lock()
	m.client = decorated
	m.api = api
	m.mu.Unlock()

	m.Logger.Info("Market module ready", log.Int("currency", int(m.config.Currency)))

	return nil
}

// CreateSellOrder places an inventory item on sale on the Community Market.
func (m *Market) CreateSellOrder(
	ctx context.Context,
	opts CreateSellOrderOptions,
	steamID id.ID,
) (*CreateSellOrder, error) {
	api, err := m.ensureAuthenticated()
	if err != nil {
		return nil, err
	}

	resp, err := api.SellItem(
		ctx, opts.AppID, opts.ContextID, opts.AssetID, opts.Amount, opts.Price, steamID,
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

// CreateBuyOrder creates an automated buy order.
func (m *Market) CreateBuyOrder(ctx context.Context, opts CreateBuyOrderOptions) (*CreateBuyOrderResponse, error) {
	api, err := m.ensureAuthenticated()
	if err != nil {
		return nil, err
	}

	totalCents := opts.Price * opts.Amount
	priceTotal := formatCurrencyDecimal(totalCents, m.config.Currency)

	resp, err := api.CreateBuyOrder(
		ctx, opts.AppID, m.config.Currency, opts.MarketHashName, priceTotal, opts.Amount, "", "0",
	)
	if err != nil {
		return nil, fmt.Errorf("market: buy order failed: %w", err)
	}

	return resp, nil
}

// CancelBuyOrder cancels an active buy order.
func (m *Market) CancelBuyOrder(ctx context.Context, buyOrderID uint64) error {
	api, err := m.ensureAuthenticated()
	if err != nil {
		return err
	}

	resp, err := api.CancelBuyOrder(ctx, buyOrderID)
	if err != nil {
		return err
	}

	if !resp.Success {
		return ErrCancelBuyOrderFailed
	}

	return nil
}

// CancelSellOrder removes a sell listing from the market.
func (m *Market) CancelSellOrder(ctx context.Context, listingID uint64) error {
	api, err := m.ensureAuthenticated()
	if err != nil {
		return err
	}

	resp, err := api.RemoveListing(ctx, listingID)
	if err != nil {
		return err
	}

	if !resp.Success {
		return ErrCancelSellOrderFailed
	}

	return nil
}

// Search executes a query against market item listings.
func (m *Market) Search(ctx context.Context, appID uint32, opts SearchOptions) (*SearchResponse, error) {
	api, err := m.ensureAuthenticated()
	if err != nil {
		return nil, err
	}

	return api.Search(ctx, appID, opts)
}

// GetPriceOverview fetches lowest price, median price, and 24h volume summaries for an item.
func (m *Market) GetPriceOverview(
	ctx context.Context,
	appID uint32,
	marketHashName string,
) (*PriceOverviewResponse, error) {
	api, err := m.ensureAuthenticated()
	if err != nil {
		return nil, err
	}

	return api.GetPriceOverview(ctx, appID, m.config.Currency, marketHashName)
}

// GetItemOrdersHistogram fetches market buy and sell order histograms for an item.
func (m *Market) GetItemOrdersHistogram(
	ctx context.Context,
	appID uint32,
	marketHashName string,
	itemNameID uint64,
) (*ItemOrdersHistogram, error) {
	api, err := m.ensureAuthenticated()
	if err != nil {
		return nil, err
	}

	resp, err := api.GetItemOrdersHistogram(
		ctx, appID, marketHashName, m.config.Country, m.config.Language, m.config.Currency, itemNameID, 0,
	)
	if err != nil {
		return nil, err
	}

	return &ItemOrdersHistogram{
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
		HighestBuyOrder:  float64(resp.HighestBuyOrder),
		LowestSellOrder:  float64(resp.LowestSellOrder),
	}, nil
}

// GetMyListings fetches active listings and buy orders for the authenticated user.
func (m *Market) GetMyListings(ctx context.Context, start, count int) (*MyListingsResponse, error) {
	api, err := m.ensureAuthenticated()
	if err != nil {
		return nil, err
	}

	return api.GetMyListings(ctx, start, count, 1)
}

// GetMarketApps parses game titles and AppIDs listed on the market navigation menu.
func (m *Market) GetMarketApps(ctx context.Context) (map[uint32]string, error) {
	api, err := m.ensureAuthenticated()
	if err != nil {
		return nil, err
	}

	html, err := api.GetMarketPage(ctx)
	if err != nil {
		return nil, fmt.Errorf("market: failed to fetch market page: %w", err)
	}

	defer html.Close()

	bodyBytes, err := io.ReadAll(html)
	if err != nil {
		return nil, err
	}

	apps := make(map[uint32]string)

	anchors := rxGameAnchor.FindAllSubmatch(bodyBytes, -1)
	for _, anchor := range anchors {
		attrs := anchor[1]
		inner := anchor[2]

		hrefMatch := rxHref.FindSubmatch(attrs)
		if len(hrefMatch) < 2 {
			continue
		}

		appID, ok := parseAppIDFromHref(string(hrefMatch[1]))
		if !ok {
			continue
		}

		nameMatch := rxGameName.FindSubmatch(inner)
		if len(nameMatch) < 2 {
			continue
		}

		apps[appID] = strings.TrimSpace(string(nameMatch[1]))
	}

	if len(apps) == 0 {
		return nil, ErrParseAppsFailed
	}

	return apps, nil
}

// GetGemValue checks if an item can be converted into gems and calculates its gem yield.
func (m *Market) GetGemValue(ctx context.Context, appID uint32, assetID uint64) (*GemValue, error) {
	api, err := m.ensureAuthenticated()
	if err != nil {
		return nil, err
	}

	resp, err := api.GetGooValue(ctx, appID, 6, assetID)
	if err != nil {
		return nil, err
	}

	if resp.Success != 1 {
		return nil, fmt.Errorf("steam error: %s (success=%d)", resp.Message, resp.Success)
	}

	return &GemValue{
		PromptTitle: resp.StrTitle,
		GemValue:    int64(resp.GooValue),
	}, nil
}

// TurnItemIntoGems converts an eligible inventory item into Steam gems.
func (m *Market) TurnItemIntoGems(
	ctx context.Context,
	appID uint32,
	assetID uint64,
	expectedGemsValue int,
) (*GemsResult, error) {
	api, err := m.ensureAuthenticated()
	if err != nil {
		return nil, err
	}

	resp, err := api.GrindIntoGoo(ctx, appID, 6, assetID, expectedGemsValue)
	if err != nil {
		return nil, err
	}

	if resp.Success != 1 {
		return nil, fmt.Errorf("steam error: %s (success=%d)", resp.Message, resp.Success)
	}

	return &GemsResult{
		GemsReceived: int64(resp.GooValueReceived),
		TotalGems:    int64(resp.GooValueTotal),
	}, nil
}

// OpenBoosterPack unpacks a trading card booster pack into cards.
func (m *Market) OpenBoosterPack(ctx context.Context, appID uint32, assetID uint64) ([]any, error) {
	api, err := m.ensureAuthenticated()
	if err != nil {
		return nil, err
	}

	resp, err := api.UnpackBooster(ctx, appID, assetID)
	if err != nil {
		return nil, err
	}

	if resp.Success != 1 {
		return nil, fmt.Errorf("steam error: %s (success=%d)", resp.Message, resp.Success)
	}

	return resp.RgItems, nil
}

// GetBoosterPackCatalog fetches the user's gem inventory balance and available booster pack creator options.
func (m *Market) GetBoosterPackCatalog(ctx context.Context) (*BoosterCatalog, error) {
	api, err := m.ensureAuthenticated()
	if err != nil {
		return nil, err
	}

	html, err := api.GetBoosterCreatorPage(ctx)
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

// CreateBoosterPack crafts a trading card booster pack using gems.
func (m *Market) CreateBoosterPack(ctx context.Context, appID uint32, useUntradableGems bool) (*BoosterResult, error) {
	api, err := m.ensureAuthenticated()
	if err != nil {
		return nil, err
	}

	resp, err := api.CreateBooster(ctx, appID, 1, generic.Ternary(useUntradableGems, 3, 2))
	if err != nil {
		return nil, err
	}

	if resp.PurchaseEResult != 1 {
		return nil, fmt.Errorf("steam purchase error: eresult=%d", resp.PurchaseEResult)
	}

	return &BoosterResult{
		TotalGems:      int64(resp.GooAmount),
		TradableGems:   int64(resp.TradableGooAmount),
		UntradableGems: int64(resp.UntradableGooAmount),
		ResultItem:     resp.PurchaseResult,
	}, nil
}

// GetGiftDetails inspects gift package contents in inventory.
func (m *Market) GetGiftDetails(ctx context.Context, giftID uint64) (*GiftDetails, error) {
	api, err := m.ensureAuthenticated()
	if err != nil {
		return nil, err
	}

	resp, err := api.ValidateUnpackGift(ctx, giftID)
	if err != nil {
		return nil, err
	}

	if resp.Success != 1 {
		return nil, fmt.Errorf("steam error: %s (success=%d)", resp.Message, resp.Success)
	}

	return &GiftDetails{
		GiftName:  resp.GiftName,
		PackageID: int64(resp.PackageID),
		Owned:     resp.Owned,
	}, nil
}

// RedeemGift unpacks an inventory gift directly to the account library.
func (m *Market) RedeemGift(ctx context.Context, giftID uint64) error {
	api, err := m.ensureAuthenticated()
	if err != nil {
		return err
	}

	resp, err := api.UnpackGift(ctx, giftID)
	if err != nil {
		return err
	}

	if resp.Success != 1 {
		return fmt.Errorf("steam error: %s (success=%d)", resp.Message, resp.Success)
	}

	return nil
}

// GemExchange packs or unpacks gem sacks.
func (m *Market) GemExchange(ctx context.Context, assetID uint64, denomIn, denomOut, qtyIn, qtyOutExpected int) error {
	api, err := m.ensureAuthenticated()
	if err != nil {
		return err
	}

	resp, err := api.ExchangeGoo(ctx, 753, assetID, denomIn, qtyIn, denomOut, qtyOutExpected)
	if err != nil {
		return err
	}

	if resp.Success != 1 {
		return fmt.Errorf("steam error: %s (success=%d)", resp.Message, resp.Success)
	}

	return nil
}

// PackGemSacks compresses 1000 raw gems into sacks of gems.
func (m *Market) PackGemSacks(ctx context.Context, assetID uint64, sackCount int) error {
	return m.GemExchange(ctx, assetID, 1, 1000, sackCount*1000, sackCount)
}

// UnpackGemSacks decompresses sacks of gems into 1000 raw gems per sack.
func (m *Market) UnpackGemSacks(ctx context.Context, assetID uint64, sackCount int) error {
	return m.GemExchange(ctx, assetID, 1000, 1, sackCount, sackCount*1000)
}

func (m *Market) ensureAuthenticated() (API, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if m.api == nil || m.client == nil {
		return nil, module.ErrNotAuthenticated
	}

	return m.api, nil
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

func parseBoosterCatalog(bodyBytes []byte) (*BoosterCatalog, error) {
	match := rxBoosterCreator.FindSubmatch(bodyBytes)
	if len(match) != 5 {
		return nil, ErrBoosterCatalogJS
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

// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package market_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/lemon4ksan/g-man/pkg/steam/community/market"
	"github.com/lemon4ksan/g-man/pkg/steam/id"
	module "github.com/lemon4ksan/g-man/test/mock"
)

func setupMarket(t *testing.T) (*market.Market, *module.InitContext, *module.AuthContext) {
	cfg := market.DefaultConfig()
	m := market.New(cfg)

	initCtx := module.NewInitContext()
	if err := m.Init(initCtx); err != nil {
		t.Fatalf("failed to init market: %v", err)
	}

	authCtx := module.NewAuthContext(id.ID(76561198000000001))
	if err := m.StartAuthed(t.Context(), authCtx); err != nil {
		t.Fatalf("failed to start authed: %v", err)
	}

	return m, initCtx, authCtx
}

func TestMarket_CreateSellOrder(t *testing.T) {
	m, _, auth := setupMarket(t)
	mockComm := auth.MockCommunity()

	mockComm.SetJSONResponse(
		"https://steamcommunity.com/market/sellitem",
		http.StatusOK,
		market.CreateSellOrderResponse{
			Success:              true,
			RequiresConfirmation: 1,
		},
	)

	opts := market.CreateSellOrderOptions{
		AppID:     440, // TF2
		ContextID: 2,
		AssetID:   123456789,
		Amount:    1,
		Price:     1050, // 10.50 units
	}

	resp, err := m.CreateSellOrder(t.Context(), opts)
	if err != nil {
		t.Fatalf("CreateSellOrder failed: %v", err)
	}

	if !resp.Success || !resp.RequiresConfirmation {
		t.Errorf("expected success and confirmation, got %+v", resp)
	}

	lastCall := mockComm.GetLastCall()
	if lastCall.Method != http.MethodPost {
		t.Errorf("expected POST, got %s", lastCall.Method)
	}

	if !strings.Contains(lastCall.Header.Get("Referer"), "inventory") {
		t.Error("Referer should point to inventory")
	}
}

func TestMarket_CreateBuyOrder_CurrencyFormatting(t *testing.T) {
	tests := []struct {
		name     string
		currency market.CurrencyCode
		price    int
		expected string
	}{
		{"USD formatting", market.CurrencyCodeUSD, 150, "1.50"},
		{"JPY formatting (no decimals)", market.CurrencyCodeJPY, 150, "150"},
		{"Large USD amount", market.CurrencyCodeUSD, 100000, "1000.00"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := market.New(market.Config{Currency: tt.currency})
			auth := module.NewAuthContext(id.ID(1))
			_ = m.StartAuthed(t.Context(), auth)

			mockComm := auth.MockCommunity()
			mockComm.SetJSONResponse(
				"https://steamcommunity.com/market/createbuyorder",
				http.StatusOK,
				market.CreateBuyOrderResponse{
					Success:    true,
					BuyOrderID: 999,
				},
			)

			_, err := m.CreateBuyOrder(t.Context(), market.CreateBuyOrderOptions{
				AppID:          440,
				MarketHashName: "Mann Co. Supply Crate Key",
				Price:          tt.price,
				Amount:         1,
			})
			if err != nil {
				t.Fatal(err)
			}

			lastCall := mockComm.GetLastCall()
			_ = lastCall.ParseForm()

			if got := lastCall.Form.Get("price_total"); got != tt.expected {
				t.Errorf("expected price_total %s, got %s", tt.expected, got)
			}
		})
	}
}

func TestMarket_GetPriceOverview(t *testing.T) {
	m, _, auth := setupMarket(t)
	mockComm := auth.MockCommunity()

	expectedURL := "https://steamcommunity.com/market/priceoverview?appid=440&currency=1&market_hash_name=Mann+Co.+Supply+Crate+Key"
	mockComm.SetJSONResponse(expectedURL, http.StatusOK, market.PriceOverviewResponse{
		Success:     true,
		LowestPrice: "$2.50",
		Volume:      "1,000",
		MedianPrice: "$2.45",
	})

	resp, err := m.GetPriceOverview(t.Context(), 440, "Mann Co. Supply Crate Key")
	if err != nil {
		t.Fatal(err)
	}

	if resp.LowestPrice != "$2.50" {
		t.Errorf("expected $2.50, got %s", resp.LowestPrice)
	}
}

func TestMarket_NotAuthenticated(t *testing.T) {
	m := market.New(market.DefaultConfig())

	_, err := m.CreateSellOrder(t.Context(), market.CreateSellOrderOptions{})
	if err == nil || !strings.Contains(err.Error(), "not authenticated") {
		t.Errorf("expected authentication error, got %v", err)
	}
}

func TestMarket_CancelSellOrder(t *testing.T) {
	m, _, auth := setupMarket(t)
	mockComm := auth.MockCommunity()

	listingID := uint64(888888)
	expectedPath := "https://steamcommunity.com/market/removelisting/888888"
	mockComm.SetJSONResponse(expectedPath, http.StatusOK, struct {
		Success bool `json:"success"`
	}{Success: true})

	err := m.CancelSellOrder(t.Context(), listingID)
	if err != nil {
		t.Fatal(err)
	}

	lastCall := mockComm.GetLastCall()
	if !strings.Contains(lastCall.URL.Path, "removelisting/888888") {
		t.Errorf("wrong path: %s", lastCall.URL.Path)
	}
}

func TestMarket_CancelBuyOrder(t *testing.T) {
	m, _, auth := setupMarket(t)
	mockComm := auth.MockCommunity()

	buyOrderID := uint64(123456789)

	mockComm.SetJSONResponse("https://steamcommunity.com/market/cancelbuyorder", http.StatusOK, struct {
		Success bool `json:"success"`
	}{Success: true})

	err := m.CancelBuyOrder(t.Context(), buyOrderID)
	if err != nil {
		t.Fatalf("CancelBuyOrder failed: %v", err)
	}

	lastCall := mockComm.GetLastCall()
	if lastCall.Method != http.MethodPost {
		t.Errorf("expected POST, got %s", lastCall.Method)
	}

	_ = lastCall.ParseForm()

	if got := lastCall.Form.Get("buy_orderid"); got != "123456789" {
		t.Errorf("expected buy_orderid 123456789, got %s", got)
	}

	if got := lastCall.Form.Get("sessionid"); got != auth.MockCommunity().MockSessionID {
		t.Errorf("expected correct sessionid, got %s", got)
	}
}

func TestMarket_GetItemOrdersHistogram(t *testing.T) {
	m, _, auth := setupMarket(t)
	mockComm := auth.MockCommunity()

	rawJSON := `{
		"success": 1, 
		"highest_buy_order": "150",
		"lowest_sell_order": "200",
		"buy_order_graph": [[1.50, 10, "10 orders at $1.50"], [1.40, 25, "25 orders at $1.40"]],
		"sell_order_graph": [[2.00, 5, "5 orders at $2.00"]],
		"price_prefix": "$",
		"graph_max_y": 100.5
	}`

	mockComm.SetHTMLResponse("market/itemordershistogram", http.StatusOK, rawJSON)

	res, err := m.GetItemOrdersHistogram(t.Context(), 440, "Mann Co. Supply Crate Key", 555)
	if err != nil {
		t.Fatalf("GetItemOrdersHistogram failed: %v", err)
	}

	if res.HighestBuyOrder != 150.0 || res.LowestSellOrder != 200.0 {
		t.Errorf("failed to parse prices, got Buy: %f, Sell: %f", res.HighestBuyOrder, res.LowestSellOrder)
	}

	if len(res.BuyOrderGraph) != 2 {
		t.Fatalf("expected 2 points in buy graph, got %d", len(res.BuyOrderGraph))
	}

	if res.BuyOrderGraph[0].Price != 1.50 || res.BuyOrderGraph[0].Volume != 10 {
		t.Errorf("invalid graph point data: %+v", res.BuyOrderGraph[0])
	}

	if res.GraphMaxY != 100.5 {
		t.Errorf("expected GraphMaxY 100.5, got %f", res.GraphMaxY)
	}

	lastCall := mockComm.GetLastCall()

	q := lastCall.URL.Query()
	if q.Get("item_nameid") != "555" {
		t.Errorf("expected item_nameid 555, got %s", q.Get("item_nameid"))
	}

	if q.Get("currency") != "1" { // CurrencyCodeUSD
		t.Errorf("expected currency 1, got %s", q.Get("currency"))
	}
}

func TestMarket_Search(t *testing.T) {
	m, _, auth := setupMarket(t)
	mockComm := auth.MockCommunity()

	mockComm.SetJSONResponse("market/search/render", http.StatusOK, market.SearchResponse{
		Success:    true,
		TotalCount: 500,
		Results: []market.SearchResultResponse{
			{Name: "Item 1", HashName: "item_1", SellPrice: 100},
		},
	})

	opts := market.SearchOptions{
		Query: "key",
		Count: 10,
		Start: 0,
	}

	res, err := m.Search(t.Context(), 440, opts)
	if err != nil {
		t.Fatal(err)
	}

	if res.TotalCount != 500 || len(res.Results) == 0 {
		t.Errorf("invalid search results: %+v", res)
	}

	lastCall := mockComm.GetLastCall()

	q := lastCall.URL.Query()
	if q.Get("query") != "key" {
		t.Errorf("expected query 'key', got %s", q.Get("query"))
	}

	if q.Get("count") != "10" {
		t.Errorf("expected count 10, got %s", q.Get("count"))
	}
}

func TestMarket_GetMyListings(t *testing.T) {
	m, _, auth := setupMarket(t)
	mockComm := auth.MockCommunity()

	mockComm.SetJSONResponse("market/mylistings", http.StatusOK, market.MyListingsResponse{
		Success:           true,
		TotalCount:        5,
		NumActiveListings: 2,
		BuyOrders: []market.BuyOrderResponse{
			{BuyOrderID: "111", HashName: "Key", Price: "250"},
		},
	})

	res, err := m.GetMyListings(t.Context(), 0, 50)
	if err != nil {
		t.Fatal(err)
	}

	if res.NumActiveListings != 2 || len(res.BuyOrders) != 1 {
		t.Errorf("invalid listings response: %+v", res)
	}

	lastCall := mockComm.GetLastCall()

	q := lastCall.URL.Query()
	if q.Get("start") != "0" || q.Get("count") != "50" {
		t.Errorf("invalid pagination params: start=%s, count=%s", q.Get("start"), q.Get("count"))
	}
}

func TestMarket_CommunityUpdates(t *testing.T) {
	m, _, auth := setupMarket(t)
	mockComm := auth.MockCommunity()

	t.Run("GetMarketApps Success", func(t *testing.T) {
		html := `
			<div class="market_search_game_button_group">
				<a class="game_button" href="https://steamcommunity.com/market/search?appid=730">
					<span class="game_button_game_name">Counter-Strike 2</span>
				</a>
			</div>
		`
		mockComm.SetHTMLResponse("market", http.StatusOK, html)

		apps, err := m.GetMarketApps(t.Context())
		if err != nil {
			t.Fatal(err)
		}

		if apps[730] != "Counter-Strike 2" {
			t.Errorf("expected CS2 app, got %v", apps)
		}
	})

	t.Run("GetGemValue Success", func(t *testing.T) {
		mockComm.SetJSONResponse("ajaxgetgoovalue", http.StatusOK, struct {
			Success  int    `json:"success"`
			GooValue string `json:"goo_value"`
			StrTitle string `json:"strTitle"`
		}{Success: 1, GooValue: "100", StrTitle: "Gems info"})

		res, err := m.GetGemValue(t.Context(), 730, 1111)
		if err != nil {
			t.Fatal(err)
		}

		if res.GemValue != 100 || res.PromptTitle != "Gems info" {
			t.Errorf("unexpected gem value details: %+v", res)
		}
	})

	t.Run("TurnItemIntoGems Success", func(t *testing.T) {
		mockComm.SetJSONResponse("ajaxgrindintogoo", http.StatusOK, struct {
			Success          int    `json:"success"`
			GooValueReceived string `json:"goo_value_received "`
			GooValueTotal    string `json:"goo_value_total"`
		}{Success: 1, GooValueReceived: "100", GooValueTotal: "1000"})

		res, err := m.TurnItemIntoGems(t.Context(), 730, 1111, 100)
		if err != nil {
			t.Fatal(err)
		}

		if res.GemsReceived != 100 || res.TotalGems != 1000 {
			t.Errorf("unexpected gems result: %+v", res)
		}
	})

	t.Run("OpenBoosterPack Success", func(t *testing.T) {
		mockComm.SetJSONResponse("ajaxunpackbooster", http.StatusOK, struct {
			Success int   `json:"success"`
			RgItems []any `json:"rgItems"`
		}{Success: 1, RgItems: []any{"card1", "card2"}})

		res, err := m.OpenBoosterPack(t.Context(), 730, 2222)
		if err != nil {
			t.Fatal(err)
		}

		if len(res) != 2 {
			t.Errorf("expected 2 items, got %d", len(res))
		}
	})

	t.Run("GetBoosterPackCatalog Success", func(t *testing.T) {
		catalogHTML := `
			CBoosterCreatorPage.Init(
				[{"appid": 730, "name": "CS2", "price": 100, "unavailable": false}],
				1000,
				800,
				200,
				[
		`
		mockComm.SetHTMLResponse("tradingcards/boostercreator", http.StatusOK, catalogHTML)

		catalog, err := m.GetBoosterPackCatalog(t.Context())
		if err != nil {
			t.Fatal(err)
		}

		if catalog.TotalGems != 1000 || catalog.Catalog[730].Price != 100 {
			t.Errorf("unexpected catalog details: %+v", catalog)
		}
	})

	t.Run("CreateBoosterPack Success", func(t *testing.T) {
		mockComm.SetJSONResponse("tradingcards/ajaxcreatebooster", http.StatusOK, struct {
			PurchaseEResult     int    `json:"purchase_eresult"`
			GooAmount           string `json:"goo_amount"`
			TradableGooAmount   string `json:"tradable_goo_amount"`
			UntradableGooAmount string `json:"untradable_goo_amount"`
			PurchaseResult      any    `json:"purchase_result"`
		}{PurchaseEResult: 1, GooAmount: "900", TradableGooAmount: "700", UntradableGooAmount: "200", PurchaseResult: "crafted_pack"})

		res, err := m.CreateBoosterPack(t.Context(), 730, true)
		if err != nil {
			t.Fatal(err)
		}

		if res.TotalGems != 900 || res.ResultItem != "crafted_pack" {
			t.Errorf("unexpected booster pack result: %+v", res)
		}
	})

	t.Run("GetGiftDetails Success", func(t *testing.T) {
		mockComm.SetJSONResponse("gifts/3333/validateunpack", http.StatusOK, struct {
			Success   int    `json:"success"`
			PackageID string `json:"packageid"`
			GiftName  string `json:"gift_name"`
			Owned     bool   `json:"owned"`
		}{Success: 1, PackageID: "4444", GiftName: "CS2 Gift", Owned: true})

		res, err := m.GetGiftDetails(t.Context(), 3333)
		if err != nil {
			t.Fatal(err)
		}

		if res.PackageID != 4444 || res.GiftName != "CS2 Gift" || !res.Owned {
			t.Errorf("unexpected gift details: %+v", res)
		}
	})

	t.Run("RedeemGift Success", func(t *testing.T) {
		mockComm.SetJSONResponse("gifts/3333/unpack", http.StatusOK, struct {
			Success int `json:"success"`
		}{Success: 1})

		err := m.RedeemGift(t.Context(), 3333)
		if err != nil {
			t.Fatal(err)
		}
	})

	t.Run("PackGemSacks Success", func(t *testing.T) {
		mockComm.SetJSONResponse("ajaxexchangegoo", http.StatusOK, struct {
			Success int `json:"success"`
		}{Success: 1})

		err := m.PackGemSacks(t.Context(), 5555, 3)
		if err != nil {
			t.Fatal(err)
		}
	})
}

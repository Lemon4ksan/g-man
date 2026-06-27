// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package market_test

import (
	"errors"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lemon4ksan/g-man/pkg/steam/community/market"
	"github.com/lemon4ksan/g-man/pkg/steam/id"
	module "github.com/lemon4ksan/g-man/test/mock"
)

const testProfileID = 76561198000000001

func setupMarket(t *testing.T) (*market.Market, *module.InitContext, *module.AuthContext) {
	t.Helper()

	cfg := market.DefaultConfig()
	m := market.New(cfg)

	initCtx := module.NewInitContext()
	if err := m.Init(initCtx); err != nil {
		t.Fatalf("failed to init market: %v", err)
	}

	authCtx := module.NewAuthContext(id.ID(testProfileID))
	if err := m.StartAuthed(t.Context(), authCtx); err != nil {
		t.Fatalf("failed to start authed: %v", err)
	}

	return m, initCtx, authCtx
}

func TestMarket_NotAuthenticated(t *testing.T) {
	t.Parallel()

	m := market.New(market.DefaultConfig())

	_, err := m.CreateSellOrder(t.Context(), market.CreateSellOrderOptions{})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not authenticated")
}

func TestMarket_CreateSellOrder(t *testing.T) {
	t.Parallel()

	t.Run("success", func(t *testing.T) {
		t.Parallel()
		m, _, auth := setupMarket(t)
		mockComm := auth.MockCommunity

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
		require.NoError(t, err)
		assert.True(t, resp.Success)
		assert.True(t, resp.RequiresConfirmation)

		lastCall := mockComm.GetLastCall()
		assert.Equal(t, http.MethodPost, lastCall.Method)
		assert.Contains(t, lastCall.Header.Get("Referer"), "inventory")
	})

	t.Run("failure", func(t *testing.T) {
		t.Parallel()
		m, _, auth := setupMarket(t)
		mockComm := auth.MockCommunity
		mockComm.ResponseErrs["market/sellitem"] = errors.New("post fail")

		_, err := m.CreateSellOrder(t.Context(), market.CreateSellOrderOptions{})
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "post fail")
	})
}

func TestMarket_CreateBuyOrder(t *testing.T) {
	t.Parallel()

	t.Run("currency_formatting", func(t *testing.T) {
		t.Parallel()

		tests := []struct {
			name     string
			currency market.CurrencyCode
			price    int
			expected string
		}{
			{"USD formatting", market.CurrencyCodeUSD, 150, "1.50"},
			{"JPY formatting (no decimals)", market.CurrencyCodeJPY, 150, "150"},
			{"KRW formatting (no decimals)", market.CurrencyCodeKRW, 150, "150"},
			{"VND formatting (no decimals)", market.CurrencyCodeVND, 150, "150"},
			{"Large USD amount", market.CurrencyCodeUSD, 100000, "1000.00"},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				t.Parallel()

				m := market.New(market.Config{Currency: tt.currency})
				auth := module.NewAuthContext(id.ID(1))
				_ = m.StartAuthed(t.Context(), auth)

				mockComm := auth.MockCommunity
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
				require.NoError(t, err)

				lastCall := mockComm.GetLastCall()
				_ = lastCall.ParseForm()
				assert.Equal(t, tt.expected, lastCall.Form.Get("price_total"))
			})
		}
	})

	t.Run("failure", func(t *testing.T) {
		t.Parallel()
		m, _, auth := setupMarket(t)
		mockComm := auth.MockCommunity
		mockComm.ResponseErrs["market/createbuyorder"] = errors.New("buy order fail")

		_, err := m.CreateBuyOrder(t.Context(), market.CreateBuyOrderOptions{})
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "buy order fail")
	})
}

func TestMarket_GetPriceOverview(t *testing.T) {
	t.Parallel()
	m, _, auth := setupMarket(t)
	mockComm := auth.MockCommunity

	expectedURL := "https://steamcommunity.com/market/priceoverview?appid=440&currency=1&market_hash_name=Mann+Co.+Supply+Crate+Key"
	mockComm.SetJSONResponse(expectedURL, http.StatusOK, market.PriceOverviewResponse{
		Success:     true,
		LowestPrice: "$2.50",
		Volume:      "1,000",
		MedianPrice: "$2.45",
	})

	resp, err := m.GetPriceOverview(t.Context(), 440, "Mann Co. Supply Crate Key")
	require.NoError(t, err)
	assert.Equal(t, "$2.50", resp.LowestPrice)
}

func TestMarket_CancelSellOrder(t *testing.T) {
	t.Parallel()

	t.Run("success", func(t *testing.T) {
		t.Parallel()
		m, _, auth := setupMarket(t)
		mockComm := auth.MockCommunity

		listingID := uint64(888888)
		expectedPath := "https://steamcommunity.com/market/removelisting/888888"
		mockComm.SetJSONResponse(expectedPath, http.StatusOK, struct {
			Success bool `json:"success"`
		}{Success: true})

		err := m.CancelSellOrder(t.Context(), listingID)
		require.NoError(t, err)

		lastCall := mockComm.GetLastCall()
		assert.Contains(t, lastCall.URL.Path, "removelisting/888888")
	})

	t.Run("failure", func(t *testing.T) {
		t.Parallel()
		m, _, auth := setupMarket(t)
		mockComm := auth.MockCommunity
		mockComm.ResponseErrs["market/removelisting/123"] = errors.New("remove fail")

		err := m.CancelSellOrder(t.Context(), 123)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "remove fail")
	})

	t.Run("unsuccessful_state", func(t *testing.T) {
		t.Parallel()
		m, _, auth := setupMarket(t)
		mockComm := auth.MockCommunity
		mockComm.SetJSONResponse("https://steamcommunity.com/market/removelisting/123", http.StatusOK, struct {
			Success bool `json:"success"`
		}{Success: false})

		err := m.CancelSellOrder(t.Context(), 123)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "cancel sell order request unsuccessful")
	})
}

func TestMarket_CancelBuyOrder(t *testing.T) {
	t.Parallel()

	t.Run("success", func(t *testing.T) {
		t.Parallel()
		m, _, auth := setupMarket(t)
		mockComm := auth.MockCommunity

		buyOrderID := uint64(123456789)

		mockComm.SetJSONResponse("https://steamcommunity.com/market/cancelbuyorder", http.StatusOK, struct {
			Success bool `json:"success"`
		}{Success: true})

		err := m.CancelBuyOrder(t.Context(), buyOrderID)
		require.NoError(t, err)

		lastCall := mockComm.GetLastCall()
		assert.Equal(t, http.MethodPost, lastCall.Method)

		_ = lastCall.ParseForm()
		assert.Equal(t, "123456789", lastCall.Form.Get("buy_orderid"))
		assert.Equal(t, mockComm.MockSessionID, lastCall.Form.Get("sessionid"))
	})

	t.Run("failure", func(t *testing.T) {
		t.Parallel()
		m, _, auth := setupMarket(t)
		mockComm := auth.MockCommunity
		mockComm.ResponseErrs["market/cancelbuyorder"] = errors.New("cancel buy fail")

		err := m.CancelBuyOrder(t.Context(), 123)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "cancel buy fail")
	})

	t.Run("unsuccessful_state", func(t *testing.T) {
		t.Parallel()
		m, _, auth := setupMarket(t)
		mockComm := auth.MockCommunity
		mockComm.SetJSONResponse("https://steamcommunity.com/market/cancelbuyorder", http.StatusOK, struct {
			Success bool `json:"success"`
		}{Success: false})

		err := m.CancelBuyOrder(t.Context(), 123)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "cancel buy order request unsuccessful")
	})
}

func TestMarket_GetItemOrdersHistogram(t *testing.T) {
	t.Parallel()

	t.Run("success", func(t *testing.T) {
		t.Parallel()
		m, _, auth := setupMarket(t)
		mockComm := auth.MockCommunity

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
		require.NoError(t, err)

		assert.Equal(t, 150.0, res.HighestBuyOrder)
		assert.Equal(t, 200.0, res.LowestSellOrder)
		require.Len(t, res.BuyOrderGraph, 2)
		assert.Equal(t, 1.50, res.BuyOrderGraph[0].Price)
		assert.Equal(t, int64(10), res.BuyOrderGraph[0].Volume)
		assert.Equal(t, 100.5, res.GraphMaxY)

		lastCall := mockComm.GetLastCall()
		q := lastCall.URL.Query()
		assert.Equal(t, "555", q.Get("item_nameid"))
		assert.Equal(t, "1", q.Get("currency"))
	})

	t.Run("failure", func(t *testing.T) {
		t.Parallel()
		m, _, auth := setupMarket(t)
		mockComm := auth.MockCommunity
		mockComm.ResponseErrs["market/itemordershistogram"] = errors.New("histogram query fail")

		_, err := m.GetItemOrdersHistogram(t.Context(), 440, "item", 111)
		assert.Error(t, err)
	})

	t.Run("empty_orders_fallback", func(t *testing.T) {
		t.Parallel()
		m, _, auth := setupMarket(t)
		mockComm := auth.MockCommunity
		rawJSON := `{
			"success": 1,
			"highest_buy_order": "",
			"lowest_sell_order": "",
			"buy_order_graph": [],
			"sell_order_graph": []
		}`
		mockComm.SetHTMLResponse("market/itemordershistogram", http.StatusOK, rawJSON)

		res, err := m.GetItemOrdersHistogram(t.Context(), 440, "item", 111)
		require.NoError(t, err)
		assert.Equal(t, 0.0, res.HighestBuyOrder)
		assert.Equal(t, 0.0, res.LowestSellOrder)
	})
}

func TestMarket_Search(t *testing.T) {
	t.Parallel()
	m, _, auth := setupMarket(t)
	mockComm := auth.MockCommunity

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
	require.NoError(t, err)

	assert.Equal(t, 500, res.TotalCount)
	require.NotEmpty(t, res.Results)

	lastCall := mockComm.GetLastCall()
	q := lastCall.URL.Query()
	assert.Equal(t, "key", q.Get("query"))
	assert.Equal(t, "10", q.Get("count"))
}

func TestMarket_GetMyListings(t *testing.T) {
	t.Parallel()
	m, _, auth := setupMarket(t)
	mockComm := auth.MockCommunity

	mockComm.SetJSONResponse("market/mylistings", http.StatusOK, market.MyListingsResponse{
		Success:           true,
		TotalCount:        5,
		NumActiveListings: 2,
		BuyOrders: []market.BuyOrderResponse{
			{BuyOrderID: "111", HashName: "Key", Price: "250"},
		},
	})

	res, err := m.GetMyListings(t.Context(), 0, 50)
	require.NoError(t, err)

	assert.Equal(t, 2, res.NumActiveListings)
	require.Len(t, res.BuyOrders, 1)

	lastCall := mockComm.GetLastCall()
	q := lastCall.URL.Query()
	assert.Equal(t, "0", q.Get("start"))
	assert.Equal(t, "50", q.Get("count"))
}

func TestMarket_GetMarketApps(t *testing.T) {
	t.Parallel()

	t.Run("success", func(t *testing.T) {
		t.Parallel()
		m, _, auth := setupMarket(t)
		mockComm := auth.MockCommunity

		html := `
			<div class="market_search_game_button_group">
				<a class="game_button" href="https://steamcommunity.com/market/search?appid=730">
					<span class="game_button_game_name">Counter-Strike 2</span>
				</a>
			</div>
		`
		mockComm.SetHTMLResponse("market", http.StatusOK, html)

		apps, err := m.GetMarketApps(t.Context())
		require.NoError(t, err)
		assert.Equal(t, "Counter-Strike 2", apps[730])
	})

	t.Run("edge_cases", func(t *testing.T) {
		t.Parallel()
		m, _, auth := setupMarket(t)
		mockComm := auth.MockCommunity

		html := `
			<div class="market_search_game_button_group">
				<a class="game_button">
					<span class="game_button_game_name">Missing Href Game</span>
				</a>
				<a class="game_button" href="https://steamcommunity.com/market/search">
					<span class="game_button_game_name">Missing AppID Query</span>
				</a>
				<a class="game_button" href="https://steamcommunity.com/market/search?appid=not_number">
					<span class="game_button_game_name">Non-numeric appID</span>
				</a>
				<a class="game_button" href="https://steamcommunity.com/market/search?appid=730">
					<span class="game_button_game_name">CS2</span>
				</a>
			</div>
		`
		mockComm.SetHTMLResponse("market", http.StatusOK, html)

		apps, err := m.GetMarketApps(t.Context())
		require.NoError(t, err)
		assert.Len(t, apps, 1)
		assert.Equal(t, "CS2", apps[730])
	})

	t.Run("empty_apps_parse_error", func(t *testing.T) {
		t.Parallel()
		m, _, auth := setupMarket(t)
		mockComm := auth.MockCommunity
		mockComm.SetHTMLResponse("market", http.StatusOK, `<div>No games container</div>`)

		_, err := m.GetMarketApps(t.Context())
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to parse any market apps")
	})

	t.Run("html_retrieval_failure", func(t *testing.T) {
		t.Parallel()
		m, _, auth := setupMarket(t)
		mockComm := auth.MockCommunity
		mockComm.ResponseErrs["market"] = errors.New("retrieval failed")

		_, err := m.GetMarketApps(t.Context())
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to fetch market page")
	})
}

func TestMarket_GetGemValue(t *testing.T) {
	t.Parallel()

	t.Run("success", func(t *testing.T) {
		t.Parallel()
		m, _, auth := setupMarket(t)
		mockComm := auth.MockCommunity

		mockComm.SetJSONResponse("ajaxgetgoovalue", http.StatusOK, struct {
			Success  int    `json:"success"`
			GooValue string `json:"goo_value"`
			StrTitle string `json:"strTitle"`
		}{Success: 1, GooValue: "100", StrTitle: "Gems info"})

		res, err := m.GetGemValue(t.Context(), 730, 1111)
		require.NoError(t, err)
		assert.Equal(t, 100, res.GemValue)
		assert.Equal(t, "Gems info", res.PromptTitle)
	})

	t.Run("steam_error", func(t *testing.T) {
		t.Parallel()
		m, _, auth := setupMarket(t)
		mockComm := auth.MockCommunity
		mockComm.SetJSONResponse("ajaxgetgoovalue", http.StatusOK, struct {
			Success int    `json:"success"`
			Message string `json:"message"`
		}{Success: 0, Message: "gem_valuation_error"})

		_, err := m.GetGemValue(t.Context(), 730, 1111)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "steam error: gem_valuation_error (success=0)")
	})

	t.Run("request_fail", func(t *testing.T) {
		t.Parallel()
		m, _, auth := setupMarket(t)
		mockComm := auth.MockCommunity
		mockComm.ResponseErrs["ajaxgetgoovalue"] = errors.New("req fail")

		_, err := m.GetGemValue(t.Context(), 730, 1111)
		assert.Error(t, err)
	})
}

func TestMarket_TurnItemIntoGems(t *testing.T) {
	t.Parallel()

	t.Run("success", func(t *testing.T) {
		t.Parallel()
		m, _, auth := setupMarket(t)
		mockComm := auth.MockCommunity

		mockComm.SetJSONResponse("ajaxgrindintogoo", http.StatusOK, struct {
			Success          int    `json:"success"`
			GooValueReceived string `json:"goo_value_received "`
			GooValueTotal    string `json:"goo_value_total"`
		}{Success: 1, GooValueReceived: "100", GooValueTotal: "1000"})

		res, err := m.TurnItemIntoGems(t.Context(), 730, 1111, 100)
		require.NoError(t, err)
		assert.Equal(t, 100, res.GemsReceived)
		assert.Equal(t, 1000, res.TotalGems)
	})

	t.Run("steam_error", func(t *testing.T) {
		t.Parallel()
		m, _, auth := setupMarket(t)
		mockComm := auth.MockCommunity
		mockComm.SetJSONResponse("ajaxgrindintogoo", http.StatusOK, struct {
			Success int    `json:"success"`
			Message string `json:"message"`
		}{Success: 0, Message: "failed_to_grind"})

		_, err := m.TurnItemIntoGems(t.Context(), 730, 1111, 100)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "steam error: failed_to_grind (success=0)")
	})

	t.Run("request_fail", func(t *testing.T) {
		t.Parallel()
		m, _, auth := setupMarket(t)
		mockComm := auth.MockCommunity
		mockComm.ResponseErrs["ajaxgrindintogoo"] = errors.New("req fail")

		_, err := m.TurnItemIntoGems(t.Context(), 730, 1111, 100)
		assert.Error(t, err)
	})
}

func TestMarket_OpenBoosterPack(t *testing.T) {
	t.Parallel()

	t.Run("success", func(t *testing.T) {
		t.Parallel()
		m, _, auth := setupMarket(t)
		mockComm := auth.MockCommunity

		mockComm.SetJSONResponse("ajaxunpackbooster", http.StatusOK, struct {
			Success int   `json:"success"`
			RgItems []any `json:"rgItems"`
		}{Success: 1, RgItems: []any{"card1", "card2"}})

		res, err := m.OpenBoosterPack(t.Context(), 730, 2222)
		require.NoError(t, err)
		assert.Len(t, res, 2)
	})

	t.Run("steam_error", func(t *testing.T) {
		t.Parallel()
		m, _, auth := setupMarket(t)
		mockComm := auth.MockCommunity
		mockComm.SetJSONResponse("ajaxunpackbooster", http.StatusOK, struct {
			Success int    `json:"success"`
			Message string `json:"message"`
		}{Success: 0, Message: "failed_unpack"})

		_, err := m.OpenBoosterPack(t.Context(), 730, 1111)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "steam error: failed_unpack (success=0)")
	})

	t.Run("request_fail", func(t *testing.T) {
		t.Parallel()
		m, _, auth := setupMarket(t)
		mockComm := auth.MockCommunity
		mockComm.ResponseErrs["ajaxunpackbooster"] = errors.New("req fail")

		_, err := m.OpenBoosterPack(t.Context(), 730, 1111)
		assert.Error(t, err)
	})
}

func TestMarket_GetBoosterPackCatalog(t *testing.T) {
	t.Parallel()

	t.Run("success", func(t *testing.T) {
		t.Parallel()
		m, _, auth := setupMarket(t)
		mockComm := auth.MockCommunity

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
		require.NoError(t, err)
		assert.Equal(t, 1000, catalog.TotalGems)
		assert.Equal(t, 100, catalog.Catalog[730].Price)
	})

	t.Run("fetch_html_error", func(t *testing.T) {
		t.Parallel()
		m, _, auth := setupMarket(t)
		mockComm := auth.MockCommunity
		mockComm.ResponseErrs["tradingcards/boostercreator"] = errors.New("fetch fail")

		_, err := m.GetBoosterPackCatalog(t.Context())
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to fetch booster creator page")
	})

	t.Run("regex_mismatch", func(t *testing.T) {
		t.Parallel()
		m, _, auth := setupMarket(t)
		mockComm := auth.MockCommunity
		mockComm.SetHTMLResponse("tradingcards/boostercreator", http.StatusOK, "CBoosterCreatorPage.Init(broken_array)")

		_, err := m.GetBoosterPackCatalog(t.Context())
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to parse booster creator catalog from JS")
	})

	t.Run("json_parse_fail", func(t *testing.T) {
		t.Parallel()
		m, _, auth := setupMarket(t)
		mockComm := auth.MockCommunity
		catalogHTML := `
			CBoosterCreatorPage.Init(
				{invalid_json},
				1000,
				800,
				200,
				[
		`
		mockComm.SetHTMLResponse("tradingcards/boostercreator", http.StatusOK, catalogHTML)

		_, err := m.GetBoosterPackCatalog(t.Context())
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to parse catalog JSON")
	})
}

func TestMarket_CreateBoosterPack(t *testing.T) {
	t.Parallel()

	t.Run("success", func(t *testing.T) {
		t.Parallel()
		m, _, auth := setupMarket(t)
		mockComm := auth.MockCommunity

		mockComm.SetJSONResponse("tradingcards/ajaxcreatebooster", http.StatusOK, struct {
			PurchaseEResult     int    `json:"purchase_eresult"`
			GooAmount           string `json:"goo_amount"`
			TradableGooAmount   string `json:"tradable_goo_amount"`
			UntradableGooAmount string `json:"untradable_goo_amount"`
			PurchaseResult      any    `json:"purchase_result"`
		}{PurchaseEResult: 1, GooAmount: "900", TradableGooAmount: "700", UntradableGooAmount: "200", PurchaseResult: "crafted_pack"})

		res, err := m.CreateBoosterPack(t.Context(), 730, true)
		require.NoError(t, err)
		assert.Equal(t, 900, res.TotalGems)
		assert.Equal(t, "crafted_pack", res.ResultItem)
	})

	t.Run("use_tradable_gems_preference", func(t *testing.T) {
		t.Parallel()
		m, _, auth := setupMarket(t)
		mockComm := auth.MockCommunity
		mockComm.SetJSONResponse("tradingcards/ajaxcreatebooster", http.StatusOK, struct {
			PurchaseEResult     int    `json:"purchase_eresult"`
			GooAmount           string `json:"goo_amount"`
			TradableGooAmount   string `json:"tradable_goo_amount"`
			UntradableGooAmount string `json:"untradable_goo_amount"`
			PurchaseResult      any    `json:"purchase_result"`
		}{PurchaseEResult: 1, GooAmount: "1000", TradableGooAmount: "1000", UntradableGooAmount: "0", PurchaseResult: "some_item"})

		res, err := m.CreateBoosterPack(t.Context(), 730, false)
		require.NoError(t, err)
		assert.Equal(t, 1000, res.TotalGems)

		lastCall := mockComm.GetLastCall()
		_ = lastCall.ParseForm()
		assert.Equal(t, "2", lastCall.Form.Get("tradability_preference"))
	})

	t.Run("purchase_eresult_error", func(t *testing.T) {
		t.Parallel()
		m, _, auth := setupMarket(t)
		mockComm := auth.MockCommunity
		mockComm.SetJSONResponse("tradingcards/ajaxcreatebooster", http.StatusOK, struct {
			PurchaseEResult int `json:"purchase_eresult"`
		}{PurchaseEResult: 2})

		_, err := m.CreateBoosterPack(t.Context(), 730, false)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "steam purchase error: eresult=2")
	})

	t.Run("conversion_failures", func(t *testing.T) {
		t.Parallel()

		t.Run("invalid_goo_amount", func(t *testing.T) {
			t.Parallel()
			m, _, auth := setupMarket(t)
			mockComm := auth.MockCommunity
			mockComm.SetJSONResponse("tradingcards/ajaxcreatebooster", http.StatusOK, struct {
				PurchaseEResult     int    `json:"purchase_eresult"`
				GooAmount           string `json:"goo_amount"`
				TradableGooAmount   string `json:"tradable_goo_amount"`
				UntradableGooAmount string `json:"untradable_goo_amount"`
			}{PurchaseEResult: 1, GooAmount: "bad_int"})

			_, err := m.CreateBoosterPack(t.Context(), 730, false)
			assert.Error(t, err)
		})

		t.Run("invalid_tradable_goo_amount", func(t *testing.T) {
			t.Parallel()
			m, _, auth := setupMarket(t)
			mockComm := auth.MockCommunity
			mockComm.SetJSONResponse("tradingcards/ajaxcreatebooster", http.StatusOK, struct {
				PurchaseEResult     int    `json:"purchase_eresult"`
				GooAmount           string `json:"goo_amount"`
				TradableGooAmount   string `json:"tradable_goo_amount"`
				UntradableGooAmount string `json:"untradable_goo_amount"`
			}{PurchaseEResult: 1, GooAmount: "100", TradableGooAmount: "bad_int"})

			_, err := m.CreateBoosterPack(t.Context(), 730, false)
			assert.Error(t, err)
		})

		t.Run("invalid_untradable_goo_amount", func(t *testing.T) {
			t.Parallel()
			m, _, auth := setupMarket(t)
			mockComm := auth.MockCommunity
			mockComm.SetJSONResponse("tradingcards/ajaxcreatebooster", http.StatusOK, struct {
				PurchaseEResult     int    `json:"purchase_eresult"`
				GooAmount           string `json:"goo_amount"`
				TradableGooAmount   string `json:"tradable_goo_amount"`
				UntradableGooAmount string `json:"untradable_goo_amount"`
			}{PurchaseEResult: 1, GooAmount: "100", TradableGooAmount: "100", UntradableGooAmount: "bad_int"})

			_, err := m.CreateBoosterPack(t.Context(), 730, false)
			assert.Error(t, err)
		})
	})

	t.Run("request_failure", func(t *testing.T) {
		t.Parallel()
		m, _, auth := setupMarket(t)
		mockComm := auth.MockCommunity
		mockComm.ResponseErrs["tradingcards/ajaxcreatebooster"] = errors.New("post fail")

		_, err := m.CreateBoosterPack(t.Context(), 730, false)
		assert.Error(t, err)
	})
}

func TestMarket_GetGiftDetails(t *testing.T) {
	t.Parallel()

	t.Run("success", func(t *testing.T) {
		t.Parallel()
		m, _, auth := setupMarket(t)
		mockComm := auth.MockCommunity

		mockComm.SetJSONResponse("gifts/3333/validateunpack", http.StatusOK, struct {
			Success   int    `json:"success"`
			PackageID string `json:"packageid"`
			GiftName  string `json:"gift_name"`
			Owned     bool   `json:"owned"`
		}{Success: 1, PackageID: "4444", GiftName: "CS2 Gift", Owned: true})

		res, err := m.GetGiftDetails(t.Context(), 3333)
		require.NoError(t, err)
		assert.Equal(t, 4444, res.PackageID)
		assert.Equal(t, "CS2 Gift", res.GiftName)
		assert.True(t, res.Owned)
	})

	t.Run("steam_failure", func(t *testing.T) {
		t.Parallel()
		m, _, auth := setupMarket(t)
		mockComm := auth.MockCommunity
		mockComm.SetJSONResponse("gifts/3333/validateunpack", http.StatusOK, struct {
			Success int    `json:"success"`
			Message string `json:"message"`
		}{Success: 0, Message: "validate_gift_failed"})

		_, err := m.GetGiftDetails(t.Context(), 3333)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "steam error: validate_gift_failed (success=0)")
	})

	t.Run("request_failure", func(t *testing.T) {
		t.Parallel()
		m, _, auth := setupMarket(t)
		mockComm := auth.MockCommunity
		mockComm.ResponseErrs["gifts/3333/validateunpack"] = errors.New("req fail")

		_, err := m.GetGiftDetails(t.Context(), 3333)
		assert.Error(t, err)
	})
}

func TestMarket_RedeemGift(t *testing.T) {
	t.Parallel()

	t.Run("success", func(t *testing.T) {
		t.Parallel()
		m, _, auth := setupMarket(t)
		mockComm := auth.MockCommunity

		mockComm.SetJSONResponse("gifts/3333/unpack", http.StatusOK, struct {
			Success int `json:"success"`
		}{Success: 1})

		err := m.RedeemGift(t.Context(), 3333)
		require.NoError(t, err)
	})

	t.Run("steam_failure", func(t *testing.T) {
		t.Parallel()
		m, _, auth := setupMarket(t)
		mockComm := auth.MockCommunity
		mockComm.SetJSONResponse("gifts/3333/unpack", http.StatusOK, struct {
			Success int    `json:"success"`
			Message string `json:"message"`
		}{Success: 0, Message: "redeem_gift_failed"})

		err := m.RedeemGift(t.Context(), 3333)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "steam error: redeem_gift_failed (success=0)")
	})

	t.Run("request_failure", func(t *testing.T) {
		t.Parallel()
		m, _, auth := setupMarket(t)
		mockComm := auth.MockCommunity
		mockComm.ResponseErrs["gifts/3333/unpack"] = errors.New("req fail")

		err := m.RedeemGift(t.Context(), 3333)
		assert.Error(t, err)
	})
}

func TestMarket_PackGemSacks(t *testing.T) {
	t.Parallel()

	t.Run("success", func(t *testing.T) {
		t.Parallel()
		m, _, auth := setupMarket(t)
		mockComm := auth.MockCommunity

		mockComm.SetJSONResponse("ajaxexchangegoo", http.StatusOK, struct {
			Success int `json:"success"`
		}{Success: 1})

		err := m.PackGemSacks(t.Context(), 5555, 3)
		require.NoError(t, err)
	})

	t.Run("steam_failure", func(t *testing.T) {
		t.Parallel()
		m, _, auth := setupMarket(t)
		mockComm := auth.MockCommunity
		mockComm.SetJSONResponse("ajaxexchangegoo", http.StatusOK, struct {
			Success int    `json:"success"`
			Message string `json:"message"`
		}{Success: 0, Message: "exchange_fail"})

		err := m.PackGemSacks(t.Context(), 5555, 3)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "steam error: exchange_fail (success=0)")
	})

	t.Run("request_failure", func(t *testing.T) {
		t.Parallel()
		m, _, auth := setupMarket(t)
		mockComm := auth.MockCommunity
		mockComm.ResponseErrs["ajaxexchangegoo"] = errors.New("req fail")

		err := m.PackGemSacks(t.Context(), 5555, 3)
		assert.Error(t, err)
	})
}

func TestMarket_UnpackGemSacks(t *testing.T) {
	t.Parallel()

	t.Run("success", func(t *testing.T) {
		t.Parallel()
		m, _, auth := setupMarket(t)
		mockComm := auth.MockCommunity

		mockComm.SetJSONResponse("ajaxexchangegoo", http.StatusOK, struct {
			Success int `json:"success"`
		}{Success: 1})

		err := m.UnpackGemSacks(t.Context(), 5555, 3)
		require.NoError(t, err)
	})
}

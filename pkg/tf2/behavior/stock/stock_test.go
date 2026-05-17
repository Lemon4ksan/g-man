// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package stock

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lemon4ksan/g-man/pkg/log"
	"github.com/lemon4ksan/g-man/pkg/tf2/bptf"
	"github.com/lemon4ksan/g-man/pkg/tf2/currency"
	"github.com/lemon4ksan/g-man/pkg/tf2/trading"
)

type redirectTripper struct {
	targetURL string
}

func (rt *redirectTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	target, _ := url.Parse(rt.targetURL)
	req.URL.Scheme = target.Scheme
	req.URL.Host = target.Host

	return http.DefaultClient.Do(req)
}

func TestStock_Undercutting(t *testing.T) {
	// 1. Create a temporary config file
	cfgFile := "test_trading_config.json"
	defer os.Remove(cfgFile)

	initialConfig := trading.Config{
		GlobalMaxStock:  3000,
		DefaultMaxStock: 5,
		Items: map[string]trading.ItemConfig{
			"5021;6": {
				SKU:          "5021;6",
				Name:         "Mann Co. Supply Crate Key",
				MaxStock:     100,
				EnableBuy:    true,
				EnableSell:   true,
				MinBuyPrice:  currency.Currency{Keys: 0, Metal: 50.0},
				MaxBuyPrice:  currency.Currency{Keys: 0, Metal: 60.0},
				MinSellPrice: currency.Currency{Keys: 0, Metal: 55.0},
				MaxSellPrice: currency.Currency{Keys: 0, Metal: 65.0},
			},
		},
	}
	data, err := json.Marshal(initialConfig)
	require.NoError(t, err)
	err = os.WriteFile(cfgFile, data, 0o644)
	require.NoError(t, err)

	cfgMgr, err := trading.NewConfigManager(cfgFile)
	require.NoError(t, err)

	// Variables to dynamically control mock competitor responses
	var (
		competitorSellPrice = 57.0
		competitorBuyPrice  = 53.0
	)

	// 2. Set up a mock http server to simulate backpack.tf classified search api response
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/v2/classifieds/search", r.URL.Path)
		sku := r.URL.Query().Get("sku")
		intent := r.URL.Query().Get("intent")

		assert.Equal(t, "5021;6", sku)

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)

		var results []bptf.ListingResponse
		if intent == "sell" {
			results = []bptf.ListingResponse{
				{
					ID:      "comp1",
					SteamID: "competitor_steam_id_1",
					Intent:  "sell",
					Currencies: map[string]float64{
						"metal": competitorSellPrice,
					},
				},
			}
		} else {
			results = []bptf.ListingResponse{
				{
					ID:      "comp3",
					SteamID: "competitor_steam_id_3",
					Intent:  "buy",
					Currencies: map[string]float64{
						"metal": competitorBuyPrice,
					},
				},
			}
		}

		resp := bptf.ListingsResponse{
			Results: results,
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	// 3. Initialize dependencies with the custom redirect client
	redirectClient := &http.Client{
		Transport: &redirectTripper{targetURL: server.URL},
	}

	bptfClient := bptf.New(redirectClient, "api-key", "token")
	listingMgr := bptf.NewListingManager(bptfClient, nil, log.New(log.DefaultConfig(log.ErrorLevel)))

	// 4. Construct behavior
	s := &Stock{
		listingMgr: listingMgr,
		cfgMgr:     cfgMgr,
		logger:     log.New(log.DefaultConfig(log.ErrorLevel)),
	}

	keyPriceRef := 60.0 // 1 key = 60.0 ref

	t.Run("Sell Undercutting", func(t *testing.T) {
		competitorSellPrice = 57.0
		// Lowest competitor sell price is 57.0 ref (513 scrap)
		// We undercut by 1 scrap -> 512 scrap (56.88 ref)
		// 512 scrap is between MinSellPrice (55.0 ref / 495 scrap) and MaxSellPrice (65.0 ref / 585 scrap)
		targetPriceScrap := currency.ToScrap(60.0) // priceDB sell target (e.g. 60.0 ref)

		finalPriceScrap, err := s.getUndercutPrice(
			context.Background(),
			"5021;6",
			"sell",
			targetPriceScrap,
			keyPriceRef,
		)
		require.NoError(t, err)

		expectedPriceScrap := currency.ToScrap(57.0) - 1
		assert.Equal(t, expectedPriceScrap, finalPriceScrap)
	})

	t.Run("Buy Overbidding", func(t *testing.T) {
		competitorBuyPrice = 53.0
		// Highest competitor buy price is 53.0 ref (477 scrap)
		// We overbid by 1 scrap -> 478 scrap (53.11 ref)
		// 478 scrap is between MinBuyPrice (50.0 ref / 450 scrap) and MaxBuyPrice (60.0 ref / 540 scrap)
		targetPriceScrap := currency.ToScrap(50.0) // priceDB buy target (e.g. 50.0 ref)

		finalPriceScrap, err := s.getUndercutPrice(context.Background(), "5021;6", "buy", targetPriceScrap, keyPriceRef)
		require.NoError(t, err)

		expectedPriceScrap := currency.ToScrap(53.0) + 1
		assert.Equal(t, expectedPriceScrap, finalPriceScrap)
	})

	t.Run("Undercut bound checking (floor)", func(t *testing.T) {
		// Set a competitor price below our MinSellPrice (e.g. 54.0 ref)
		// MinSellPrice is 55.0 ref (495 scrap).
		// Undercut should cap at 55.0 ref (495 scrap).
		competitorSellPrice = 54.0

		targetPriceScrap := currency.ToScrap(60.0)
		finalPriceScrap, err := s.getUndercutPrice(
			context.Background(),
			"5021;6",
			"sell",
			targetPriceScrap,
			keyPriceRef,
		)
		require.NoError(t, err)
		assert.Equal(t, currency.ToScrap(55.0), finalPriceScrap)
	})

	t.Run("Overbid bound checking (ceiling)", func(t *testing.T) {
		// Set a competitor price above our MaxBuyPrice (e.g. 61.0 ref)
		// MaxBuyPrice is 60.0 ref (540 scrap).
		// Overbid should cap at 60.0 ref (540 scrap).
		competitorBuyPrice = 61.0

		targetPriceScrap := currency.ToScrap(50.0)
		finalPriceScrap, err := s.getUndercutPrice(context.Background(), "5021;6", "buy", targetPriceScrap, keyPriceRef)
		require.NoError(t, err)
		assert.Equal(t, currency.ToScrap(60.0), finalPriceScrap)
	})
}

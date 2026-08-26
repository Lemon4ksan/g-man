// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package mock_test

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/lemon4ksan/aoni/mod"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lemon4ksan/g-man/pkg/test/mock"
)

type InventoryResponse struct {
	TotalInventoryCount int              `json:"total_inventory_count"`
	Success             int              `json:"success"`
	Assets              []map[string]any `json:"assets"`
}

type TradeOfferData struct {
	TradeOfferID string `json:"tradeofferid"`
}

type GetOfferResponse struct {
	Response struct {
		Offer map[string]any `json:"offer"`
	} `json:"response"`
}

type AcceptResponse struct {
	TradeID string `json:"tradeid"`
}

func TestSteamEmulator_OpenIDLogin(t *testing.T) {
	emu := mock.NewSteamEmulator()
	defer emu.Close()

	// In OpenID, the browser/client hits /openid/login and gets 303 Redirect
	req, err := http.NewRequest(http.MethodGet, emu.BaseURL()+"/openid/login?openid.return_to=https://mysite.com/auth/steam", nil)
	require.NoError(t, err)

	// Direct non-following transport check
	transport := &http.Transport{}
	resp, err := transport.RoundTrip(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusSeeOther, resp.StatusCode)
	loc := resp.Header.Get("Location")
	assert.Contains(t, loc, "openid.claimed_id")
	assert.Contains(t, loc, "openid.mode=id_res")
}

func TestSteamEmulator_InventorySync(t *testing.T) {
	emu := mock.NewSteamEmulator()
	defer emu.Close()

	client := emu.Client()
	ctx := context.Background()

	// 1. Default empty inventory
	res, err := client.Get[InventoryResponse](ctx, "/inventory/76561198000000001/440/2")
	require.NoError(t, err)
	assert.Equal(t, 0, res.TotalInventoryCount)

	// 2. Set custom mock inventory
	customInv := `{"assets":[{"assetid":"101","classid":"201"}],"descriptions":[],"total_inventory_count":1,"success":1}`
	emu.SetInventory(76561198000000001, 440, 2, []byte(customInv))

	res2, err := client.Get[InventoryResponse](ctx, "/inventory/76561198000000001/440/2")
	require.NoError(t, err)
	assert.Equal(t, 1, res2.TotalInventoryCount)
	assert.Len(t, res2.Assets, 1)
	assert.Equal(t, "101", res2.Assets[0]["assetid"])
}

func TestSteamEmulator_TradeOfferLifecycle(t *testing.T) {
	emu := mock.NewSteamEmulator()
	defer emu.Close()

	client := emu.Client()
	ctx := context.Background()

	// 1. Send Trade Offer
	sendRes, err := client.Post[TradeOfferData](ctx, "/tradeoffer/new/send", struct{}{})
	require.NoError(t, err)
	assert.NotEmpty(t, sendRes.TradeOfferID)

	// 2. Check Offer Status
	getRes, err := client.Get[GetOfferResponse](ctx, "/IEconService/GetTradeOffer/v1", mod.WithQuery("tradeofferid", sendRes.TradeOfferID))
	require.NoError(t, err)
	assert.Equal(t, float64(2), getRes.Response.Offer["trade_offer_state"]) // 2 = Active

	// 3. Accept Offer
	acceptRes, err := client.Post[AcceptResponse](ctx, "/tradeoffer/"+sendRes.TradeOfferID+"/accept", struct{}{})
	require.NoError(t, err)
	assert.Equal(t, "900000001", acceptRes.TradeID)

	// 4. Verify Offer Transition to Accepted (State 3)
	getRes2, err := client.Get[GetOfferResponse](ctx, "/IEconService/GetTradeOffer/v1", mod.WithQuery("tradeofferid", sendRes.TradeOfferID))
	require.NoError(t, err)
	assert.Equal(t, float64(3), getRes2.Response.Offer["trade_offer_state"]) // 3 = Accepted
}

func TestSteamEmulator_SimulateErrors(t *testing.T) {
	emu := mock.NewSteamEmulator()
	defer emu.Close()

	client := emu.Client()
	ctx := context.Background()

	// Simulate Rate Limit (429)
	emu.SimulateError("/inventory", http.StatusTooManyRequests)

	_, err := client.Get[InventoryResponse](ctx, "/inventory/76561198000000001/440/2")
	require.Error(t, err)

	// Clear Errors
	emu.ClearErrors()
	res, err := client.Get[InventoryResponse](ctx, "/inventory/76561198000000001/440/2")
	require.NoError(t, err)
	assert.Equal(t, 1, res.Success)
}

func TestSteamEmulator_HARPlayback(t *testing.T) {
	emu := mock.NewSteamEmulator()
	defer emu.Close()

	harData := `{
		"log": {
			"entries": [
				{
					"request": {
						"method": "GET",
						"url": "https://steamcommunity.com/id/lemon4ksan"
					},
					"response": {
						"status": 200,
						"content": {
							"mimeType": "text/html",
							"text": "<html><title>Steam Community :: lemon4ksan</title></html>"
						}
					}
				}
			]
		}
	}`

	err := emu.LoadHAR(strings.NewReader(harData))
	require.NoError(t, err)
}

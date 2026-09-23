// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package web

import (
	"context"
	"fmt"
	"io"
	"math"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lemon4ksan/g-man/pkg/steam/guard"
	"github.com/lemon4ksan/g-man/pkg/steam/id"
	"github.com/lemon4ksan/g-man/pkg/steam/service"
	tr "github.com/lemon4ksan/g-man/pkg/steam/transport"
	"github.com/lemon4ksan/g-man/pkg/trading"
)

// Helper to extract the request body from a recorded HTTP call
func getRequestBody(req io.Reader, getBody func() (io.ReadCloser, error)) string {
	if getBody != nil {
		rc, err := getBody()
		if err == nil && rc != nil {
			defer rc.Close()

			bytes, _ := io.ReadAll(rc)

			return string(bytes)
		}
	}

	if req != nil {
		bytes, _ := io.ReadAll(req)
		return string(bytes)
	}

	return ""
}

// 1. Partner Normalization: Adversarial inputs (0, negative, 32-bit int max, 64-bit ID, malformed).
// Verify partner is NEVER transmitted as "0" to Steam Community.
func TestAdversarial_PartnerNormalization(t *testing.T) {
	t.Parallel()

	t.Run("zero_partner_fails_fast", func(t *testing.T) {
		t.Parallel()

		res, err := NormalizePartnerSteamID(0)
		require.Error(t, err)
		assert.Equal(t, id.ID(0), res)
		assert.Contains(t, err.Error(), "partner SteamID cannot be zero")
	})

	t.Run("32bit_account_id_1_normalized_to_64bit", func(t *testing.T) {
		t.Parallel()

		res, err := NormalizePartnerSteamID(1)
		require.NoError(t, err)
		assert.NotEqual(t, id.ID(0), res)
		assert.NotEqual(t, id.ID(1), res)
		// AccountID 1 becomes 76561197960265729
		expected := id.FromAccountID(1)
		assert.Equal(t, expected, res)
		assert.Equal(t, "76561197960265729", strconv.FormatUint(res.Uint64(), 10))
		assert.NotEqual(t, "0", strconv.FormatUint(res.Uint64(), 10))
	})

	t.Run("32bit_account_id_max_normalized_to_64bit", func(t *testing.T) {
		t.Parallel()

		max32 := uint32(math.MaxUint32)
		res, err := NormalizePartnerSteamID(id.ID(max32))
		require.NoError(t, err)
		assert.NotEqual(t, id.ID(0), res)

		expected := id.FromAccountID(max32)
		assert.Equal(t, expected, res)
		assert.Equal(t, strconv.FormatUint(expected.Uint64(), 10), strconv.FormatUint(res.Uint64(), 10))
		assert.NotEqual(t, "0", strconv.FormatUint(res.Uint64(), 10))
	})

	t.Run("valid_64bit_individual_unchanged", func(t *testing.T) {
		t.Parallel()

		sid64 := id.ID(76561198012345678)
		res, err := NormalizePartnerSteamID(sid64)
		require.NoError(t, err)
		assert.Equal(t, sid64, res)
		assert.Equal(t, "76561198012345678", strconv.FormatUint(res.Uint64(), 10))
	})

	t.Run("adversarial_negative_id_cast_to_uint64_invalid_fails_fast", func(t *testing.T) {
		t.Parallel()

		negAsUint := id.ID(^uint64(0)) // 18446744073709551615
		res, err := NormalizePartnerSteamID(negAsUint)
		require.Error(t, err)
		assert.ErrorIs(t, err, ErrInvalidPartnerSteamID)
		assert.Equal(t, id.ID(0), res)
	})

	t.Run("non_individual_account_types_fail_fast", func(t *testing.T) {
		t.Parallel()

		nonIndividualTypes := []id.AccountType{
			id.AccountTypeMultiseat,
			id.AccountTypeGameServer,
			id.AccountTypeAnonGameServer,
			id.AccountTypeClan,
			id.AccountTypeChat,
		}

		for _, accType := range nonIndividualTypes {
			accType := accType
			t.Run(accType.String(), func(t *testing.T) {
				t.Parallel()

				nonIndivID := id.ID(uint64(id.UniversePublic)<<56 | uint64(accType)<<52 | 1<<32 | 12345)
				res, err := NormalizePartnerSteamID(nonIndivID)
				require.Error(t, err)
				assert.ErrorIs(t, err, ErrInvalidPartnerSteamID)
				assert.Equal(t, id.ID(0), res)
			})
		}
	})

	t.Run("adversarial_negative_int64_cast_fails_fast", func(t *testing.T) {
		t.Parallel()

		negValues := []int64{-1, -2, -100, -9999999}
		for _, negVal := range negValues {
			negVal := negVal
			t.Run(strconv.FormatInt(negVal, 10), func(t *testing.T) {
				t.Parallel()

				negID := id.ID(uint64(negVal))
				res, err := NormalizePartnerSteamID(negID)
				require.Error(t, err)
				assert.ErrorIs(t, err, ErrInvalidPartnerSteamID)
				assert.Equal(t, id.ID(0), res)
			})
		}
	})
}

func TestAdversarial_AcceptOffer_PartnerNeverZero(t *testing.T) {
	t.Parallel()

	t.Run("accept_with_partner_zero_and_failed_getoffer_never_calls_community", func(t *testing.T) {
		t.Parallel()
		f := newTestFixture(t)

		// GetOffer will return not found error
		f.web.SetJSONResponse("IEconService", "GetTradeOffer", map[string]any{
			"response": map[string]any{},
		})

		err := f.manager.AcceptOfferWithPartner(t.Context(), 10001, 0)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "partner SteamID cannot be zero")

		// Verify that NO call was made to Steam Community accept endpoint
		for _, call := range f.comm.Calls {
			assert.NotContains(
				t,
				call.URL.Path,
				"accept",
				"Community accept endpoint must NEVER be called when partner is 0",
			)
		}
	})

	t.Run("accept_with_partner_zero_and_getoffer_otherid_zero_never_calls_community", func(t *testing.T) {
		t.Parallel()
		f := newTestFixture(t)

		// GetOffer returns an offer with OtherSteamID = 0
		f.web.SetJSONResponse("IEconService", "GetTradeOffer", map[string]any{
			"response": map[string]any{
				"offer": map[string]any{
					"tradeofferid":    "10002",
					"accountid_other": 0,
				},
			},
		})

		err := f.manager.AcceptOfferWithPartner(t.Context(), 10002, 0)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "partner SteamID cannot be zero")

		// Verify community was never called
		for _, call := range f.comm.Calls {
			assert.NotContains(
				t,
				call.URL.Path,
				"accept",
				"Community accept endpoint must NEVER be called when partner is 0",
			)
		}
	})

	t.Run("accept_with_32bit_partner_transmits_64bit_steamid_not_zero_or_32bit", func(t *testing.T) {
		t.Parallel()
		f := newTestFixture(t)

		f.comm.SetHTMLResponse("tradeoffer/10003/accept", 200, `{"needs_mobile_confirmation":false}`)

		err := f.manager.AcceptOfferWithPartner(t.Context(), 10003, 45678)
		require.NoError(t, err)

		// Find the call to tradeoffer/10003/accept
		var acceptCallBody string
		for _, call := range f.comm.Calls {
			if strings.Contains(call.URL.Path, "10003/accept") {
				acceptCallBody = getRequestBody(call.Body, call.GetBody)
			}
		}

		require.NotEmpty(t, acceptCallBody, "Expected an HTTP call to accept endpoint")
		vals, parseErr := url.ParseQuery(acceptCallBody)
		require.NoError(t, parseErr)

		partnerVal := vals.Get("partner")
		assert.NotEqual(t, "0", partnerVal, "Partner must NEVER be 0")
		assert.NotEqual(t, "45678", partnerVal, "Partner must NOT be raw 32-bit AccountID")
		assert.Equal(t, "76561197960311406", partnerVal, "Partner must be normalized 64-bit SteamID")
	})

	t.Run("accept_with_64bit_partner_transmits_exact_64bit_steamid", func(t *testing.T) {
		t.Parallel()
		f := newTestFixture(t)

		f.comm.SetHTMLResponse("tradeoffer/10004/accept", 200, `{"needs_mobile_confirmation":false}`)

		sid64 := id.ID(76561198000000002)
		err := f.manager.AcceptOfferWithPartner(t.Context(), 10004, sid64)
		require.NoError(t, err)

		var acceptCallBody string
		for _, call := range f.comm.Calls {
			if strings.Contains(call.URL.Path, "10004/accept") {
				acceptCallBody = getRequestBody(call.Body, call.GetBody)
			}
		}

		require.NotEmpty(t, acceptCallBody)
		vals, parseErr := url.ParseQuery(acceptCallBody)
		require.NoError(t, parseErr)

		partnerVal := vals.Get("partner")
		assert.Equal(t, "76561198000000002", partnerVal)
	})
}

// 2. Send form payload: Verify captcha="" is present and valid in all cases.
func TestAdversarial_SendFormPayload_CaptchaAndFields(t *testing.T) {
	t.Parallel()

	t.Run("sendNewReq_EncodeFormString_contains_empty_captcha_and_normalized_partner", func(t *testing.T) {
		t.Parallel()

		req := sendNewReq{
			ServerID:     1,
			PartnerID:    id.FromAccountID(45678),
			Captcha:      "",
			Message:      "Adversarial offer & test = 123",
			JSON:         `{"newversion":true,"version":2}`,
			CreateParams: `{"trade_offer_access_token":"tok123"}`,
			CounteredID:  998877,
		}

		encoded, err := req.EncodeFormString()
		require.NoError(t, err)

		// Parse form values
		vals, err := url.ParseQuery(encoded)
		require.NoError(t, err)

		// Verification 1: Captcha is present and strictly empty string
		assert.True(t, vals.Has("captcha"), "Form payload must contain 'captcha' key")
		assert.Equal(t, "", vals.Get("captcha"), "Captcha must be empty string")
		assert.Contains(t, encoded, "&captcha=", "Raw payload must contain '&captcha='")

		// Verification 2: ServerID is "1"
		assert.Equal(t, "1", vals.Get("serverid"))

		// Verification 3: Partner is 64-bit normalized SteamID
		assert.Equal(t, "76561197960311406", vals.Get("partner"))
		assert.NotEqual(t, "0", vals.Get("partner"))

		// Verification 4: Other fields
		assert.Equal(t, "Adversarial offer & test = 123", vals.Get("tradeoffermessage"))
		assert.Equal(t, `{"newversion":true,"version":2}`, vals.Get("json_tradeoffer"))
		assert.Equal(t, `{"trade_offer_access_token":"tok123"}`, vals.Get("trade_offer_create_params"))
		assert.Equal(t, "998877", vals.Get("tradeofferid_countered"))
	})

	t.Run("sendNewReq_EncodeFormString_with_32bit_partner_normalizes_to_64bit", func(t *testing.T) {
		t.Parallel()

		req := sendNewReq{
			ServerID:  1,
			PartnerID: 45678, // raw 32-bit AccountID
			Captcha:   "",
			Message:   "test",
			JSON:      "{}",
		}

		encoded, err := req.EncodeFormString()
		require.NoError(t, err)

		vals, err := url.ParseQuery(encoded)
		require.NoError(t, err)

		assert.Equal(t, "76561197960311406", vals.Get("partner"))
		assert.True(t, vals.Has("captcha"))
		assert.Equal(t, "", vals.Get("captcha"))
	})

	t.Run("send_offer_with_zero_partner_fails_fast_and_never_calls_community", func(t *testing.T) {
		t.Parallel()
		f := newTestFixture(t)

		params := trading.OfferParams{
			PartnerID: 0,
			Message:   "Hello",
		}

		offerID, err := f.manager.SendOffer(t.Context(), params)
		require.Error(t, err)
		assert.Equal(t, uint64(0), offerID)
		assert.Contains(t, err.Error(), "partner SteamID cannot be zero")

		for _, call := range f.comm.Calls {
			assert.NotContains(t, call.URL.Path, "tradeoffer/new/send")
		}
	})

	t.Run("send_offer_http_200_empty_body_nil_dereference_safety", func(t *testing.T) {
		t.Parallel()
		f := newTestFixture(t)
		f.comm.SetRawResponse("tradeoffer/new/send", 200, []byte(""))

		params := trading.OfferParams{
			PartnerID: 45678,
			Message:   "test",
		}

		assert.NotPanics(t, func() {
			offerID, err := f.manager.SendOffer(t.Context(), params)
			require.Error(t, err)
			assert.Equal(t, uint64(0), offerID)
			assert.ErrorIs(t, err, ErrEmptyResponse)
		})
	})

	t.Run("accept_offer_payload_includes_empty_captcha", func(t *testing.T) {
		t.Parallel()
		f := newTestFixture(t)

		f.comm.SetHTMLResponse("tradeoffer/20001/accept", 200, `{"needs_mobile_confirmation":false}`)

		err := f.manager.AcceptOfferWithPartner(t.Context(), 20001, id.FromAccountID(45678))
		require.NoError(t, err)

		var acceptBody string
		for _, call := range f.comm.Calls {
			if strings.Contains(call.URL.Path, "20001/accept") {
				acceptBody = getRequestBody(call.Body, call.GetBody)
			}
		}

		require.NotEmpty(t, acceptBody)
		vals, err := url.ParseQuery(acceptBody)
		require.NoError(t, err)

		assert.True(t, vals.Has("captcha"), "Accept payload must contain captcha field")
		assert.Equal(t, "", vals.Get("captcha"), "Accept captcha must be empty string")
		assert.Equal(t, "1", vals.Get("serverid"))
		assert.Equal(t, "20001", vals.Get("tradeofferid"))
		assert.Equal(t, "76561197960311406", vals.Get("partner"))
	})

	t.Run("send_offer_http_200_null_json_nil_dereference_safety", func(t *testing.T) {
		t.Parallel()
		f := newTestFixture(t)

		f.comm.SetRawResponse("tradeoffer/new/send", 200, []byte("null"))

		params := trading.OfferParams{
			PartnerID: 45678,
			Message:   "test null json",
		}

		assert.NotPanics(t, func() {
			offerID, err := f.manager.SendOffer(t.Context(), params)
			require.Error(t, err)
			assert.Equal(t, uint64(0), offerID)
			assert.ErrorIs(t, err, ErrEmptyResponse)
		})
	})

	t.Run("send_offer_http_200_empty_trade_offer_id_fails_without_publishing_event", func(t *testing.T) {
		t.Parallel()
		f := newTestFixture(t)

		f.comm.SetHTMLResponse(
			"tradeoffer/new/send",
			200,
			`{"tradeofferid":"","needs_mobile_confirmation":true}`,
		)

		sub := f.manager.Bus.Subscribe(&guard.ConfirmationRequiredEvent{})
		t.Cleanup(sub.Unsubscribe)

		params := trading.OfferParams{
			PartnerID: 45678,
			Message:   "test empty offer id",
		}

		offerID, err := f.manager.SendOffer(t.Context(), params)
		require.Error(t, err)
		assert.Equal(t, uint64(0), offerID)
		assert.Contains(t, err.Error(), "invalid trade offer ID in response")

		select {
		case ev := <-sub.C():
			t.Fatalf("unexpected ConfirmationRequiredEvent published for empty tradeofferid: %+v", ev)
		case <-time.After(50 * time.Millisecond):
		}
	})

	t.Run("send_offer_http_200_malformed_trade_offer_id_fails_without_publishing_event", func(t *testing.T) {
		t.Parallel()
		f := newTestFixture(t)

		f.comm.SetHTMLResponse(
			"tradeoffer/new/send",
			200,
			`{"tradeofferid":"invalid_not_uint64","needs_mobile_confirmation":true}`,
		)

		sub := f.manager.Bus.Subscribe(&guard.ConfirmationRequiredEvent{})
		t.Cleanup(sub.Unsubscribe)

		params := trading.OfferParams{
			PartnerID: 45678,
			Message:   "test malformed offer id",
		}

		offerID, err := f.manager.SendOffer(t.Context(), params)
		require.Error(t, err)
		assert.Equal(t, uint64(0), offerID)
		assert.Contains(t, err.Error(), "invalid trade offer ID in response")

		select {
		case ev := <-sub.C():
			t.Fatalf("unexpected ConfirmationRequiredEvent published for malformed tradeofferid: %+v", ev)
		case <-time.After(50 * time.Millisecond):
		}
	})

	t.Run("send_offer_http_200_malformed_json_fails", func(t *testing.T) {
		t.Parallel()
		f := newTestFixture(t)

		f.comm.SetRawResponse("tradeoffer/new/send", 200, []byte("{malformed:json}"))

		params := trading.OfferParams{
			PartnerID: 45678,
			Message:   "test malformed json",
		}

		offerID, err := f.manager.SendOffer(t.Context(), params)
		require.Error(t, err)
		assert.Equal(t, uint64(0), offerID)
	})

	t.Run("send_offer_payload_over_the_wire_contains_empty_captcha_and_normalized_partner", func(t *testing.T) {
		t.Parallel()
		f := newTestFixture(t)

		f.comm.SetHTMLResponse(
			"tradeoffer/new/send",
			200,
			`{"tradeofferid":"123456","needs_mobile_confirmation":false}`,
		)

		params := trading.OfferParams{
			PartnerID: 45678,
			Message:   "Hello from over-the-wire test",
		}

		offerID, err := f.manager.SendOffer(t.Context(), params)
		require.NoError(t, err)
		assert.Equal(t, uint64(123456), offerID)

		var sendBody string
		for _, call := range f.comm.Calls {
			if strings.Contains(call.URL.Path, "tradeoffer/new/send") {
				sendBody = getRequestBody(call.Body, call.GetBody)
			}
		}

		require.NotEmpty(t, sendBody)

		vals, err := url.ParseQuery(sendBody)
		require.NoError(t, err)

		assert.True(t, vals.Has("captcha"), "Form body must contain captcha field")
		assert.Equal(t, "", vals.Get("captcha"), "Captcha field must be empty string")
		assert.Contains(t, sendBody, "&captcha=", "Raw body must explicitly contain &captcha=")
		assert.Equal(t, "76561197960311406", vals.Get("partner"))
		assert.NotEqual(t, "0", vals.Get("partner"))
		assert.Equal(t, "1", vals.Get("serverid"))
	})

	t.Run("sendNewReq_EncodeFormString_with_zero_partner_returns_error", func(t *testing.T) {
		t.Parallel()

		req := sendNewReq{
			ServerID:  1,
			PartnerID: 0,
			Captcha:   "",
		}

		_, err := req.EncodeFormString()
		require.Error(t, err)
		assert.ErrorIs(t, err, ErrInvalidPartnerSteamID)
	})
}

// 3. Fallback on HTTP 500 / timeout: Simulate Steam accept failure where offer transitioned to state 9 (CreatedNeedsConfirmation).
// Verify trade is recognized as accepted and ConfirmationRequiredEvent is published.
func TestAdversarial_AcceptOffer_State9ConfirmationFallback(t *testing.T) {
	t.Parallel()

	t.Run("http_500_fallback_to_state_9_emits_confirmation_event_and_returns_nil", func(t *testing.T) {
		t.Parallel()
		f := newTestFixture(t)
		partnerID := id.FromAccountID(OtherAccountID)
		offerID := uint64(30001)

		// 1. Community accept POST responds with HTTP 500 Internal Server Error
		f.comm.SetHTMLResponse(fmt.Sprintf("tradeoffer/%d/accept", offerID), 500, `Internal Server Error`)

		// 2. WebAPI GetTradeOffer responds with State 9 (CreatedNeedsConfirmation)
		f.web.SetJSONResponse("IEconService", "GetTradeOffer", map[string]any{
			"response": map[string]any{
				"offer": map[string]any{
					"tradeofferid":      strconv.FormatUint(offerID, 10),
					"accountid_other":   OtherAccountID,
					"trade_offer_state": int(trading.OfferStateCreatedNeedsConfirmation),
					"items_to_receive": []any{
						map[string]any{
							"appid":            440,
							"contextid":        "2",
							"assetid":          "1",
							"name":             "Key",
							"market_hash_name": "Key",
						},
					},
				},
			},
		})

		sub := f.manager.Bus.Subscribe(&guard.ConfirmationRequiredEvent{})
		t.Cleanup(sub.Unsubscribe)

		err := f.manager.AcceptOfferWithPartner(t.Context(), offerID, partnerID)
		assert.NoError(t, err, "State 9 fallback must recognize the trade and return nil error")

		select {
		case ev := <-sub.C():
			event, ok := ev.(*guard.ConfirmationRequiredEvent)
			require.True(t, ok)
			assert.Equal(t, strconv.FormatUint(offerID, 10), event.TradeOfferID)
			assert.True(t, event.IsAppConfirm)
		case <-time.After(2 * time.Second):
			t.Fatal("timeout waiting for ConfirmationRequiredEvent to be published")
		}
	})

	t.Run("http_504_gateway_timeout_fallback_to_state_9", func(t *testing.T) {
		t.Parallel()
		f := newTestFixture(t)
		partnerID := id.FromAccountID(OtherAccountID)
		offerID := uint64(30002)

		// Community accept POST responds with HTTP 504 Gateway Timeout
		f.comm.SetHTMLResponse(fmt.Sprintf("tradeoffer/%d/accept", offerID), 504, `Gateway Timeout`)

		f.web.SetJSONResponse("IEconService", "GetTradeOffer", map[string]any{
			"response": map[string]any{
				"offer": map[string]any{
					"tradeofferid":      strconv.FormatUint(offerID, 10),
					"accountid_other":   OtherAccountID,
					"trade_offer_state": int(trading.OfferStateCreatedNeedsConfirmation),
					"items_to_receive": []any{
						map[string]any{
							"appid":            440,
							"contextid":        "2",
							"assetid":          "1",
							"name":             "Key",
							"market_hash_name": "Key",
						},
					},
				},
			},
		})

		sub := f.manager.Bus.Subscribe(&guard.ConfirmationRequiredEvent{})
		t.Cleanup(sub.Unsubscribe)

		err := f.manager.AcceptOfferWithPartner(t.Context(), offerID, partnerID)
		assert.NoError(t, err)

		select {
		case ev := <-sub.C():
			event := ev.(*guard.ConfirmationRequiredEvent)
			assert.Equal(t, strconv.FormatUint(offerID, 10), event.TradeOfferID)
			assert.True(t, event.IsAppConfirm)
		case <-time.After(2 * time.Second):
			t.Fatal("timeout waiting for ConfirmationRequiredEvent")
		}
	})

	t.Run("http_500_fallback_to_state_3_accepted_returns_nil_no_confirmation_event", func(t *testing.T) {
		t.Parallel()
		f := newTestFixture(t)
		partnerID := id.FromAccountID(OtherAccountID)
		offerID := uint64(30003)

		f.comm.SetHTMLResponse(fmt.Sprintf("tradeoffer/%d/accept", offerID), 500, `Internal Server Error`)

		// State 3: Accepted
		f.web.SetJSONResponse("IEconService", "GetTradeOffer", map[string]any{
			"response": map[string]any{
				"offer": map[string]any{
					"tradeofferid":      strconv.FormatUint(offerID, 10),
					"accountid_other":   OtherAccountID,
					"trade_offer_state": int(trading.OfferStateAccepted),
					"items_to_receive": []any{
						map[string]any{
							"appid":            440,
							"contextid":        "2",
							"assetid":          "1",
							"name":             "Key",
							"market_hash_name": "Key",
						},
					},
				},
			},
		})

		sub := f.manager.Bus.Subscribe(&guard.ConfirmationRequiredEvent{})
		t.Cleanup(sub.Unsubscribe)

		err := f.manager.AcceptOfferWithPartner(t.Context(), offerID, partnerID)
		assert.NoError(t, err, "State 3 fallback must return nil error")

		select {
		case ev := <-sub.C():
			t.Fatalf("unexpected ConfirmationRequiredEvent published for state 3: %+v", ev)
		case <-time.After(50 * time.Millisecond):
			// Success: no confirmation event expected for already accepted trade
		}
	})

	t.Run("http_500_fallback_to_state_11_in_escrow_returns_nil_no_confirmation_event", func(t *testing.T) {
		t.Parallel()
		f := newTestFixture(t)
		partnerID := id.FromAccountID(OtherAccountID)
		offerID := uint64(30004)

		f.comm.SetHTMLResponse(fmt.Sprintf("tradeoffer/%d/accept", offerID), 500, `Internal Server Error`)

		// State 11: In Escrow
		f.web.SetJSONResponse("IEconService", "GetTradeOffer", map[string]any{
			"response": map[string]any{
				"offer": map[string]any{
					"tradeofferid":      strconv.FormatUint(offerID, 10),
					"accountid_other":   OtherAccountID,
					"trade_offer_state": int(trading.OfferStateInEscrow),
					"items_to_receive": []any{
						map[string]any{
							"appid":            440,
							"contextid":        "2",
							"assetid":          "1",
							"name":             "Key",
							"market_hash_name": "Key",
						},
					},
				},
			},
		})

		sub := f.manager.Bus.Subscribe(&guard.ConfirmationRequiredEvent{})
		t.Cleanup(sub.Unsubscribe)

		err := f.manager.AcceptOfferWithPartner(t.Context(), offerID, partnerID)
		assert.NoError(t, err, "State 11 fallback must return nil error")

		select {
		case ev := <-sub.C():
			t.Fatalf("unexpected ConfirmationRequiredEvent published for state 11: %+v", ev)
		case <-time.After(50 * time.Millisecond):
			// Success
		}
	})

	t.Run("http_500_state_still_active_returns_original_error", func(t *testing.T) {
		t.Parallel()
		f := newTestFixture(t)
		partnerID := id.FromAccountID(OtherAccountID)
		offerID := uint64(30005)

		f.comm.SetHTMLResponse(fmt.Sprintf("tradeoffer/%d/accept", offerID), 500, `Internal Server Error`)

		// State 2: Active (trade was NOT accepted on Steam)
		f.web.SetJSONResponse("IEconService", "GetTradeOffer", map[string]any{
			"response": map[string]any{
				"offer": map[string]any{
					"tradeofferid":      strconv.FormatUint(offerID, 10),
					"accountid_other":   OtherAccountID,
					"trade_offer_state": int(trading.OfferStateActive),
				},
			},
		})

		err := f.manager.AcceptOfferWithPartner(t.Context(), offerID, partnerID)
		require.Error(t, err, "If offer state is still Active after HTTP 500, AcceptOffer must return error")
	})

	t.Run("http_200_empty_body_nil_dereference_safety", func(t *testing.T) {
		t.Parallel()

		f := newTestFixture(t)
		offerID := uint64(30007)
		partnerID := id.FromAccountID(OtherAccountID)

		// Steam returns 200 OK with completely empty body
		f.comm.SetRawResponse(fmt.Sprintf("tradeoffer/%d/accept", offerID), 200, []byte(""))

		// Must not panic with nil pointer dereference
		assert.NotPanics(t, func() {
			err := f.manager.AcceptOfferWithPartner(t.Context(), offerID, partnerID)
			assert.NoError(t, err)
		})
	})

	t.Run("http_200_null_json_nil_dereference_safety", func(t *testing.T) {
		t.Parallel()

		f := newTestFixture(t)
		offerID := uint64(30008)
		partnerID := id.FromAccountID(OtherAccountID)

		f.comm.SetRawResponse(fmt.Sprintf("tradeoffer/%d/accept", offerID), 200, []byte("null"))

		assert.NotPanics(t, func() {
			err := f.manager.AcceptOfferWithPartner(t.Context(), offerID, partnerID)
			assert.NoError(t, err)
		})
	})

	t.Run("http_200_malformed_json_recovers_if_state_is_9", func(t *testing.T) {
		t.Parallel()
		f := newTestFixture(t)
		partnerID := id.FromAccountID(OtherAccountID)
		offerID := uint64(30009)

		f.comm.SetRawResponse(fmt.Sprintf("tradeoffer/%d/accept", offerID), 200, []byte("{malformed:json"))

		f.web.SetJSONResponse("IEconService", "GetTradeOffer", map[string]any{
			"response": map[string]any{
				"offer": map[string]any{
					"tradeofferid":      strconv.FormatUint(offerID, 10),
					"accountid_other":   OtherAccountID,
					"trade_offer_state": int(trading.OfferStateCreatedNeedsConfirmation),
					"items_to_receive": []any{
						map[string]any{
							"appid":            440,
							"contextid":        "2",
							"assetid":          "1",
							"name":             "Key",
							"market_hash_name": "Key",
						},
					},
				},
			},
		})

		sub := f.manager.Bus.Subscribe(&guard.ConfirmationRequiredEvent{})
		t.Cleanup(sub.Unsubscribe)

		err := f.manager.AcceptOfferWithPartner(t.Context(), offerID, partnerID)
		assert.NoError(t, err, "Malformed response with state 9 on WebAPI must recover and return nil")

		select {
		case ev := <-sub.C():
			event := ev.(*guard.ConfirmationRequiredEvent)
			assert.Equal(t, strconv.FormatUint(offerID, 10), event.TradeOfferID)
			assert.True(t, event.IsAppConfirm)
		case <-time.After(2 * time.Second):
			t.Fatal("timeout waiting for ConfirmationRequiredEvent")
		}
	})

	t.Run("http_200_malformed_json_fails_if_state_is_active", func(t *testing.T) {
		t.Parallel()
		f := newTestFixture(t)
		partnerID := id.FromAccountID(OtherAccountID)
		offerID := uint64(30010)

		f.comm.SetRawResponse(fmt.Sprintf("tradeoffer/%d/accept", offerID), 200, []byte("{malformed:json"))

		f.web.SetJSONResponse("IEconService", "GetTradeOffer", map[string]any{
			"response": map[string]any{
				"offer": map[string]any{
					"tradeofferid":      strconv.FormatUint(offerID, 10),
					"accountid_other":   OtherAccountID,
					"trade_offer_state": int(trading.OfferStateActive),
				},
			},
		})

		err := f.manager.AcceptOfferWithPartner(t.Context(), offerID, partnerID)
		require.Error(t, err, "Malformed response with active offer must return error")
	})
}

// 4. Concurrency stress tests with race detector.
func TestAdversarial_AcceptOffer_ConcurrencyStress(t *testing.T) {
	t.Parallel()
	f := newTestFixture(t)

	const (
		numWorkers = 50
		iterations = 5
	)

	var (
		wg             sync.WaitGroup
		successCount   atomic.Uint64
		fallback9Count atomic.Uint64
		errorCount     atomic.Uint64
	)

	// Route GetTradeOffer dynamically per offerID using OnDo
	f.web.OnDo = func(req *tr.Request) (*tr.Response, error) {
		target := req.Target()
		if webTarget, ok := target.(*service.WebAPITarget); ok && webTarget.Interface == "IEconService" &&
			webTarget.Method == "GetTradeOffer" {
			idStr := req.Params().Get("tradeofferid")
			idVal, _ := strconv.ParseUint(idStr, 10, 64)
			num := idVal - 40000

			if num%3 == 1 {
				// State 9 fallback
				respJSON := fmt.Sprintf(`{
					"response": {
						"offer": {
							"tradeofferid": "%s",
							"accountid_other": %d,
							"trade_offer_state": %d
						}
					}
				}`, idStr, OtherAccountID, int(trading.OfferStateCreatedNeedsConfirmation))

				return tr.NewResponse(io.NopCloser(strings.NewReader(respJSON)), tr.HTTPMetadata{StatusCode: 200}), nil
			}

			// Offer not found / no offer for other IDs
			return tr.NewResponse(
				io.NopCloser(strings.NewReader(`{"response":{}}`)),
				tr.HTTPMetadata{StatusCode: 200},
			), nil
		}

		return nil, nil
	}

	// Pre-populate responses for offers
	for i := 1; i <= numWorkers; i++ {
		offerID := uint64(40000 + i)
		switch i % 3 {
		case 0:
			// Normal accept HTTP 200
			f.comm.SetHTMLResponse(
				fmt.Sprintf("tradeoffer/%d/accept", offerID),
				200,
				`{"needs_mobile_confirmation":false}`,
			)

		case 1:
			// HTTP 500 that transitions to State 9 via GetTradeOffer above
			f.comm.SetHTMLResponse(fmt.Sprintf("tradeoffer/%d/accept", offerID), 500, `Internal Server Error`)
		case 2:
			// Partner 0 -> should fail fast because GetTradeOffer returns empty response
		}
	}

	sub := f.manager.Bus.Subscribe(&guard.ConfirmationRequiredEvent{})
	t.Cleanup(sub.Unsubscribe)

	// Collector for events
	var eventCount atomic.Uint64

	stopEvents := make(chan struct{})
	go func() {
		for {
			select {
			case <-sub.C():
				eventCount.Add(1)
			case <-stopEvents:
				return
			}
		}
	}()

	// Launch concurrent readers of manager state
	stopReaders := make(chan struct{})
	go func() {
		for {
			select {
			case <-stopReaders:
				return
			default:
				_ = f.manager.Community()
				_ = f.manager.GetPollData()

				time.Sleep(1 * time.Millisecond)
			}
		}
	}()

	// Launch concurrent AcceptOfferWithPartner workers
	for w := 0; w < numWorkers; w++ {
		wg.Add(1)

		workerID := w + 1
		go func(idVal int) {
			defer wg.Done()

			for it := 0; it < iterations; it++ {
				offerID := uint64(40000 + idVal)

				var partner id.ID

				switch idVal % 3 {
				case 0:
					partner = id.FromAccountID(OtherAccountID)

					err := f.manager.AcceptOfferWithPartner(context.Background(), offerID, partner)
					if err == nil {
						successCount.Add(1)
					}

				case 1:
					partner = id.FromAccountID(OtherAccountID)

					err := f.manager.AcceptOfferWithPartner(context.Background(), offerID, partner)
					if err == nil {
						fallback9Count.Add(1)
					}

				case 2:
					partner = 0

					err := f.manager.AcceptOfferWithPartner(context.Background(), offerID, partner)
					if err != nil {
						errorCount.Add(1)
					}
				}
			}
		}(workerID)
	}

	wg.Wait()
	close(stopReaders)
	time.Sleep(50 * time.Millisecond)
	close(stopEvents)

	assert.Positive(t, successCount.Load())
	assert.Positive(t, fallback9Count.Load())
	assert.Positive(t, errorCount.Load())
	assert.Positive(t, eventCount.Load())
}

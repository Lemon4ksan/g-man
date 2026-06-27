// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package guard

import (
	"errors"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"google.golang.org/protobuf/proto"

	pbSteam "github.com/lemon4ksan/g-man/pkg/protobuf/steam"
	"github.com/lemon4ksan/g-man/pkg/steam/id"
	"github.com/lemon4ksan/g-man/test/mock"
)

const testSteamID = id.ID(76561198000000001)

func TestTwoFactorService_QueryTimeOffset(t *testing.T) {
	t.Parallel()

	t.Run("success", func(t *testing.T) {
		t.Parallel()

		mockSvc := mock.NewServiceMock()
		svc := NewTwoFactorService(mockSvc)

		serverTime := time.Now().Add(30 * time.Second).Unix()

		mockSvc.SetJSONResponse("ITwoFactorService", "QueryTime", map[string]any{
			"response": map[string]string{
				"server_time": strconv.FormatInt(serverTime, 10),
			},
		})

		offset, err := svc.QueryTimeOffset(t.Context())
		assert.NoError(t, err)
		assert.InDelta(t, 30*time.Second, offset, float64(2*time.Second))
	})

	t.Run("invalid_format", func(t *testing.T) {
		t.Parallel()

		mockSvc := mock.NewServiceMock()
		svc := NewTwoFactorService(mockSvc)

		mockSvc.SetJSONResponse("ITwoFactorService", "QueryTime", map[string]any{
			"response": map[string]string{
				"server_time": "not-a-number",
			},
		})

		_, err := svc.QueryTimeOffset(t.Context())
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "invalid server time format")
	})

	t.Run("network_error", func(t *testing.T) {
		t.Parallel()

		mockSvc := mock.NewServiceMock()
		svc := NewTwoFactorService(mockSvc)

		mockSvc.ResponseErrs["ITwoFactorService.QueryTime"] = errors.New("timeout")

		_, err := svc.QueryTimeOffset(t.Context())
		assert.Error(t, err)
	})
}

func TestMobileConf_GetConfirmations(t *testing.T) {
	t.Parallel()

	t.Run("success", func(t *testing.T) {
		t.Parallel()

		mockComm := mock.NewHTTPStub()
		svc := NewMobileConf(mockComm)

		expected := &ConfirmationsList{
			Success: true,
			Confirmations: []*Confirmation{
				{ID: 123, Title: "Trade with Bot"},
			},
		}
		mockComm.SetJSONResponse("mobileconf/getlist", 200, expected)

		resp, err := svc.GetConfirmations(t.Context(), "android:1", testSteamID, "key", 12345)
		assert.NoError(t, err)
		assert.True(t, resp.Success)
		assert.Len(t, resp.Confirmations, 1)
		assert.Equal(t, uint64(123), resp.Confirmations[0].ID)
	})
}

func TestMobileConf_GetConfirmationOfferID(t *testing.T) {
	t.Parallel()

	t.Run("found", func(t *testing.T) {
		t.Parallel()

		mockComm := mock.NewHTTPStub()
		svc := NewMobileConf(mockComm)

		html := `<div>Some content <div id="tradeofferid_987654321"></div></div>`
		mockComm.SetRawResponse("mobileconf/detailspage/123", 200, []byte(html))

		offerID, err := svc.GetConfirmationOfferID(t.Context(), 123, "dev", testSteamID, "key", 0)
		assert.NoError(t, err)
		assert.Equal(t, uint64(987654321), offerID)
	})

	t.Run("not_found", func(t *testing.T) {
		t.Parallel()

		mockComm := mock.NewHTTPStub()
		svc := NewMobileConf(mockComm)

		mockComm.SetRawResponse("mobileconf/detailspage/123", 200, []byte("no offer here"))

		_, err := svc.GetConfirmationOfferID(t.Context(), 123, "dev", testSteamID, "key", 0)
		assert.Error(t, err)
		assert.Equal(t, "offer ID not found in confirmation details page", err.Error())
	})
}

func TestMobileConf_RespondToConfirmation(t *testing.T) {
	t.Parallel()

	conf := &Confirmation{ID: 111, Nonce: 222}

	t.Run("accept_success", func(t *testing.T) {
		t.Parallel()

		mockComm := mock.NewHTTPStub()
		svc := NewMobileConf(mockComm)

		mockComm.SetJSONResponse("mobileconf/ajaxop", 200, map[string]bool{"success": true})

		err := svc.RespondToConfirmation(t.Context(), conf, true, "dev", testSteamID, "key", 0)
		assert.NoError(t, err)

		params := mockComm.GetLastCallParams()
		assert.Equal(t, "allow", params.Get("op"))
		assert.Equal(t, "111", params.Get("cid"))
	})

	t.Run("steam_rejection", func(t *testing.T) {
		t.Parallel()

		mockComm := mock.NewHTTPStub()
		svc := NewMobileConf(mockComm)

		mockComm.SetJSONResponse("mobileconf/ajaxop", 200, map[string]bool{"success": false})

		err := svc.RespondToConfirmation(t.Context(), conf, false, "dev", testSteamID, "key", 0)
		assert.Error(t, err)
		assert.Equal(t, "steam rejected confirmation action", err.Error())
	})
}

func TestMobileConf_RespondToMultiple(t *testing.T) {
	t.Parallel()

	confs := []*Confirmation{
		{ID: 1, Nonce: 10},
		{ID: 2, Nonce: 20},
	}

	t.Run("empty_list", func(t *testing.T) {
		t.Parallel()

		mockComm := mock.NewHTTPStub()
		svc := NewMobileConf(mockComm)

		err := svc.RespondToMultiple(t.Context(), nil, true, "dev", testSteamID, "key", 0)
		assert.NoError(t, err)
		assert.Empty(t, mockComm.Calls)
	})

	t.Run("success", func(t *testing.T) {
		t.Parallel()

		mockComm := mock.NewHTTPStub()
		svc := NewMobileConf(mockComm)

		mockComm.SetJSONResponse("mobileconf/multiajaxop", 200, map[string]any{
			"success": true,
		})

		err := svc.RespondToMultiple(t.Context(), confs, true, "dev", testSteamID, "key", 0)
		assert.NoError(t, err)

		params := mockComm.GetLastCallParams()
		assert.Equal(t, "allow", params.Get("op"))
		assert.ElementsMatch(t, []string{"1", "2"}, params["cid[]"])
		assert.ElementsMatch(t, []string{"10", "20"}, params["ck[]"])
	})

	t.Run("failure_with_message", func(t *testing.T) {
		t.Parallel()

		mockComm := mock.NewHTTPStub()
		svc := NewMobileConf(mockComm)

		mockComm.SetJSONResponse("mobileconf/multiajaxop", 200, map[string]any{
			"success": false,
			"message": "failed manually",
		})

		err := svc.RespondToMultiple(t.Context(), confs, false, "dev", testSteamID, "key", 0)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed manually")
	})
}

func TestTwoFactorService_UnifiedMethods(t *testing.T) {
	t.Parallel()

	t.Run("add_authenticator", func(t *testing.T) {
		t.Parallel()

		mockSvc := mock.NewServiceMock()
		svc := NewTwoFactorService(mockSvc)

		expected := &pbSteam.CTwoFactor_AddAuthenticator_Response{
			SharedSecret: []byte("shared_secret_bytes"),
		}
		mockSvc.SetProtoResponse("TwoFactor", "AddAuthenticator", expected)

		resp, err := svc.AddAuthenticator(t.Context(), testSteamID, "android:mock_id")
		assert.NoError(t, err)
		assert.Equal(t, []byte("shared_secret_bytes"), resp.GetSharedSecret())
	})

	t.Run("finalize_authenticator_success", func(t *testing.T) {
		t.Parallel()

		mockSvc := mock.NewServiceMock()
		svc := NewTwoFactorService(mockSvc)

		expected := &pbSteam.CTwoFactor_FinalizeAddAuthenticator_Response{
			Success: proto.Bool(true),
		}
		mockSvc.SetProtoResponse("TwoFactor", "FinalizeAddAuthenticator", expected)

		resp, err := svc.FinalizeAuthenticator(t.Context(), testSteamID, validSecret, 1700000000, "12345")
		assert.NoError(t, err)
		assert.True(t, resp.GetSuccess())
	})

	t.Run("finalize_authenticator_totp_error", func(t *testing.T) {
		t.Parallel()

		mockSvc := mock.NewServiceMock()
		svc := NewTwoFactorService(mockSvc)

		_, err := svc.FinalizeAuthenticator(t.Context(), testSteamID, "invalid-secret-b64-!!!", 1700000000, "12345")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to generate verification totp code")
	})

	t.Run("query_status", func(t *testing.T) {
		t.Parallel()

		mockSvc := mock.NewServiceMock()
		svc := NewTwoFactorService(mockSvc)

		expected := &pbSteam.CTwoFactor_Status_Response{
			DeviceIdentifier: proto.String("android:status_id"),
		}
		mockSvc.SetProtoResponse("TwoFactor", "Status", expected)

		resp, err := svc.QueryStatus(t.Context(), testSteamID)
		assert.NoError(t, err)
		assert.Equal(t, "android:status_id", resp.GetDeviceIdentifier())
	})

	t.Run("remove_authenticator", func(t *testing.T) {
		t.Parallel()

		mockSvc := mock.NewServiceMock()
		svc := NewTwoFactorService(mockSvc)

		expected := &pbSteam.CTwoFactor_RemoveAuthenticator_Response{
			Success: proto.Bool(true),
		}
		mockSvc.SetProtoResponse("TwoFactor", "RemoveAuthenticator", expected)

		resp, err := svc.RemoveAuthenticator(t.Context(), "revocation_code")
		assert.NoError(t, err)
		assert.True(t, resp.GetSuccess())
	})

	t.Run("remove_authenticator_via_challenge_start", func(t *testing.T) {
		t.Parallel()

		mockSvc := mock.NewServiceMock()
		svc := NewTwoFactorService(mockSvc)

		expected := &pbSteam.CTwoFactor_RemoveAuthenticatorViaChallengeStart_Response{
			Success: proto.Bool(true),
		}
		mockSvc.SetProtoResponse("TwoFactor", "RemoveAuthenticatorViaChallengeStart", expected)

		resp, err := svc.RemoveAuthenticatorViaChallengeStart(t.Context())
		assert.NoError(t, err)
		assert.True(t, resp.GetSuccess())
	})

	t.Run("remove_authenticator_via_challenge_continue", func(t *testing.T) {
		t.Parallel()

		mockSvc := mock.NewServiceMock()
		svc := NewTwoFactorService(mockSvc)

		expected := &pbSteam.CTwoFactor_RemoveAuthenticatorViaChallengeContinue_Response{
			Success: proto.Bool(true),
		}
		mockSvc.SetProtoResponse("TwoFactor", "RemoveAuthenticatorViaChallengeContinue", expected)

		resp, err := svc.RemoveAuthenticatorViaChallengeContinue(t.Context(), testSteamID, "sms_code")
		assert.NoError(t, err)
		assert.True(t, resp.GetSuccess())
	})
}

func TestIsFinalizeWantMore(t *testing.T) {
	t.Parallel()

	t.Run("nil_response", func(t *testing.T) {
		t.Parallel()
		assert.False(t, IsFinalizeWantMore(nil))
	})

	t.Run("no_unknown_fields", func(t *testing.T) {
		t.Parallel()

		resp := &pbSteam.CTwoFactor_FinalizeAddAuthenticator_Response{}
		assert.False(t, IsFinalizeWantMore(resp))
	})

	t.Run("tag_2_varint_non_zero_want_more", func(t *testing.T) {
		t.Parallel()

		resp := &pbSteam.CTwoFactor_FinalizeAddAuthenticator_Response{}
		// Wire format: Tag 2, Type Varint (0): (2 << 3) | 0 = 16 = 0x10. Value 1 = 0x01
		resp.ProtoReflect().SetUnknown([]byte{0x10, 0x01})
		assert.True(t, IsFinalizeWantMore(resp))
	})

	t.Run("tag_2_varint_zero_dont_want_more", func(t *testing.T) {
		t.Parallel()

		resp := &pbSteam.CTwoFactor_FinalizeAddAuthenticator_Response{}
		// Wire format: Tag 2, Type Varint (0): 0x10. Value 0 = 0x00
		resp.ProtoReflect().SetUnknown([]byte{0x10, 0x00})
		assert.False(t, IsFinalizeWantMore(resp))
	})

	t.Run("skip_other_tags_before_tag_2", func(t *testing.T) {
		t.Parallel()

		resp := &pbSteam.CTwoFactor_FinalizeAddAuthenticator_Response{}
		// Tag 3, Type Varint (0): (3 << 3) | 0 = 24 = 0x18. Value 5 = 0x05
		// Followed by Tag 2, Type Varint (0): 0x10. Value 1 = 0x01
		resp.ProtoReflect().SetUnknown([]byte{0x18, 0x05, 0x10, 0x01})
		assert.True(t, IsFinalizeWantMore(resp))
	})

	t.Run("malformed_bytes_negative_consume_field_value", func(t *testing.T) {
		t.Parallel()

		resp := &pbSteam.CTwoFactor_FinalizeAddAuthenticator_Response{}
		// Tag 3, Type Varint, but missing its value byte (causes parse error / n < 0)
		resp.ProtoReflect().SetUnknown([]byte{0x18})
		assert.False(t, IsFinalizeWantMore(resp))
	})
}

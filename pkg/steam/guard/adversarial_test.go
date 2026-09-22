// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package guard

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lemon4ksan/g-man/pkg/steam/service"
	"github.com/lemon4ksan/g-man/pkg/test/mock"
)

func TestAdversarial_MultiAjaxOp_MalformedJSON(t *testing.T) {
	t.Parallel()

	confs := []*Confirmation{
		{ID: 1001, Nonce: 2001},
		{ID: 1002, Nonce: 2002},
	}

	t.Run("truncated_json_body", func(t *testing.T) {
		t.Parallel()

		mockComm := mock.NewHTTPStub()
		svc := NewMobileConf(mockComm)

		mockComm.SetRawResponse("mobileconf/multiajaxop", 200, []byte(`{"success":`))

		err := svc.RespondToMultiple(t.Context(), confs, true, "dev", testSteamID, "key", 0)
		require.Error(t, err)
		assert.False(t, errors.Is(err, ErrConfirmationRejected), "Truncated JSON must NOT be classified as confirmation rejected")
		assert.False(t, errors.Is(err, service.ErrSessionExpired), "Truncated JSON must NOT be classified as session expired")
	})

	t.Run("html_cloudflare_error", func(t *testing.T) {
		t.Parallel()

		mockComm := mock.NewHTTPStub()
		svc := NewMobileConf(mockComm)

		cloudflareHTML := `<!DOCTYPE html><html><head><title>504 Gateway Time-out</title></head><body><center><h1>504 Gateway Time-out</h1></center></body></html>`
		mockComm.SetHTMLResponse("mobileconf/multiajaxop", 504, cloudflareHTML)

		err := svc.RespondToMultiple(t.Context(), confs, true, "dev", testSteamID, "key", 0)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "HTML")
		assert.False(t, errors.Is(err, ErrConfirmationRejected), "HTML Cloudflare error must NOT be classified as confirmation rejected")
		assert.False(t, errors.Is(err, service.ErrSessionExpired), "HTML Cloudflare error must NOT be classified as session expired")
	})

	t.Run("non_json_500_internal_server_error", func(t *testing.T) {
		t.Parallel()

		mockComm := mock.NewHTTPStub()
		svc := NewMobileConf(mockComm)

		mockComm.SetRawResponse("mobileconf/multiajaxop", 500, []byte("Internal Server Error"))

		err := svc.RespondToMultiple(t.Context(), confs, true, "dev", testSteamID, "key", 0)
		require.Error(t, err)
		assert.False(t, errors.Is(err, ErrConfirmationRejected), "500 Internal Server Error must NOT be classified as confirmation rejected")
		assert.False(t, errors.Is(err, service.ErrSessionExpired), "500 Internal Server Error must NOT be classified as session expired")
	})

	t.Run("empty_response_body_causes_nil_panic", func(t *testing.T) {
		t.Parallel()

		mockComm := mock.NewHTTPStub()
		svc := NewMobileConf(mockComm)

		mockComm.SetRawResponse("mobileconf/multiajaxop", 200, []byte(""))

		assert.NotPanics(t, func() {
			err := svc.RespondToMultiple(t.Context(), confs, true, "dev", testSteamID, "key", 0)
			assert.Error(t, err)
			assert.ErrorIs(t, err, ErrConfirmationRejected)
		})
	})

	t.Run("single_conf_empty_response_body_causes_nil_panic", func(t *testing.T) {
		t.Parallel()

		mockComm := mock.NewHTTPStub()
		svc := NewMobileConf(mockComm)

		mockComm.SetRawResponse("mobileconf/ajaxop", 200, []byte(""))

		assert.NotPanics(t, func() {
			err := svc.RespondToConfirmation(t.Context(), confs[0], true, "dev", testSteamID, "key", 0)
			assert.Error(t, err)
			assert.ErrorIs(t, err, ErrConfirmationRejected)
		})
	})
}

func TestAdversarial_MultiAjaxOp_DetailAndNeedAuth(t *testing.T) {
	t.Parallel()

	confs := []*Confirmation{
		{ID: 1001, Nonce: 2001},
		{ID: 1002, Nonce: 2002},
	}

	t.Run("empty_message_with_non_empty_detail", func(t *testing.T) {
		t.Parallel()

		mockComm := mock.NewHTTPStub()
		svc := NewMobileConf(mockComm)

		mockComm.SetJSONResponse("mobileconf/multiajaxop", 200, map[string]any{
			"success": false,
			"message": "",
			"detail":  "Trade offer no longer valid",
		})

		err := svc.RespondToMultiple(t.Context(), confs, true, "dev", testSteamID, "key", 0)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "Trade offer no longer valid")
	})

	t.Run("needauth_true_with_non_empty_message", func(t *testing.T) {
		t.Parallel()

		mockComm := mock.NewHTTPStub()
		svc := NewMobileConf(mockComm)

		mockComm.SetJSONResponse("mobileconf/multiajaxop", 200, map[string]any{
			"success":  false,
			"needauth": true,
			"message":  "You must be logged in to confirm trades",
		})

		err := svc.RespondToMultiple(t.Context(), confs, true, "dev", testSteamID, "key", 0)
		require.Error(t, err)
		assert.ErrorIs(t, err, service.ErrSessionExpired, "needauth: true MUST return service.ErrSessionExpired even with non-empty message")
	})
}

func TestAdversarial_ErrorTyping_PermanentVsTransient(t *testing.T) {
	t.Parallel()

	singleConf := []*Confirmation{{ID: 501, Nonce: 601}}
	multiConfs := []*Confirmation{
		{ID: 501, Nonce: 601},
		{ID: 502, Nonce: 602},
	}

	t.Run("single_confirmation_rejection_is_typed", func(t *testing.T) {
		t.Parallel()

		mockComm := mock.NewHTTPStub()
		svc := NewMobileConf(mockComm)

		mockComm.SetJSONResponse("mobileconf/ajaxop", 200, map[string]any{
			"success": false,
			"message": "Confirmation not found",
		})

		err := svc.RespondToConfirmation(t.Context(), singleConf[0], true, "dev", testSteamID, "key", 0)
		require.Error(t, err)
		assert.ErrorIs(t, err, ErrConfirmationRejected, "Single confirmation rejection must be typed as ErrConfirmationRejected")
	})

	t.Run("multi_confirmation_rejection_is_typed", func(t *testing.T) {
		t.Parallel()

		mockComm := mock.NewHTTPStub()
		svc := NewMobileConf(mockComm)

		mockComm.SetJSONResponse("mobileconf/multiajaxop", 200, map[string]any{
			"success": false,
			"message": "Trade offer no longer valid",
		})

		err := svc.RespondToMultiple(t.Context(), multiConfs, true, "dev", testSteamID, "key", 0)
		require.Error(t, err)
		// Empirical check: Does RespondToMultiple wrap ErrConfirmationRejected?
		assert.ErrorIs(t, err, ErrConfirmationRejected, "Multi confirmation rejection must be typed as ErrConfirmationRejected")
	})

	t.Run("transient_network_error_not_conf_rejected", func(t *testing.T) {
		t.Parallel()

		mockComm := mock.NewHTTPStub()
		svc := NewMobileConf(mockComm)

		mockComm.ResponseErrs["mobileconf/multiajaxop"] = errors.New("connection reset by peer")

		err := svc.RespondToMultiple(t.Context(), multiConfs, true, "dev", testSteamID, "key", 0)
		require.Error(t, err)
		assert.False(t, errors.Is(err, ErrConfirmationRejected), "Network error must not be ErrConfirmationRejected")
		assert.False(t, errors.Is(err, service.ErrSessionExpired), "Network error must not be ErrSessionExpired")
	})

	t.Run("guardian_respond_parity_single_vs_multi", func(t *testing.T) {
		t.Parallel()

		cfg := defaultValidConfig()

		// Single confirmation rejection via Guardian
		gSingle, _, _ := setupAuthenticatedGuardian(t, cfg, testSteamID)
		mockCommSingle := mock.NewHTTPStub()
		mockCommSingle.SetJSONResponse("mobileconf/ajaxop", 200, map[string]any{
			"success": false,
			"message": "Offer cancelled",
		})
		gSingle.service = NewMobileConf(mockCommSingle)

		errSingle := gSingle.Accept(t.Context(), singleConf[0])
		require.Error(t, errSingle)
		assert.ErrorIs(t, errSingle, ErrConfirmationRejected, "Guardian single confirmation rejection must wrap ErrConfirmationRejected")

		// Multi confirmation rejection via Guardian
		gMulti, _, _ := setupAuthenticatedGuardian(t, cfg, testSteamID)
		mockCommMulti := mock.NewHTTPStub()
		mockCommMulti.SetJSONResponse("mobileconf/multiajaxop", 200, map[string]any{
			"success": false,
			"message": "Offer cancelled",
		})
		gMulti.service = NewMobileConf(mockCommMulti)

		errMulti := gMulti.AcceptMultiple(t.Context(), multiConfs)
		require.Error(t, errMulti)
		assert.ErrorIs(t, errMulti, ErrConfirmationRejected, "Guardian multi confirmation rejection MUST wrap ErrConfirmationRejected to match single confirmation")
	})
}

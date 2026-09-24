// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package guard

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	testifymock "github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/lemon4ksan/g-man/internal/crypto"
	"github.com/lemon4ksan/g-man/pkg/steam/id"
	"github.com/lemon4ksan/g-man/pkg/steam/service"
	testmock "github.com/lemon4ksan/g-man/pkg/test/mock"
)

// =================================================================================================
// Section 1: Action Tags Under Adversarial Inputs
// =================================================================================================

func TestAdversarial_ActionTags_InvalidAndExtreme(t *testing.T) {
	t.Parallel()

	secret := []byte("12345678901234567890")
	timestamp := int64(1700000000)

	adversarialTags := []struct {
		name string
		tag  string
	}{
		{name: "empty_tag", tag: ""},
		{name: "whitespace_only", tag: "   \t\n"},
		{name: "trailing_space", tag: "allow "},
		{name: "leading_newline", tag: "\ncancel"},
		{name: "null_byte_embedded", tag: "allow\x00evil"},
		{name: "special_chars", tag: "!@#$%^&*()_+-=[]{}|;':\",./<>?"},
		{name: "unicode_cyrillic", tag: "разрешить"},
		{name: "unicode_emoji", tag: "✅allow❌"},
		{name: "exact_32_chars", tag: strings.Repeat("A", 32)},
		{name: "length_33_chars_boundary", tag: strings.Repeat("B", 33)},
		{name: "length_64_chars", tag: strings.Repeat("C", 64)},
		{name: "length_1024_chars_overflow_attempt", tag: strings.Repeat("D", 1024)},
	}

	cfg := Config{
		IdentitySecret: validSecret,
		DeviceID:       "android:123",
	}
	g, err := New(cfg)
	require.NoError(t, err)

	for _, tc := range adversarialTags {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			// 1. GenerateConfirmationKey directly
			key := GenerateConfirmationKey(secret, timestamp, tc.tag)
			assert.NotEmpty(t, key, "Key must not be empty even for adversarial tag: %q", tc.tag)
			assert.Len(t, key, 28, "Key must strictly be 28 characters base64")

			// 2. Guardian.ConfirmationKey method
			gKey, err := g.ConfirmationKey(tc.tag, timestamp)
			require.NoError(t, err)
			assert.Len(t, gKey, 28)

			// 3. Determinism: identical input must always produce identical key
			repeatKey := GenerateConfirmationKey(secret, timestamp, tc.tag)
			assert.Equal(t, key, repeatKey)
		})
	}
}

func TestAdversarial_ActionTags_CaseSensitivityAndParity(t *testing.T) {
	t.Parallel()

	secret := []byte("12345678901234567890")
	timestamp := int64(1700000000)

	// In HMAC-SHA1, uppercase vs lowercase variations must produce DIFFERENT hashes.
	// Steam strictly expects lowercase "allow" and "cancel".
	casePairs := []struct {
		canonical string
		variant   string
	}{
		{canonical: "allow", variant: "ALLOW"},
		{canonical: "allow", variant: "Allow"},
		{canonical: "allow", variant: "aLLoW"},
		{canonical: "cancel", variant: "CANCEL"},
		{canonical: "cancel", variant: "Cancel"},
		{canonical: "cancel", variant: "cAnCeL"},
		{canonical: "accept", variant: "ACCEPT"},
		{canonical: "reject", variant: "REJECT"},
		{canonical: "conf", variant: "CONF"},
		{canonical: "details", variant: "DETAILS"},
	}

	for _, pair := range casePairs {
		t.Run(fmt.Sprintf("%s_vs_%s", pair.canonical, pair.variant), func(t *testing.T) {
			t.Parallel()

			keyCanonical := GenerateConfirmationKey(secret, timestamp, pair.canonical)
			keyVariant := GenerateConfirmationKey(secret, timestamp, pair.variant)

			assert.NotEqual(t, keyCanonical, keyVariant,
				"Case variant %q MUST NOT collide with canonical %q HMAC key", pair.variant, pair.canonical)
		})
	}
}

func TestAdversarial_ActionTags_GuardianAlwaysEmitsCanonicalLowercase(t *testing.T) {
	t.Parallel()

	cfg := defaultValidConfig()
	g, _, mockSvc := setupAuthenticatedGuardian(t, cfg, id.ID(123))

	secretBytes, err := decodeSecret(cfg.IdentitySecret)
	require.NoError(t, err)

	conf := &Confirmation{ID: 99999, Nonce: 88888}

	// 1. Accept must ALWAYS derive HMAC with "allow" and pass "allow"
	mockSvc.On("RespondToConfirmation", testifymock.Anything, conf, true, cfg.DeviceID, g.SteamID(),
		testifymock.MatchedBy(func(key string) bool {
			expected := crypto.GenerateConfirmationKey(secretBytes, g.clock.Now().Unix(), "allow")
			return key == string(expected[:])
		}),
		testifymock.Anything,
	).Return(nil).Once()

	err = g.Accept(t.Context(), conf)
	require.NoError(t, err)

	// 2. Cancel must ALWAYS derive HMAC with "cancel" and pass "cancel"
	mockSvc.On("RespondToConfirmation", testifymock.Anything, conf, false, cfg.DeviceID, g.SteamID(),
		testifymock.MatchedBy(func(key string) bool {
			expected := crypto.GenerateConfirmationKey(secretBytes, g.clock.Now().Unix(), "cancel")
			return key == string(expected[:])
		}),
		testifymock.Anything,
	).Return(nil).Once()

	err = g.Cancel(t.Context(), conf)
	require.NoError(t, err)
}

func TestAdversarial_ActionTags_URLParametersMatchCanonical(t *testing.T) {
	t.Parallel()

	conf := &Confirmation{ID: 101, Nonce: 202}

	t.Run("single_allow_query_params", func(t *testing.T) {
		t.Parallel()

		mockComm := testmock.NewHTTPStub()
		svc := NewMobileConf(mockComm)
		mockComm.SetJSONResponse("mobileconf/ajaxop", 200, map[string]bool{"success": true})

		err := svc.RespondToConfirmation(t.Context(), conf, true, "android:dev", testSteamID, "confkey", 1700000000)
		require.NoError(t, err)

		params := mockComm.GetLastCallParams()
		assert.Equal(t, "allow", params.Get("op"), "Op must strictly be lowercase allow")
		assert.Equal(t, "allow", params.Get("tag"), "Tag must strictly be lowercase allow")
		assert.Equal(t, "101", params.Get("cid"))
		assert.Equal(t, "202", params.Get("ck"))
		assert.Equal(t, "confkey", params.Get("k"))
	})

	t.Run("single_cancel_query_params", func(t *testing.T) {
		t.Parallel()

		mockComm := testmock.NewHTTPStub()
		svc := NewMobileConf(mockComm)
		mockComm.SetJSONResponse("mobileconf/ajaxop", 200, map[string]bool{"success": true})

		err := svc.RespondToConfirmation(t.Context(), conf, false, "android:dev", testSteamID, "confkey", 1700000000)
		require.NoError(t, err)

		params := mockComm.GetLastCallParams()
		assert.Equal(t, "cancel", params.Get("op"), "Op must strictly be lowercase cancel")
		assert.Equal(t, "cancel", params.Get("tag"), "Tag must strictly be lowercase cancel")
	})

	t.Run("multi_allow_form_params", func(t *testing.T) {
		t.Parallel()

		mockComm := testmock.NewHTTPStub()
		svc := NewMobileConf(mockComm)
		mockComm.SetJSONResponse("mobileconf/multiajaxop", 200, map[string]bool{"success": true})

		confs := []*Confirmation{{ID: 101, Nonce: 202}, {ID: 102, Nonce: 203}}
		err := svc.RespondToMultiple(t.Context(), confs, true, "android:dev", testSteamID, "confkey", 1700000000)
		require.NoError(t, err)

		params := mockComm.GetLastCallParams()
		assert.Equal(t, "allow", params.Get("op"))
		assert.Equal(t, "allow", params.Get("tag"))
		assert.Equal(t, []string{"101", "102"}, params["cid[]"])
		assert.Equal(t, []string{"202", "203"}, params["ck[]"])
	})

	t.Run("multi_cancel_form_params", func(t *testing.T) {
		t.Parallel()

		mockComm := testmock.NewHTTPStub()
		svc := NewMobileConf(mockComm)
		mockComm.SetJSONResponse("mobileconf/multiajaxop", 200, map[string]bool{"success": true})

		confs := []*Confirmation{{ID: 101, Nonce: 202}, {ID: 102, Nonce: 203}}
		err := svc.RespondToMultiple(t.Context(), confs, false, "android:dev", testSteamID, "confkey", 1700000000)
		require.NoError(t, err)

		params := mockComm.GetLastCallParams()
		assert.Equal(t, "cancel", params.Get("op"))
		assert.Equal(t, "cancel", params.Get("tag"))
	})
}

// =================================================================================================
// Section 2: Concurrency & Nonce Integrity (50–100 Concurrent Operations)
// =================================================================================================

func TestAdversarial_Concurrency_100_ConcurrentSingleConfirmations(t *testing.T) {
	t.Parallel()

	cfg := defaultValidConfig()
	cfg.RateLimit = 0 // Disable rate limit for concurrency pressure test
	g, _, mockSvc := setupAuthenticatedGuardian(t, cfg, id.ID(123))

	const numOps = 100
	mockSvc.On("RespondToConfirmation", testifymock.Anything, testifymock.Anything, testifymock.Anything,
		cfg.DeviceID, g.SteamID(), testifymock.Anything, testifymock.Anything).
		Return(nil).Times(numOps)

	var wg sync.WaitGroup
	wg.Add(numOps)

	startBarrier := make(chan struct{})

	for i := range numOps {
		go func(idx int) {
			defer wg.Done()

			<-startBarrier

			conf := &Confirmation{
				ID:    uint64(1000 + idx),
				Nonce: uint64(5000 + idx),
			}
			isAccept := idx%2 == 0

			var err error

			if isAccept {
				err = g.Accept(context.Background(), conf)
			} else {
				err = g.Cancel(context.Background(), conf)
			}

			assert.NoError(t, err)
		}(i)
	}

	// Release all 100 goroutines simultaneously
	close(startBarrier)
	wg.Wait()

	// Verify metrics atomicity under race detector
	totalAccepted := g.Metrics().TotalAccepted.Load()
	totalRejected := g.Metrics().TotalRejected.Load()
	totalErrors := g.Metrics().TotalErrors.Load()

	assert.Equal(t, int64(numOps/2), totalAccepted, "TotalAccepted must be exactly 50")
	assert.Equal(t, int64(numOps/2), totalRejected, "TotalRejected must be exactly 50")
	assert.Equal(t, int64(0), totalErrors, "TotalErrors must be 0")
}

func TestAdversarial_Concurrency_BatchMultiConfirmation_100_Nonces(t *testing.T) {
	t.Parallel()

	const batchSize = 100

	confs := make([]*Confirmation, batchSize)
	for i := range batchSize {
		confs[i] = &Confirmation{
			ID:    uint64(20000 + i),
			Nonce: uint64(90000 + i),
		}
	}

	mockComm := testmock.NewHTTPStub()
	svc := NewMobileConf(mockComm)
	mockComm.SetJSONResponse("mobileconf/multiajaxop", 200, map[string]bool{"success": true})

	err := svc.RespondToMultiple(t.Context(), confs, true, "android:dev", testSteamID, "batchkey", 1700000000)
	require.NoError(t, err)

	params := mockComm.GetLastCallParams()
	cidList := params["cid[]"]
	ckList := params["ck[]"]

	require.Len(t, cidList, batchSize, "cid[] count must match batch size")
	require.Len(t, ckList, batchSize, "ck[] count must match batch size")

	// Nonce pairing oracle: verify 1-to-1 index alignment and zero permutation
	for i := range batchSize {
		expectedCID := strconv.FormatUint(uint64(20000+i), 10)
		expectedCK := strconv.FormatUint(uint64(90000+i), 10)
		assert.Equal(t, expectedCID, cidList[i], "cid[%d] desynced", i)
		assert.Equal(t, expectedCK, ckList[i], "ck[%d] desynced", i)
	}
}

func TestAdversarial_Concurrency_50_ConcurrentBatchConfirmations_DuplicateNonces(t *testing.T) {
	t.Parallel()

	mockComm := testmock.NewHTTPStub()
	svc := NewMobileConf(mockComm)
	mockComm.SetJSONResponse("mobileconf/multiajaxop", 200, map[string]bool{"success": true})

	const numWorkers = 50

	var wg sync.WaitGroup

	wg.Add(numWorkers)

	startBarrier := make(chan struct{})

	// Pre-build batches where duplicate nonces are intentionally present
	// (e.g. concurrent workers trying to confirm the same items)
	duplicateBatch := []*Confirmation{
		{ID: 777, Nonce: 888},
		{ID: 777, Nonce: 888}, // duplicate in same batch
		{ID: 778, Nonce: 889},
	}

	var successCount atomic.Int64
	for range numWorkers {
		go func() {
			defer wg.Done()

			<-startBarrier

			err := svc.RespondToMultiple(
				context.Background(),
				duplicateBatch,
				true,
				"android:dev",
				testSteamID,
				"key",
				1700000000,
			)
			if err == nil {
				successCount.Add(1)
			}
		}()
	}

	close(startBarrier)
	wg.Wait()

	assert.Equal(
		t,
		int64(numWorkers),
		successCount.Load(),
		"All 50 concurrent batch calls must complete without race/corruption",
	)
}

func TestAdversarial_MultiRequest_EncodeFormString_AdversarialInputs(t *testing.T) {
	t.Parallel()

	t.Run("empty_confs", func(t *testing.T) {
		t.Parallel()

		req := multiRequest{
			baseParams: baseParams{
				DeviceID:  "android:123",
				SteamID:   testSteamID,
				ConfKey:   "key",
				Timestamp: 1700000000,
				Mode:      "react",
				ActionTag: "allow",
			},
			ConfIDs: nil,
			Nonces:  nil,
		}

		encoded, err := req.EncodeFormString()
		require.NoError(t, err)
		assert.NotContains(t, encoded, "cid[]=")
		assert.NotContains(t, encoded, "ck[]=")
	})

	t.Run("escaped_characters_in_device_id_and_key", func(t *testing.T) {
		t.Parallel()

		req := multiRequest{
			baseParams: baseParams{
				DeviceID:  "android:test device&evil=true",
				SteamID:   testSteamID,
				ConfKey:   "key+with/slash==&tag=hacked",
				Timestamp: 1700000000,
				Mode:      "react",
				ActionTag: "allow",
			},
			ConfIDs: []uint64{1},
			Nonces:  []uint64{2},
		}

		encoded, err := req.EncodeFormString()
		require.NoError(t, err)

		// Parse back as URL query values to verify proper escaping
		vals, err := url.ParseQuery(encoded)
		require.NoError(t, err)

		assert.Equal(t, "android:test device&evil=true", vals.Get("p"))
		assert.Equal(t, "key+with/slash==&tag=hacked", vals.Get("k"))
		assert.Equal(t, "allow", vals.Get("tag"))
		assert.Empty(t, vals.Get("evil"), "Injection via DeviceID must be escaped and not create new keys")
	})
}

// =================================================================================================
// Section 3: Session Expiration Simulation (needauth: true)
// =================================================================================================

func TestAdversarial_SessionExpired_SingleConfirmation_NeedAuth_NeverSwallowed(t *testing.T) {
	t.Parallel()

	conf := &Confirmation{ID: 1234, Nonce: 5678}

	adversarialNeedAuthPayloads := []struct {
		name     string
		response map[string]any
	}{
		{
			name: "needauth_true_only",
			response: map[string]any{
				"success":  false,
				"needauth": true,
			},
		},
		{
			name: "needauth_true_with_message",
			response: map[string]any{
				"success":  false,
				"needauth": true,
				"message":  "Please log in to Steam",
			},
		},
		{
			name: "needauth_true_with_detail",
			response: map[string]any{
				"success":  false,
				"needauth": true,
				"detail":   "Session token revoked or expired",
			},
		},
		{
			name: "needauth_true_with_message_and_detail",
			response: map[string]any{
				"success":  false,
				"needauth": true,
				"message":  "Session invalid",
				"detail":   "OAuth token expired",
			},
		},
		{
			name: "needauth_true_even_if_success_true_anomaly",
			response: map[string]any{
				"success":  true, // Valve anomaly: needauth true with success true
				"needauth": true,
			},
		},
	}

	for _, tc := range adversarialNeedAuthPayloads {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			mockComm := testmock.NewHTTPStub()
			svc := NewMobileConf(mockComm)
			mockComm.SetJSONResponse("mobileconf/ajaxop", 200, tc.response)

			err := svc.RespondToConfirmation(t.Context(), conf, true, "dev", testSteamID, "key", 0)
			require.Error(t, err)

			// MUST strictly be service.ErrSessionExpired
			assert.ErrorIs(t, err, service.ErrSessionExpired,
				"needauth: true must trigger service.ErrSessionExpired directly without swallowing")
			assert.False(t, errors.Is(err, ErrConfirmationRejected),
				"Session expiration must NOT be masked as ErrConfirmationRejected")
		})
	}
}

func TestAdversarial_SessionExpired_MultiConfirmation_NeedAuth_NeverSwallowed(t *testing.T) {
	t.Parallel()

	confs := []*Confirmation{
		{ID: 1, Nonce: 10},
		{ID: 2, Nonce: 20},
	}

	adversarialMultiPayloads := []struct {
		name     string
		response map[string]any
	}{
		{
			name: "multi_needauth_true_only",
			response: map[string]any{
				"success":  false,
				"needauth": true,
			},
		},
		{
			name: "multi_needauth_with_message",
			response: map[string]any{
				"success":  false,
				"needauth": true,
				"message":  "Access denied: authentication required",
			},
		},
		{
			name: "multi_needauth_with_detail",
			response: map[string]any{
				"success":  false,
				"needauth": true,
				"detail":   "Your session has ended",
			},
		},
	}

	for _, tc := range adversarialMultiPayloads {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			mockComm := testmock.NewHTTPStub()
			svc := NewMobileConf(mockComm)
			mockComm.SetJSONResponse("mobileconf/multiajaxop", 200, tc.response)

			err := svc.RespondToMultiple(t.Context(), confs, true, "dev", testSteamID, "key", 0)
			require.Error(t, err)

			// MUST strictly be service.ErrSessionExpired
			assert.ErrorIs(t, err, service.ErrSessionExpired,
				"needauth: true in multiajaxop must trigger service.ErrSessionExpired")
		})
	}
}

func TestAdversarial_SessionExpired_GuardianIntegrationPropagation(t *testing.T) {
	t.Parallel()

	cfg := defaultValidConfig()
	g, _, mockSvc := setupAuthenticatedGuardian(t, cfg, id.ID(123))

	conf := &Confirmation{ID: 555, Nonce: 666}

	// Configure mock to return service.ErrSessionExpired
	mockSvc.On(
		"RespondToConfirmation",
		testifymock.Anything,
		conf,
		true,
		testifymock.Anything,
		g.SteamID(),
		testifymock.Anything,
		testifymock.Anything,
	).Return(service.ErrSessionExpired).Once()

	err := g.Accept(t.Context(), conf)
	require.Error(t, err)

	assert.ErrorIs(t, err, service.ErrSessionExpired,
		"Guardian.Accept must bubble up service.ErrSessionExpired without modification")
	assert.Equal(t, int64(1), g.Metrics().TotalErrors.Load(),
		"Guardian.Metrics().TotalErrors must record the session expiration failure")
}

func TestAdversarial_SessionExpired_DetailFallbackPriority(t *testing.T) {
	t.Parallel()

	conf := &Confirmation{ID: 11, Nonce: 22}

	t.Run("single_conf_detail_priority_when_message_empty", func(t *testing.T) {
		t.Parallel()

		mockComm := testmock.NewHTTPStub()
		svc := NewMobileConf(mockComm)

		mockComm.SetJSONResponse("mobileconf/ajaxop", 200, map[string]any{
			"success":  false,
			"needauth": false,
			"message":  "",
			"detail":   "Trade partner cancelled the offer",
		})

		err := svc.RespondToConfirmation(t.Context(), conf, true, "dev", testSteamID, "key", 0)
		require.Error(t, err)
		assert.ErrorIs(t, err, ErrConfirmationRejected)
		assert.Contains(t, err.Error(), "Trade partner cancelled the offer")
	})

	t.Run("single_conf_message_priority_when_both_present", func(t *testing.T) {
		t.Parallel()

		mockComm := testmock.NewHTTPStub()
		svc := NewMobileConf(mockComm)

		mockComm.SetJSONResponse("mobileconf/ajaxop", 200, map[string]any{
			"success":  false,
			"needauth": false,
			"message":  "Primary failure message",
			"detail":   "Secondary detail",
		})

		err := svc.RespondToConfirmation(t.Context(), conf, true, "dev", testSteamID, "key", 0)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "Primary failure message")
	})

	t.Run("multi_conf_detail_priority_when_message_empty", func(t *testing.T) {
		t.Parallel()

		mockComm := testmock.NewHTTPStub()
		svc := NewMobileConf(mockComm)

		mockComm.SetJSONResponse("mobileconf/multiajaxop", 200, map[string]any{
			"success":  false,
			"needauth": false,
			"message":  "",
			"detail":   "Items already accepted in another trade",
		})

		err := svc.RespondToMultiple(t.Context(), []*Confirmation{conf}, true, "dev", testSteamID, "key", 0)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "Items already accepted in another trade")
	})

	t.Run("multi_conf_unknown_fallback_when_both_empty", func(t *testing.T) {
		t.Parallel()

		mockComm := testmock.NewHTTPStub()
		svc := NewMobileConf(mockComm)

		mockComm.SetJSONResponse("mobileconf/multiajaxop", 200, map[string]any{
			"success":  false,
			"needauth": false,
			"message":  "",
			"detail":   "",
		})

		err := svc.RespondToMultiple(t.Context(), []*Confirmation{conf}, true, "dev", testSteamID, "key", 0)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "unknown error")
	})
}

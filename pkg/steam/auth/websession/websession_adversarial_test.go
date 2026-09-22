// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package websession

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"sync/atomic"
	"testing"

	log "github.com/lemon4ksan/foundation/async/logkit"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lemon4ksan/g-man/pkg/steam/id"
	pb "github.com/lemon4ksan/g-man/protobuf/steam"
)

// TestAdversarial_TokenRefresherFallback_SteamClient_ZeroFinalizeCalls strictly verifies that
// for SteamClient platforms with an empty access token:
// 1. If tokenRefresher is nil, MissingTokenRefresherError is returned without contacting /jwt/finalizelogin.
// 2. If tokenRefresher succeeds, the refreshed token is used and /jwt/finalizelogin is NEVER contacted.
// 3. If tokenRefresher fails, the error is returned and /jwt/finalizelogin is NEVER contacted.
// 4. If tokenRefresher returns an empty string, an error is returned and /jwt/finalizelogin is NEVER contacted.
func TestAdversarial_TokenRefresherFallback_SteamClient_ZeroFinalizeCalls(t *testing.T) {
	t.Parallel()

	platform := pb.EAuthTokenPlatformType_k_EAuthTokenPlatformType_SteamClient

	t.Run("nil_refresher_returns_MissingTokenRefresherError_zero_finalize_calls", func(t *testing.T) {
		t.Parallel()
		ctx := t.Context()

		var finalizeCalls atomic.Int32

		mt := setupMockTransport()
		mt.handlers[urlFinalize] = func(r *http.Request) (*http.Response, error) {
			finalizeCalls.Add(1)
			return nil, errors.New("FATAL: /jwt/finalizelogin should never be contacted for SteamClient")
		}

		ws := newMockedSession(mt)
		// Refresher is nil
		err := ws.Authenticate(ctx, platform, "valid_refresh_token", "")

		require.Error(t, err)
		assert.ErrorIs(t, err, ErrMissingTokenRefresher)

		var missingErr *MissingTokenRefresherError
		require.ErrorAs(t, err, &missingErr)
		assert.Equal(t, platform, missingErr.Platform)

		assert.Equal(t, int32(0), finalizeCalls.Load(), "/jwt/finalizelogin must NEVER be contacted")
		assert.False(t, ws.IsAuthenticated())
	})

	t.Run("valid_refresher_invokes_CM_socket_and_never_contacts_finalize", func(t *testing.T) {
		t.Parallel()
		ctx := t.Context()

		var (
			finalizeCalls  atomic.Int32
			refresherCalls atomic.Int32
		)

		mt := setupMockTransport()
		mt.handlers[urlFinalize] = func(r *http.Request) (*http.Response, error) {
			finalizeCalls.Add(1)
			return nil, errors.New("FATAL: /jwt/finalizelogin should never be contacted for SteamClient")
		}

		ws := newMockedSession(mt)
		ws.WithTokenRefresher(func(c context.Context, refreshToken string) (string, error) {
			refresherCalls.Add(1)
			assert.Equal(t, "valid_refresh_token", refreshToken)
			return "generated_access_token_client_456", nil
		})

		err := ws.Authenticate(ctx, platform, "valid_refresh_token", "")
		require.NoError(t, err)

		assert.Equal(t, int32(1), refresherCalls.Load(), "TokenRefresher must be called exactly once")
		assert.Equal(t, int32(0), finalizeCalls.Load(), "/jwt/finalizelogin must NEVER be contacted")
		assert.True(t, ws.IsAuthenticated())

		// Verify steamLoginSecure cookie contains the generated token
		u, _ := url.Parse("https://steamcommunity.com")
		cookies := ws.Cookies(u)

		var secureCookie *http.Cookie
		for _, c := range cookies {
			if c.Name == cookieSteamLoginSecure {
				secureCookie = c
				break
			}
		}

		require.NotNil(t, secureCookie, "steamLoginSecure cookie must be seeded")
		assert.Contains(t, secureCookie.Value, "generated_access_token_client_456")
	})

	t.Run("refresher_error_propagates_and_never_contacts_finalize", func(t *testing.T) {
		t.Parallel()
		ctx := t.Context()

		var finalizeCalls atomic.Int32

		mt := setupMockTransport()
		mt.handlers[urlFinalize] = func(r *http.Request) (*http.Response, error) {
			finalizeCalls.Add(1)
			return nil, errors.New("FATAL: /jwt/finalizelogin called")
		}

		ws := newMockedSession(mt)
		expectedErr := errors.New("CM socket timeout")
		ws.WithTokenRefresher(func(c context.Context, refreshToken string) (string, error) {
			return "", expectedErr
		})

		err := ws.Authenticate(ctx, platform, "valid_refresh_token", "")
		require.Error(t, err)
		assert.ErrorIs(t, err, expectedErr)
		assert.Equal(t, int32(0), finalizeCalls.Load(), "/jwt/finalizelogin must NEVER be contacted")
		assert.False(t, ws.IsAuthenticated())
	})

	t.Run("refresher_returning_empty_token_fails_safely_without_finalize", func(t *testing.T) {
		t.Parallel()
		ctx := t.Context()

		var finalizeCalls atomic.Int32

		mt := setupMockTransport()
		mt.handlers[urlFinalize] = func(r *http.Request) (*http.Response, error) {
			finalizeCalls.Add(1)
			return nil, errors.New("FATAL: /jwt/finalizelogin called")
		}

		ws := newMockedSession(mt)
		ws.WithTokenRefresher(func(c context.Context, refreshToken string) (string, error) {
			return "", nil // returns empty string without error
		})

		err := ws.Authenticate(ctx, platform, "valid_refresh_token", "")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "token refresher returned empty access token")
		assert.Equal(t, int32(0), finalizeCalls.Load(), "/jwt/finalizelogin must NEVER be contacted")
		assert.False(t, ws.IsAuthenticated())
	})
}

// TestAdversarial_TokenRefresherFallback_MobileApp_ZeroFinalizeCalls strictly verifies the same
// invariants for MobileApp platform tokens.
func TestAdversarial_TokenRefresherFallback_MobileApp_ZeroFinalizeCalls(t *testing.T) {
	t.Parallel()

	platform := pb.EAuthTokenPlatformType_k_EAuthTokenPlatformType_MobileApp

	t.Run("nil_refresher_returns_MissingTokenRefresherError_zero_finalize_calls", func(t *testing.T) {
		t.Parallel()
		ctx := t.Context()

		var finalizeCalls atomic.Int32

		mt := setupMockTransport()
		mt.handlers[urlFinalize] = func(r *http.Request) (*http.Response, error) {
			finalizeCalls.Add(1)
			return nil, errors.New("FATAL: /jwt/finalizelogin should never be contacted for MobileApp")
		}

		ws := newMockedSession(mt)
		err := ws.Authenticate(ctx, platform, "valid_refresh_token", "")

		require.Error(t, err)
		assert.ErrorIs(t, err, ErrMissingTokenRefresher)

		var missingErr *MissingTokenRefresherError
		require.ErrorAs(t, err, &missingErr)
		assert.Equal(t, platform, missingErr.Platform)

		assert.Equal(t, int32(0), finalizeCalls.Load())
		assert.False(t, ws.IsAuthenticated())
	})

	t.Run("valid_refresher_invokes_CM_socket_and_never_contacts_finalize", func(t *testing.T) {
		t.Parallel()
		ctx := t.Context()

		var (
			finalizeCalls  atomic.Int32
			refresherCalls atomic.Int32
		)

		mt := setupMockTransport()
		mt.handlers[urlFinalize] = func(r *http.Request) (*http.Response, error) {
			finalizeCalls.Add(1)
			return nil, errors.New("FATAL: /jwt/finalizelogin should never be contacted for MobileApp")
		}

		ws := newMockedSession(mt)
		ws.WithTokenRefresher(func(c context.Context, refreshToken string) (string, error) {
			refresherCalls.Add(1)
			return "generated_access_token_mobile_789", nil
		})

		err := ws.Authenticate(ctx, platform, "valid_refresh_token", "")
		require.NoError(t, err)

		assert.Equal(t, int32(1), refresherCalls.Load())
		assert.Equal(t, int32(0), finalizeCalls.Load())
		assert.True(t, ws.IsAuthenticated())

		u, _ := url.Parse("https://steamcommunity.com")
		cookies := ws.Cookies(u)

		var secureCookie *http.Cookie
		for _, c := range cookies {
			if c.Name == cookieSteamLoginSecure {
				secureCookie = c
				break
			}
		}

		require.NotNil(t, secureCookie)
		assert.Contains(t, secureCookie.Value, "generated_access_token_mobile_789")
	})
}

// TestAdversarial_WebBrowser_ContrastingBehavior confirms that WebBrowser tokens (unlike SteamClient/MobileApp)
// DO execute authSlowPath and attempt /jwt/finalizelogin.
func TestAdversarial_WebBrowser_ContrastingBehavior(t *testing.T) {
	t.Parallel()
	ctx := t.Context()

	var finalizeCalls atomic.Int32

	mt := setupMockTransport()
	mt.handlers[urlFinalize] = func(r *http.Request) (*http.Response, error) {
		finalizeCalls.Add(1)
		return nil, errors.New("simulated finalize network error")
	}

	ws := newMockedSession(mt)
	err := ws.Authenticate(
		ctx,
		pb.EAuthTokenPlatformType_k_EAuthTokenPlatformType_WebBrowser,
		"browser_refresh_token",
		"",
	)
	require.Error(t, err)
	assert.GreaterOrEqual(t, finalizeCalls.Load(), int32(1), "WebBrowser platform MUST contact finalize login")
}

// TestAdversarial_WebSession_Refresh_ZeroFinalizeCalls verifies that background Refresh()
// on SteamClient and MobileApp platforms generates tokens via refresher and NEVER contacts finalize.
func TestAdversarial_WebSession_Refresh_ZeroFinalizeCalls(t *testing.T) {
	t.Parallel()

	platforms := []struct {
		name     string
		platform pb.EAuthTokenPlatformType
	}{
		{name: "SteamClient", platform: pb.EAuthTokenPlatformType_k_EAuthTokenPlatformType_SteamClient},
		{name: "MobileApp", platform: pb.EAuthTokenPlatformType_k_EAuthTokenPlatformType_MobileApp},
	}

	for _, p := range platforms {
		t.Run(p.name, func(t *testing.T) {
			t.Parallel()
			ctx := t.Context()

			var (
				finalizeCalls  atomic.Int32
				refresherCalls atomic.Int32
			)

			mt := setupMockTransport()
			mt.handlers[urlFinalize] = func(r *http.Request) (*http.Response, error) {
				finalizeCalls.Add(1)
				return nil, errors.New("FATAL: /jwt/finalizelogin called during refresh")
			}

			ws := New(id.ID(76561197960265728), log.Discard, mt)
			ws.WithTokenRefresher(func(c context.Context, refreshToken string) (string, error) {
				count := refresherCalls.Add(1)
				return "fresh_token_v" + string(rune('0'+count)), nil
			})

			// Initial authentication with token
			err := ws.Authenticate(ctx, p.platform, "my_refresh_token", "initial_token")
			require.NoError(t, err)
			assert.Equal(t, int32(0), finalizeCalls.Load())

			// Trigger Refresh
			err = ws.Refresh(ctx)
			require.NoError(t, err)

			assert.Equal(t, int32(1), refresherCalls.Load(), "Refresher must be called during Refresh()")
			assert.Equal(t, int32(0), finalizeCalls.Load(), "Finalize must NEVER be called during Refresh()")
			assert.True(t, ws.IsAuthenticated())
		})
	}
}

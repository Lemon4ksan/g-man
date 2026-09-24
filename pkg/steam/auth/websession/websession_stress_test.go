// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package websession

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	log "github.com/lemon4ksan/foundation/async/logkit"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lemon4ksan/g-man/pkg/steam/id"
	pb "github.com/lemon4ksan/g-man/protobuf/steam"
)

// ---------------------------------------------------------------------------
// 1. High-Concurrency WebSession.Refresh() Stress Harness (50 - 100 goroutines)
// ---------------------------------------------------------------------------

// TestStress_Refresh_SingleFlight_100Goroutines verifies that under intense concurrent
// load (100 goroutines), singleflight completely collapses concurrent refreshes into a single execution,
// prevents auth thrashing, incurs 0 data races, and leaves the session correctly authenticated.
func TestStress_Refresh_SingleFlight_100Goroutines(t *testing.T) {
	t.Parallel()

	const concurrentCallers = 100

	var refresherExecutions atomic.Int64

	ws := newMockedSession(setupMockTransport())
	ws.WithTokenRefresher(func(ctx context.Context, refreshToken string) (string, error) {
		refresherExecutions.Add(1)
		// Simulate network latency for CM socket GenerateAccessTokenForApp
		time.Sleep(150 * time.Millisecond)

		return "adv_token_fresh_100", nil
	})

	err := ws.Authenticate(
		t.Context(),
		pb.EAuthTokenPlatformType_k_EAuthTokenPlatformType_SteamClient,
		"adversarial_rt",
		"initial_token",
	)
	require.NoError(t, err)
	require.True(t, ws.IsAuthenticated())

	startBarrier := make(chan struct{})

	var wg sync.WaitGroup
	wg.Add(concurrentCallers)

	errChan := make(chan error, concurrentCallers)

	for i := range concurrentCallers {
		go func(id int) {
			defer wg.Done()

			<-startBarrier

			callCtx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()

			if err := ws.Refresh(callCtx); err != nil {
				errChan <- fmt.Errorf("goroutine %d refresh failed: %w", id, err)
			}
		}(i)
	}

	// Release all 100 goroutines simultaneously
	close(startBarrier)
	wg.Wait()
	close(errChan)

	for err := range errChan {
		require.NoError(t, err)
	}

	// Verify exactly 1 CM socket execution occurred across all 100 goroutines
	assert.Equal(
		t,
		int64(1),
		refresherExecutions.Load(),
		"TokenRefresher must be invoked exactly once via singleflight under 100 concurrent callers",
	)
	assert.True(t, ws.IsAuthenticated())

	// Verify all domains received the fresh token from singleflight
	u, err := url.Parse("https://steamcommunity.com")
	require.NoError(t, err)

	cookies := ws.Cookies(u)
	require.NotEmpty(t, cookies)

	var foundSecure bool
	for _, c := range cookies {
		if c.Name == cookieSteamLoginSecure {
			foundSecure = true

			assert.Contains(t, c.Value, "adv_token_fresh_100")
		}
	}

	assert.True(t, foundSecure, "fresh token must be present in cookie jar")
}

// TestStress_Refresh_MultiWave_SustainedConcurrency verifies multiple successive waves of
// high-concurrency refresh calls (3 waves of 50 goroutines = 150 total) properly reclaim the
// singleflight key after each batch finishes, without memory leaks or race conditions.
func TestStress_Refresh_MultiWave_SustainedConcurrency(t *testing.T) {
	t.Parallel()

	const (
		waves          = 3
		callersPerWave = 50
	)

	var (
		refresherExecutions atomic.Int64
		entered             atomic.Int64
	)

	ws := newMockedSession(setupMockTransport())
	ws.WithTokenRefresher(func(ctx context.Context, refreshToken string) (string, error) {
		idx := refresherExecutions.Add(1)
		target := idx * callersPerWave
		deadline := time.Now().Add(2 * time.Second)

		for entered.Load() < target && time.Now().Before(deadline) {
			time.Sleep(1 * time.Millisecond)
		}

		time.Sleep(20 * time.Millisecond)

		return fmt.Sprintf("wave_token_%d", idx), nil
	})

	err := ws.Authenticate(
		t.Context(),
		pb.EAuthTokenPlatformType_k_EAuthTokenPlatformType_SteamClient,
		"wave_rt",
		"init",
	)
	require.NoError(t, err)

	for wave := 1; wave <= waves; wave++ {
		startBarrier := make(chan struct{})

		var readyWg sync.WaitGroup
		readyWg.Add(callersPerWave)

		var wg sync.WaitGroup
		wg.Add(callersPerWave)
		errs := make([]error, callersPerWave)

		for i := range callersPerWave {
			go func(i int) {
				defer wg.Done()

				readyWg.Done()
				<-startBarrier

				entered.Add(1)

				errs[i] = ws.Refresh(t.Context())
			}(i)
		}

		readyWg.Wait()
		close(startBarrier)
		wg.Wait()

		for _, err := range errs {
			require.NoError(t, err)
		}

		assert.Equal(
			t,
			int64(wave),
			refresherExecutions.Load(),
			"Wave %d must trigger exactly 1 refresher execution",
			wave,
		)
	}
}

// ---------------------------------------------------------------------------
// 2. Cookie Jar Concurrent Reads/Writes & Domain Scoping Stress Harness
// ---------------------------------------------------------------------------

// TestStress_CookieJar_ConcurrentReadWriteStress stresses the cookie jar under
// simultaneous writes (refresh, seed, clear) and reads (Cookies, SessionID, ClientSessionID, Do)
// with 80 concurrent goroutines over 500ms to guarantee zero race conditions or panics.
func TestStress_CookieJar_ConcurrentReadWriteStress(t *testing.T) {
	t.Parallel()

	ws := newMockedSession(setupMockTransport())
	ws.WithTokenRefresher(func(ctx context.Context, refreshToken string) (string, error) {
		return "stress_token", nil
	})

	err := ws.Authenticate(
		t.Context(),
		pb.EAuthTokenPlatformType_k_EAuthTokenPlatformType_SteamClient,
		"stress_rt",
		"stress_at",
	)
	require.NoError(t, err)

	stopCtx, cancel := context.WithTimeout(t.Context(), 500*time.Millisecond)
	defer cancel()

	var wg sync.WaitGroup

	testDomains := []string{
		"https://steamcommunity.com/tradeoffer/1",
		"https://store.steampowered.com/app/440",
		"https://help.steampowered.com/en/",
		"https://login.steampowered.com/jwt/finalizelogin",
		"https://s.team/chat",
		"https://api.steamcommunity.com/ISteamUser/v1",
	}

	// 20 Reader Goroutines for Cookies
	for i := range 20 {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()

			u, _ := url.Parse(testDomains[idx%len(testDomains)])
			for stopCtx.Err() == nil {
				_ = ws.Cookies(u)
			}
		}(i)
	}

	// 20 Reader Goroutines for SessionID and ClientSessionID
	for i := range 20 {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()

			domain := testDomains[idx%len(testDomains)]
			for stopCtx.Err() == nil {
				_ = ws.SessionID(domain)
				_ = ws.ClientSessionID(domain)
			}
		}(i)
	}

	// 20 Reader Goroutines for IsAuthenticated & HTTP
	for range 20 {
		wg.Add(1)
		go func() {
			defer wg.Done()

			for stopCtx.Err() == nil {
				_ = ws.IsAuthenticated()
				_ = ws.HTTP()
			}
		}()
	}

	// 10 Writer Goroutines calling Refresh
	for range 10 {
		wg.Add(1)
		go func() {
			defer wg.Done()

			for stopCtx.Err() == nil {
				_ = ws.Refresh(stopCtx)

				time.Sleep(5 * time.Millisecond)
			}
		}()
	}

	// 5 Writer Goroutines calling AddDomains
	for i := range 5 {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()

			for stopCtx.Err() == nil {
				ws.AddDomains(fmt.Sprintf("https://domain%d.steampowered.com", idx))
				time.Sleep(10 * time.Millisecond)
			}
		}(i)
	}

	// 5 Goroutines calling Clear & re-Authenticate
	for range 5 {
		wg.Add(1)
		go func() {
			defer wg.Done()

			for stopCtx.Err() == nil {
				ws.Clear()
				_ = ws.Authenticate(
					stopCtx,
					pb.EAuthTokenPlatformType_k_EAuthTokenPlatformType_SteamClient,
					"stress_rt",
					"stress_at",
				)

				time.Sleep(15 * time.Millisecond)
			}
		}()
	}

	wg.Wait()
}

// TestStress_CookieJar_DomainScopedIsolation tests strict domain isolation and wildcard matching.
// Cookies set on Steam domains must match valid subdomains, but MUST NOT leak to lookalike domains or third parties.
func TestStress_CookieJar_DomainScopedIsolation(t *testing.T) {
	t.Parallel()

	ws := newMockedSession(setupMockTransport())
	err := ws.Authenticate(
		t.Context(),
		pb.EAuthTokenPlatformType_k_EAuthTokenPlatformType_SteamClient,
		"rt",
		"secure_token_xyz",
	)
	require.NoError(t, err)

	testCases := []struct {
		url         string
		shouldMatch bool
		description string
	}{
		// Valid Steam domains & subdomains
		{"https://steamcommunity.com/tradeoffer/1", true, "exact steamcommunity.com"},
		{
			"https://api.steamcommunity.com/ISteamUser/GetPlayerSummaries/v0002/",
			true,
			"subdomain api.steamcommunity.com",
		},
		{"https://broadcast.steamcommunity.com/watch", true, "subdomain broadcast.steamcommunity.com"},
		{"https://store.steampowered.com/checkout", true, "exact store.steampowered.com"},
		{"https://checkout.steampowered.com/cart", true, "subdomain checkout.steampowered.com"},
		{"https://help.steampowered.com/wizard/HelpWithGame", true, "exact help.steampowered.com"},
		{"https://login.steampowered.com/jwt/finalizelogin", true, "exact login.steampowered.com"},
		{"https://s.team/chat/123", true, "exact s.team"},
		{"https://sub.s.team/invite", true, "subdomain sub.s.team"},

		// Insecure scheme (cookies have Secure: true)
		{"http://steamcommunity.com/", false, "insecure http:// scheme"},
		{"http://store.steampowered.com/", false, "insecure http:// scheme for store"},

		// Host spoofing & lookalike domains (RFC 6265 attacks)
		{"https://evilsteampowered.com/", false, "lookalike evilsteampowered.com"},
		{"https://not-steampowered.com/", false, "lookalike not-steampowered.com"},
		{"https://evil-steamcommunity.com/", false, "lookalike evil-steamcommunity.com"},
		{"https://steamcommunity.com.attacker.com/", false, "attacker subdomain suffix attack"},
		{"https://steampowered.com.evil.org/", false, "attacker evil.org suffix attack"},
		{"https://s.team.attacker.com/", false, "attacker s.team subdomain suffix attack"},
		{"https://fake-s.team/", false, "fake-s.team lookalike"},
		{"https://steampowered.com:8443/", true, "custom port on steampowered.com"},

		// Unrelated external domains
		{"https://google.com/", false, "google.com"},
		{"https://github.com/", false, "github.com"},
		{"https://example.com/", false, "example.com"},
	}

	for _, tc := range testCases {
		t.Run(tc.description, func(t *testing.T) {
			t.Parallel()

			u, err := url.Parse(tc.url)
			require.NoError(t, err)

			cookies := ws.Cookies(u)
			if tc.shouldMatch {
				assert.NotEmpty(t, cookies, "expected cookies for %s", tc.url)

				names := make(map[string]bool)
				for _, c := range cookies {
					names[c.Name] = true
				}

				assert.True(t, names[cookieSessionID], "sessionid missing for %s", tc.url)
				assert.True(t, names[cookieClientSessionID], "clientsessionid missing for %s", tc.url)
				assert.True(t, names[cookieSteamLoginSecure], "steamLoginSecure missing for %s", tc.url)
			} else {
				assert.Empty(t, cookies, "SECURITY LEAK: cookies leaked to %s", tc.url)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// 3. Edge Cases: Empty Tokens, Malformed Tokens, Cancelled Contexts
// ---------------------------------------------------------------------------

// TestStress_EdgeCases_EmptyAndMissingTokens tests boundary token validation.
func TestStress_EdgeCases_EmptyAndMissingTokens(t *testing.T) {
	t.Parallel()

	t.Run("empty_refresh_token_Authenticate", func(t *testing.T) {
		t.Parallel()

		ws := newMockedSession(setupMockTransport())
		err := ws.Authenticate(t.Context(), pb.EAuthTokenPlatformType_k_EAuthTokenPlatformType_SteamClient, "", "at")
		require.ErrorIs(t, err, ErrRefreshTokenRequired)
	})

	t.Run("empty_refresh_token_Refresh_unauthenticated", func(t *testing.T) {
		t.Parallel()

		ws := newMockedSession(setupMockTransport())
		err := ws.Refresh(t.Context())
		require.ErrorIs(t, err, ErrRefreshTokenRequired)
	})

	t.Run("nil_token_refresher_for_SteamClient", func(t *testing.T) {
		t.Parallel()

		ws := newMockedSession(setupMockTransport())
		err := ws.Authenticate(
			t.Context(),
			pb.EAuthTokenPlatformType_k_EAuthTokenPlatformType_SteamClient,
			"valid_rt",
			"",
		)
		require.Error(t, err)
		assert.True(t, errors.Is(err, ErrMissingTokenRefresher))

		var missingErr *MissingTokenRefresherError
		assert.True(t, errors.As(err, &missingErr))
		assert.Equal(t, pb.EAuthTokenPlatformType_k_EAuthTokenPlatformType_SteamClient, missingErr.Platform)
	})

	t.Run("nil_token_refresher_for_MobileApp", func(t *testing.T) {
		t.Parallel()

		ws := newMockedSession(setupMockTransport())
		err := ws.Authenticate(
			t.Context(),
			pb.EAuthTokenPlatformType_k_EAuthTokenPlatformType_MobileApp,
			"valid_rt",
			"",
		)
		require.Error(t, err)
		assert.True(t, errors.Is(err, ErrMissingTokenRefresher))
	})

	t.Run("token_refresher_returns_empty_token", func(t *testing.T) {
		t.Parallel()

		ws := newMockedSession(setupMockTransport())
		ws.WithTokenRefresher(func(ctx context.Context, refreshToken string) (string, error) {
			return "", nil // returns empty string without error
		})
		err := ws.Authenticate(
			t.Context(),
			pb.EAuthTokenPlatformType_k_EAuthTokenPlatformType_SteamClient,
			"valid_rt",
			"",
		)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "token refresher returned empty access token")
		assert.False(t, ws.IsAuthenticated())
	})

	t.Run("token_refresher_returns_error", func(t *testing.T) {
		t.Parallel()

		ws := newMockedSession(setupMockTransport())
		refresherErr := errors.New("cm socket rpc failure: connection reset by peer")
		ws.WithTokenRefresher(func(ctx context.Context, refreshToken string) (string, error) {
			return "", refresherErr
		})
		err := ws.Authenticate(
			t.Context(),
			pb.EAuthTokenPlatformType_k_EAuthTokenPlatformType_SteamClient,
			"valid_rt",
			"",
		)
		require.Error(t, err)
		assert.ErrorIs(t, err, refresherErr)
		assert.False(t, ws.IsAuthenticated())
	})
}

// TestStress_EdgeCases_MalformedTokens verifies formatting resilience when access tokens
// contain exotic, non-ASCII, UTF-8, pipe delimiters, or delimiter-injection attempts.
func TestStress_EdgeCases_MalformedTokens(t *testing.T) {
	t.Parallel()

	adversarialTokens := []struct {
		name      string
		token     string
		steamID   id.ID
		mustMatch func(t *testing.T, formatted string)
	}{
		{
			name:    "pipe_injection_attack",
			token:   "malicious||76561197960265729||injected",
			steamID: id.ID(76561197960265728),
			mustMatch: func(t *testing.T, formatted string) {
				// The pipes inside the token MUST be encoded as %7C%7C so the primary delimiter 76561197960265728%7C%7C is unambiguous
				assert.Equal(t, "76561197960265728%7C%7Cmalicious%7C%7C76561197960265729%7C%7Cinjected", formatted)
			},
		},
		{
			name:    "utf8_and_emoji",
			token:   "token_with_emoji_🚀_and_cyrillic_тест",
			steamID: id.ID(76561197960265728),
			mustMatch: func(t *testing.T, formatted string) {
				// UTF-8 bytes must be percent-encoded, preserving strict ASCII for HTTP headers
				assert.NotContains(t, formatted, "🚀")
				assert.NotContains(t, formatted, "тест")
				assert.Contains(t, formatted, "%F0%9F%9A%80")
			},
		},
		{
			name:    "newlines_and_carriage_returns",
			token:   "token\r\nSet-Cookie: evil=1\r\n",
			steamID: id.ID(76561197960265728),
			mustMatch: func(t *testing.T, formatted string) {
				// \r and \n MUST be escaped to prevent HTTP Response Splitting / Header Injection
				assert.NotContains(t, formatted, "\r")
				assert.NotContains(t, formatted, "\n")
				assert.Contains(t, formatted, "%0D%0A")
			},
		},
		{
			name:    "spaces_and_quotes",
			token:   `token with spaces and "quotes" and <tags>`,
			steamID: id.ID(76561197960265728),
			mustMatch: func(t *testing.T, formatted string) {
				assert.NotContains(t, formatted, " ")
				assert.NotContains(t, formatted, `"`)
				assert.NotContains(t, formatted, `<`)
				assert.NotContains(t, formatted, `>`)
				assert.Contains(t, formatted, "%20")
				assert.Contains(t, formatted, "%22")
			},
		},
		{
			name:    "zero_steam_id",
			token:   "valid_jwt_token",
			steamID: id.ID(0),
			mustMatch: func(t *testing.T, formatted string) {
				assert.Equal(t, "0%7C%7Cvalid_jwt_token", formatted)
			},
		},
	}

	for _, tc := range adversarialTokens {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			formatted := FormatSteamLoginSecure(tc.steamID, tc.token)
			tc.mustMatch(t, formatted)

			// Verify that the cookie jar successfully accepts the encoded cookie without errors
			ws := New(tc.steamID, log.Discard, nil)
			ws.WithTokenRefresher(func(ctx context.Context, refreshToken string) (string, error) {
				return tc.token, nil
			})

			err := ws.Authenticate(
				t.Context(),
				pb.EAuthTokenPlatformType_k_EAuthTokenPlatformType_SteamClient,
				"rt",
				tc.token,
			)
			require.NoError(t, err)

			u, err := url.Parse("https://steamcommunity.com")
			require.NoError(t, err)

			cookies := ws.Cookies(u)
			require.NotEmpty(t, cookies)

			var foundSecure bool
			for _, c := range cookies {
				if c.Name == cookieSteamLoginSecure {
					foundSecure = true

					assert.Equal(t, formatted, c.Value)
				}
			}

			assert.True(t, foundSecure)
		})
	}
}

// TestStress_EdgeCases_ContextCancellationDuringRefresh tests that when a context is
// cancelled before or during Refresh, the system behaves safely: it returns the context error,
// never deadlocks or leaks singleflight state, and allows subsequent calls to succeed.
func TestStress_EdgeCases_ContextCancellationDuringRefresh(t *testing.T) {
	t.Parallel()

	t.Run("already_cancelled_context_fails_fast", func(t *testing.T) {
		t.Parallel()

		ws := newMockedSession(setupMockTransport())
		cancelledCtx, cancel := context.WithCancel(t.Context())
		cancel()

		err := ws.Refresh(cancelledCtx)
		require.ErrorIs(t, err, context.Canceled)
	})

	t.Run("context_cancelled_during_refresher_execution", func(t *testing.T) {
		t.Parallel()

		refresherStarted := make(chan struct{})
		ws := newMockedSession(setupMockTransport())
		ws.WithTokenRefresher(func(ctx context.Context, refreshToken string) (string, error) {
			close(refresherStarted)
			// Wait until context is cancelled
			<-ctx.Done()

			return "", ctx.Err()
		})

		err := ws.Authenticate(
			t.Context(),
			pb.EAuthTokenPlatformType_k_EAuthTokenPlatformType_SteamClient,
			"cancel_rt",
			"initial_token",
		)
		require.NoError(t, err)

		cancelCtx, cancel := context.WithCancel(t.Context())
		errChan := make(chan error, 1)

		go func() {
			errChan <- ws.Refresh(cancelCtx)
		}()

		// Wait for refresher to begin execution
		<-refresherStarted
		// Cancel context mid-flight
		cancel()

		err = <-errChan
		require.ErrorIs(t, err, context.Canceled)

		// CRUCIAL: SingleFlight key MUST be cleared so the subsequent refresh succeeds!
		ws.WithTokenRefresher(func(ctx context.Context, refreshToken string) (string, error) {
			return "recovered_token", nil
		})

		freshCtx := t.Context()
		err = ws.Refresh(freshCtx)
		require.NoError(t, err, "subsequent refresh must succeed after prior cancelled refresh was reclaimed")
		assert.True(t, ws.IsAuthenticated())
	})
}

// TestStress_SoftLogout_Patterns verifies SIMD/byte searches in isSoftLogoutBody
// for all variations of Steam logout indicators.
func TestStress_SoftLogout_Patterns(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		body     string
		expected bool
	}{
		{"empty_body", "", false},
		{"normal_json", `{"success": true, "items": []}`, false},
		{"normal_html", `<html><head><title>Team Fortress 2</title></head><body><h1>Welcome</h1></body></html>`, false},
		{"steam_id_false", `var g_steamID = false;`, true},
		{"steam_id_zero", `var g_steamID = "0";`, true},
		{"sign_in_title", `<html><head><title>Sign In</title></head><body>login form</body></html>`, true},
		{"logged_in_false_compact", `{"success":false,"Logged In":false}`, true},
		{"logged_in_false_space", `{"success":false,"Logged In": false}`, true},
		{"lower_logged_in_false_compact", `{"success":false,"logged in":false}`, true},
		{"lower_logged_in_false_space", `{"success":false,"logged in": false}`, true},
		{
			"embedded_in_large_payload",
			strings.Repeat("A", 1000) + `"Logged In": false` + strings.Repeat("B", 1000),
			true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			result := isSoftLogoutBody([]byte(tt.body))
			assert.Equal(t, tt.expected, result)
		})
	}
}

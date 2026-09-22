// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package client_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lemon4ksan/g-man/pkg/steam/community/client"
	"github.com/lemon4ksan/g-man/pkg/steam/service"
	"github.com/lemon4ksan/g-man/pkg/test/mock"
)

// Adversarial mock session tracking Refresh calls.
type trackingRefresherSession struct {
	mockSession
	refreshCount atomic.Int32
	refreshDelay time.Duration
	refreshErr   error
}

func (m *trackingRefresherSession) Refresh(ctx context.Context) error {
	m.refreshCount.Add(1)

	if m.refreshDelay > 0 {
		select {
		case <-time.After(m.refreshDelay):
		case <-ctx.Done():
			return ctx.Err()
		}
	}

	return m.refreshErr
}

// TestAdversarial_SoftLogout_302Redirects verifies soft logout detection and re-auth
// across varying 302/303 redirect targets to /login/home and /login with different query parameters.
func TestAdversarial_SoftLogout_302Redirects(t *testing.T) {
	t.Parallel()

	redirectLocations := []struct {
		name       string
		statusCode int
		location   string
	}{
		{name: "relative_login_home", statusCode: http.StatusFound, location: "/login/home"},
		{name: "relative_login_home_trailing_slash", statusCode: http.StatusFound, location: "/login/home/"},
		{
			name:       "relative_login_home_with_goto",
			statusCode: http.StatusFound,
			location:   "/login/home/?goto=%2Ftradeoffers",
		},
		{
			name:       "relative_login_home_complex_query",
			statusCode: http.StatusFound,
			location:   "/login/home/?goto=%2Ftradeoffers%2F1%3Fpartner%3D123&session=expired",
		},
		{name: "absolute_login_home", statusCode: http.StatusFound, location: "https://steamcommunity.com/login/home"},
		{
			name:       "absolute_login_home_trailing_slash",
			statusCode: http.StatusFound,
			location:   "https://steamcommunity.com/login/home/",
		},
		{
			name:       "absolute_login_home_with_goto",
			statusCode: http.StatusFound,
			location:   "https://steamcommunity.com/login/home/?goto=%2Ftradeoffers",
		},
		{name: "relative_login", statusCode: http.StatusFound, location: "/login"},
		{name: "relative_login_with_query", statusCode: http.StatusFound, location: "/login?oauth=1&goto=%2F"},
		{name: "absolute_login", statusCode: http.StatusFound, location: "https://steamcommunity.com/login"},
		{
			name:       "see_other_login_home",
			statusCode: http.StatusSeeOther,
			location:   "https://steamcommunity.com/login/home/?goto=%2F",
		},
	}

	for _, tc := range redirectLocations {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ctx := t.Context()

			sess := &trackingRefresherSession{}

			var callCount atomic.Int32

			mockSvc := mock.NewServiceMock()
			mockSvc.OnRest = func(method, path string, body any) (*http.Response, error) {
				step := callCount.Add(1)
				if step == 1 {
					// First request simulates soft logout redirect
					return &http.Response{
						StatusCode: tc.statusCode,
						Header:     http.Header{"Location": []string{tc.location}},
						Body:       io.NopCloser(strings.NewReader("")),
					}, nil
				}

				// Subsequent retried request must succeed
				return &http.Response{
					StatusCode: http.StatusOK,
					Header:     http.Header{"Content-Type": []string{"application/json"}},
					Body:       io.NopCloser(strings.NewReader(`{"success":true,"offer_count":5}`)),
				}, nil
			}

			c := client.New(nil, sess).WithREST(mockSvc)
			resp, err := c.Request(ctx, http.MethodGet, "tradeoffers")
			require.NoError(t, err, "Request should succeed after auto-refresh without loop error")

			require.NotNil(t, resp)
			defer resp.Body.Close()

			bodyBytes, err := io.ReadAll(resp.Body)
			require.NoError(t, err)
			assert.Contains(t, string(bodyBytes), `"success":true`)

			assert.Equal(t, int32(1), sess.refreshCount.Load(), "Refresh should be triggered exactly once")
			assert.Equal(t, int32(2), callCount.Load(), "HTTP request should be called exactly twice (initial + retry)")
		})
	}
}

// TestAdversarial_SoftLogout_HTMLAndJSONForms tests detection of HTML login markers and JSON logged-in states.
func TestAdversarial_SoftLogout_HTMLAndJSONForms(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name        string
		contentType string
		body        string
	}{
		{
			name:        "title_sign_in",
			contentType: "text/html; charset=utf-8",
			body:        `<!DOCTYPE html><html><head><title>Sign In</title></head><body>Please log in</body></html>`,
		},
		{
			name:        "g_steamID_false",
			contentType: "text/html; charset=utf-8",
			body:        `<html><head><script>var g_steamID = false; var g_sessionID = "xyz";</script></head><body>Community</body></html>`,
		},
		{
			name:        "g_steamID_zero_string",
			contentType: "text/html; charset=utf-8",
			body:        `<html><head><script>var g_steamID = "0";</script></head><body>Community</body></html>`,
		},
		{
			name:        "json_Logged_In_false_compact",
			contentType: "application/json",
			body:        `{"success":false,"Logged In":false}`,
		},
		{
			name:        "json_Logged_In_false_spaced",
			contentType: "application/json",
			body:        `{"success":false,"Logged In": false}`,
		},
		{
			name:        "json_lower_logged_in_false_compact",
			contentType: "application/json",
			body:        `{"success":false,"logged in":false}`,
		},
		{
			name:        "json_lower_logged_in_false_spaced",
			contentType: "application/json",
			body:        `{"success":false,"logged in": false}`,
		},
		{
			name:        "json_with_additional_fields",
			contentType: "application/json",
			body:        `{"status":401,"error":"session expired","Logged In": false,"tradeoffers":[]}`,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ctx := t.Context()

			sess := &trackingRefresherSession{}

			var callCount atomic.Int32

			mockSvc := mock.NewServiceMock()
			mockSvc.OnRest = func(method, path string, body any) (*http.Response, error) {
				step := callCount.Add(1)
				if step == 1 {
					return &http.Response{
						StatusCode: http.StatusOK,
						Header:     http.Header{"Content-Type": []string{tc.contentType}},
						Body:       io.NopCloser(strings.NewReader(tc.body)),
					}, nil
				}

				return &http.Response{
					StatusCode: http.StatusOK,
					Header:     http.Header{"Content-Type": []string{"application/json"}},
					Body:       io.NopCloser(strings.NewReader(`{"success":true,"recovered":true}`)),
				}, nil
			}

			c := client.New(nil, sess).WithREST(mockSvc)
			resp, err := c.Request(ctx, http.MethodGet, "tradeoffers")
			require.NoError(t, err, "Request should succeed after auto-refresh without loop error")

			require.NotNil(t, resp)
			defer resp.Body.Close()

			bodyBytes, err := io.ReadAll(resp.Body)
			require.NoError(t, err)
			assert.Contains(t, string(bodyBytes), `"recovered":true`)

			assert.Equal(t, int32(1), sess.refreshCount.Load(), "Refresh should be triggered exactly once")
			assert.Equal(t, int32(2), callCount.Load(), "HTTP request should be called exactly twice")
		})
	}
}

// TestAdversarial_SoftLogout_ConsecutiveFailuresNoInfiniteLoop verifies that if re-auth
// succeeds but the retried request ALSO returns a soft logout page, the client terminates
// cleanly and does NOT enter an infinite retry loop.
func TestAdversarial_SoftLogout_ConsecutiveFailuresNoInfiniteLoop(t *testing.T) {
	t.Parallel()
	ctx := t.Context()

	sess := &trackingRefresherSession{}

	var callCount atomic.Int32

	mockSvc := mock.NewServiceMock()
	mockSvc.OnRest = func(method, path string, body any) (*http.Response, error) {
		callCount.Add(1)
		// Always return soft logout page
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"text/html"}},
			Body:       io.NopCloser(strings.NewReader("<title>Sign In</title>")),
		}, nil
	}

	c := client.New(nil, sess).WithREST(mockSvc)
	resp, err := c.Request(ctx, http.MethodGet, "tradeoffers")
	require.Error(t, err, "Should fail when retried request is also expired")
	assert.Nil(t, resp)
	assert.ErrorIs(t, err, service.ErrSessionExpired)

	// Refresh should have been attempted exactly once
	assert.Equal(t, int32(1), sess.refreshCount.Load())
	// Exactly two requests: initial + one retry
	assert.Equal(t, int32(2), callCount.Load(), "Must not loop infinitely on consecutive soft logouts")
}

// TestAdversarial_SoftLogout_RefreshErrorPreservation verifies that underlying refresh errors
// (e.g. CM socket disconnect, expired refresh token) are preserved and never masked by ErrRedirectLoop.
func TestAdversarial_SoftLogout_RefreshErrorPreservation(t *testing.T) {
	t.Parallel()

	expectedErr := errors.New("socket auth failed: invalid token signature")
	sess := &trackingRefresherSession{
		refreshErr: expectedErr,
	}

	var callCount atomic.Int32

	mockSvc := mock.NewServiceMock()
	mockSvc.OnRest = func(method, path string, body any) (*http.Response, error) {
		callCount.Add(1)

		return &http.Response{
			StatusCode: http.StatusFound,
			Header:     http.Header{"Location": []string{"/login/home"}},
			Body:       io.NopCloser(strings.NewReader("")),
		}, nil
	}

	c := client.New(nil, sess).WithREST(mockSvc)
	resp, err := c.Request(t.Context(), http.MethodGet, "tradeoffers")
	require.Error(t, err)
	assert.Nil(t, resp)
	assert.ErrorContains(t, err, "invalid token signature")
	assert.NotErrorIs(t, err, client.ErrRedirectLoop)
	assert.Equal(t, int32(1), sess.refreshCount.Load())
	assert.Equal(t, int32(1), callCount.Load(), "Should not retry if refresh failed")
}

// TestAdversarial_SoftLogout_NoRefresherSession verifies behavior when session does not implement Refresher.
func TestAdversarial_SoftLogout_NoRefresherSession(t *testing.T) {
	t.Parallel()

	nonRefresherSession := &mockSession{} // Only implements SessionProvider, not Refresher
	mockSvc := mock.NewServiceMock()
	mockSvc.OnRest = func(method, path string, body any) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusFound,
			Header:     http.Header{"Location": []string{"/login/home"}},
			Body:       io.NopCloser(strings.NewReader("")),
		}, nil
	}

	c := client.New(nil, nonRefresherSession).WithREST(mockSvc)
	resp, err := c.Request(t.Context(), http.MethodGet, "tradeoffers")
	require.Error(t, err)
	assert.Nil(t, resp)
	assert.ErrorIs(t, err, client.ErrRedirectLoop, "Non-refresher session should yield ErrRedirectLoop")
}

// TestAdversarial_SoftLogout_ConcurrentRequests verifies thread-safety and behavior
// when multiple concurrent community requests encounter soft logouts simultaneously.
func TestAdversarial_SoftLogout_ConcurrentRequests(t *testing.T) {
	t.Parallel()
	ctx := t.Context()

	sess := &trackingRefresherSession{
		refreshDelay: 10 * time.Millisecond,
	}

	var (
		totalCalls atomic.Int32
		mu         sync.Mutex
	)

	endpointState := make(map[string]int) // counts per path

	mockSvc := mock.NewServiceMock()
	mockSvc.OnRest = func(method, path string, body any) (*http.Response, error) {
		totalCalls.Add(1)
		mu.Lock()
		endpointState[path]++
		count := endpointState[path]
		mu.Unlock()

		if count == 1 {
			// First call for this path returns soft logout
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"application/json"}},
				Body:       io.NopCloser(strings.NewReader(`{"success":false,"Logged In":false}`)),
			}, nil
		}

		// Subsequent call succeeds
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(fmt.Sprintf(`{"success":true,"path":%q}`, path))),
		}, nil
	}

	c := client.New(nil, sess).WithREST(mockSvc)

	concurrency := 8

	var wg sync.WaitGroup
	wg.Add(concurrency)

	for i := range concurrency {
		path := fmt.Sprintf("req-%d", i)
		go func() {
			defer wg.Done()

			resp, err := c.Request(ctx, http.MethodGet, path)
			assert.NoError(t, err)

			if resp != nil {
				_ = resp.Body.Close()
			}
		}()
	}

	wg.Wait()
	assert.GreaterOrEqual(t, sess.refreshCount.Load(), int32(1))
}

// TestAdversarial_SoftLogout_LargeHTMLBodyInspection tests detection limits when HTML is large.
// Confirms that markers placed within the 4096-byte peek window are reliably detected.
func TestAdversarial_SoftLogout_LargeHTMLBodyInspection(t *testing.T) {
	t.Parallel()
	ctx := t.Context()

	// Place g_steamID = false; at offset 2000 (well inside the 4096-byte peek buffer)
	padding := strings.Repeat("<!-- comment -->\n", 120) // ~2040 bytes
	htmlBody := padding + "<script>var g_steamID = false;</script>" + strings.Repeat("<p>More content</p>", 500)

	sess := &trackingRefresherSession{}

	var callCount atomic.Int32

	mockSvc := mock.NewServiceMock()
	mockSvc.OnRest = func(method, path string, body any) (*http.Response, error) {
		if callCount.Add(1) == 1 {
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"text/html; charset=utf-8"}},
				Body:       io.NopCloser(strings.NewReader(htmlBody)),
			}, nil
		}

		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(`{"success":true}`)),
		}, nil
	}

	c := client.New(nil, sess).WithREST(mockSvc)
	resp, err := c.Request(ctx, http.MethodGet, "tradeoffers")
	require.NoError(t, err)

	defer resp.Body.Close()

	assert.Equal(t, int32(1), sess.refreshCount.Load(), "Should detect soft logout in large HTML within peek window")
	assert.Equal(t, int32(2), callCount.Load())
}

// TestAdversarial_CheckSteamErrors_DirectTableMatrix tests CheckSteamErrors directly against
// a rigorous matrix of boundary conditions.
func TestAdversarial_CheckSteamErrors_DirectTableMatrix(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name       string
		statusCode int
		header     http.Header
		body       []byte
		wantExp    bool
	}{
		{
			name:       "302_redirect_login_home",
			statusCode: http.StatusFound,
			header:     http.Header{"Location": []string{"/login/home"}},
			body:       nil,
			wantExp:    true,
		},
		{
			name:       "302_redirect_other_page",
			statusCode: http.StatusFound,
			header:     http.Header{"Location": []string{"/profiles/76561198000000000"}},
			body:       nil,
			wantExp:    false,
		},
		{
			name:       "200_body_g_steamID_false",
			statusCode: http.StatusOK,
			header:     http.Header{},
			body:       []byte("var g_steamID = false;"),
			wantExp:    true,
		},
		{
			name:       "200_body_g_steamID_zero",
			statusCode: http.StatusOK,
			header:     http.Header{},
			body:       []byte(`var g_steamID = "0";`),
			wantExp:    true,
		},
		{
			name:       "200_body_sign_in_title",
			statusCode: http.StatusOK,
			header:     http.Header{},
			body:       []byte("<title>Sign In</title>"),
			wantExp:    true,
		},
		{
			name:       "200_body_logged_in_false",
			statusCode: http.StatusOK,
			header:     http.Header{},
			body:       []byte(`{"Logged In":false}`),
			wantExp:    true,
		},
		{
			name:       "401_unauthorized",
			statusCode: http.StatusUnauthorized,
			header:     http.Header{},
			body:       []byte("Unauthorized"),
			wantExp:    true,
		},
		{
			name:       "403_forbidden",
			statusCode: http.StatusForbidden,
			header:     http.Header{},
			body:       []byte("Access Denied"),
			wantExp:    true,
		},
		{
			name:       "200_valid_page",
			statusCode: http.StatusOK,
			header:     http.Header{},
			body:       []byte(`var g_steamID = "76561198012345678";`),
			wantExp:    false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			err := client.CheckSteamErrors(tc.statusCode, tc.header, tc.body)
			if tc.wantExp {
				require.Error(t, err)
				assert.True(t, client.IsSessionExpiredError(err), "Error %v must be classified as session expired", err)
			} else if err != nil {
				assert.False(t, client.IsSessionExpiredError(err))
			}
		})
	}
}

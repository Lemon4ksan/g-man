// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package community

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/lemon4ksan/g-man/pkg/log"
	"github.com/lemon4ksan/g-man/pkg/steam/api"
)

type MockHTTPDoer struct {
	OnDo func(req *http.Request) (*http.Response, error)
}

func (m *MockHTTPDoer) Do(req *http.Request) (*http.Response, error) {
	return m.OnDo(req)
}

func setupCommunity(onDo func(req *http.Request) (*http.Response, error)) *Client {
	doer := &MockHTTPDoer{OnDo: onDo}
	sessionFunc := func(uri string) string { return "valid_session_id" }
	return New(doer, sessionFunc, log.Discard)
}

func TestCommunityClient_HeadersInjection(t *testing.T) {
	client := setupCommunity(func(req *http.Request) (*http.Response, error) {
		if req.Header.Get("User-Agent") == "" {
			t.Error("User-Agent was not injected")
		}

		if req.Method == "POST" && req.Header.Get("Origin") == "" {
			t.Error("Origin header missing for POST request")
		}

		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader("ok")),
			Header:     make(http.Header),
		}, nil
	})

	ctx := context.Background()
	_, _ = GetHTML(ctx, client, "inventory")

	_, _ = PostForm[struct{}](ctx, client, "tradeoffer/send", map[string]string{"partner": "123"})
}

func TestCommunityClient_SessionIDInjection(t *testing.T) {
	t.Run("PostForm Injection", func(t *testing.T) {
		client := setupCommunity(func(req *http.Request) (*http.Response, error) {
			body, _ := io.ReadAll(req.Body)
			if !strings.Contains(string(body), "sessionid=valid_session_id") {
				t.Errorf("sessionid missing in form body: %s", string(body))
			}
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(bytes.NewReader(nil)),
			}, nil
		})

		_, _ = PostForm[struct{}](context.Background(), client, "test", nil)
	})

	t.Run("PostJSON Injection", func(t *testing.T) {
		client := setupCommunity(func(req *http.Request) (*http.Response, error) {
			if req.URL.Query().Get("sessionid") != "valid_session_id" {
				t.Error("sessionid missing in query params for JSON post")
			}
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(bytes.NewReader(nil)),
			}, nil
		})

		payload := map[string]string{"foo": "bar"}
		_, _ = PostJSON[struct{}](context.Background(), client, "test", payload)
	})
}

func TestCommunityClient_ErrorParsing(t *testing.T) {
	cases := []struct {
		name          string
		statusCode    int
		body          string
		location      string
		expectedError error
		errorContains string
	}{
		{
			name:          "Rate Limit",
			statusCode:    429,
			expectedError: ErrRateLimit,
		},
		{
			name:          "Not Logged In (Redirect)",
			statusCode:    302,
			location:      "https://steamcommunity.com/login/home",
			expectedError: api.ErrSessionExpired,
		},
		{
			name:          "Not Logged In (HTML State)",
			statusCode:    200,
			body:          "g_steamID = false; ... <title>Sign In</title>",
			expectedError: api.ErrSessionExpired,
		},
		{
			name:          "Family View Restricted",
			statusCode:    403,
			body:          `<div id="parental_notice_instructions">Enter your PIN below to exit Family View.</div>`,
			expectedError: ErrFamilyViewRestricted,
		},
		{
			name:          "Sorry Page with Message",
			statusCode:    200,
			body:          `<h1>Sorry!</h1><h3>The item is no longer available</h3>`,
			errorContains: "steam community error: The item is no longer available",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			client := setupCommunity(func(req *http.Request) (*http.Response, error) {
				header := http.Header{}
				if tc.location != "" {
					header.Set("Location", tc.location)
				}
				return &http.Response{
					StatusCode: tc.statusCode,
					Header:     header,
					Body:       io.NopCloser(strings.NewReader(tc.body)),
				}, nil
			})

			_, err := GetHTML(context.Background(), client, "test")
			if err == nil {
				t.Fatal("expected error, got nil")
			}

			if tc.expectedError != nil && !errors.Is(err, tc.expectedError) {
				t.Errorf("expected error %v, got %v", tc.expectedError, err)
			}

			if tc.errorContains != "" && !strings.Contains(err.Error(), tc.errorContains) {
				t.Errorf("expected error to contain %q, got %q", tc.errorContains, err.Error())
			}
		})
	}
}

func TestCommunityClient_Generics(t *testing.T) {
	type response struct {
		Success bool `json:"success"`
		ID      int  `json:"id"`
	}

	t.Run("Successful CallCommunityGet", func(t *testing.T) {
		client := setupCommunity(func(req *http.Request) (*http.Response, error) {
			if req.URL.Query().Get("item_id") != "42" {
				t.Errorf("param item_id missing or wrong: %s", req.URL.Query().Get("item_id"))
			}
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader(`{"success": true, "id": 100}`)),
			}, nil
		})

		type request struct {
			ItemID int `url:"item_id"`
		}

		res, err := Get[response](context.Background(), client, "test", &request{ItemID: 42})
		if err != nil {
			t.Fatalf("Call failed: %v", err)
		}

		if !res.Success || res.ID != 100 {
			t.Errorf("unexpected response content: %+v", res)
		}
	})

	t.Run("Override Format to VDF", func(t *testing.T) {
		client := setupCommunity(func(req *http.Request) (*http.Response, error) {
			vdfData := `"response" { "success" "1" }`
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader(vdfData)),
			}, nil
		})

		res, err := Get[response](context.Background(), client, "test", nil,
			api.WithFormat(api.FormatVDF),
		)

		if err != nil {
			t.Fatalf("VDF call failed: %v", err)
		}
		if !res.Success {
			t.Error("failed to parse VDF via generic call")
		}
	})
}

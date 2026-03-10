// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package api

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/lemon4ksan/g-man/pkg/log"
	tr "github.com/lemon4ksan/g-man/pkg/steam/transport"
)

type mockLogger struct{ log.Logger }
func (m *mockLogger) Debug(msg string, fields ...log.Field) {}

func setupCommunity(onDo func(req *tr.Request) (*tr.Response, error)) *CommunityClient {
	transport := &MockTransport{OnDo: onDo}
	sessionFunc := func(uri string) string { return "valid_session_id" }
	return NewCommunityClient(transport, sessionFunc, &mockLogger{})
}

func TestCommunityClient_HeadersInjection(t *testing.T) {
	client := setupCommunity(func(req *tr.Request) (*tr.Response, error) {
		if req.Header().Get("User-Agent") == "" {
			t.Error("User-Agent was not injected")
		}

		target := req.Target().(tr.HTTPTarget)
		if target.HTTPMethod() == "POST" && req.Header().Get("Origin") == "" {
			t.Error("Origin header missing for POST request")
		}
		
		return &tr.Response{StatusCode: 200, Body: []byte("ok")}, nil
	})

	ctx := context.Background()
	_, _ = client.Get(ctx, "inventory")
	_, _ = client.PostForm(ctx, "tradeoffer/send", url.Values{"partner": {"123"}})
}

func TestCommunityClient_SessionIDInjection(t *testing.T) {
	t.Run("PostForm Injection", func(t *testing.T) {
		client := setupCommunity(func(req *tr.Request) (*tr.Response, error) {
			body := string(req.Body())
			if !strings.Contains(body, "sessionid=valid_session_id") {
				t.Errorf("sessionid missing in form body: %s", body)
			}
			return &tr.Response{StatusCode: 200}, nil
		})
		_, _ = client.PostForm(context.Background(), "test", nil)
	})

	t.Run("PostJSON Injection", func(t *testing.T) {
		client := setupCommunity(func(req *tr.Request) (*tr.Response, error) {
			if req.Params().Get("sessionid") != "valid_session_id" {
				t.Error("sessionid missing in query params for JSON post")
			}
			return &tr.Response{StatusCode: 200}, nil
		})
		_, _ = client.PostJSON(context.Background(), "test", map[string]string{"a": "b"})
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
			expectedError: ErrNotLoggedIn,
		},
		{
			name:          "Not Logged In (HTML State)",
			statusCode:    200,
			body:          "g_steamID = false; ... <title>Sign In</title>",
			expectedError: ErrNotLoggedIn,
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
		{
			name:          "Trade Error Message",
			statusCode:    200,
			body:          `<div id="error_msg">You cannot trade with this user.</div>`,
			errorContains: "trade error: You cannot trade with this user.",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			client := setupCommunity(func(req *tr.Request) (*tr.Response, error) {
				header := http.Header{}
				if tc.location != "" {
					header.Set("Location", tc.location)
				}
				return &tr.Response{
					StatusCode: tc.statusCode,
					Body:       []byte(tc.body),
					Header:     header,
				}, nil
			})

			_, err := client.Get(context.Background(), "test")
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

func TestCommunityClient_GetJSON(t *testing.T) {
	type response struct {
		Success bool `json:"success"`
	}

	t.Run("Successful Parse", func(t *testing.T) {
		client := setupCommunity(func(req *tr.Request) (*tr.Response, error) {
			return &tr.Response{
				StatusCode: 200,
				Body:       []byte(`{"success": true}`),
			}, nil
		})

		var res response
		err := client.GetJSON(context.Background(), "test", nil, &res)
		if err != nil {
			t.Fatalf("GetJSON failed: %v", err)
		}
		if !res.Success {
			t.Error("expected success true")
		}
	})

	t.Run("Empty Response", func(t *testing.T) {
		client := setupCommunity(func(req *tr.Request) (*tr.Response, error) {
			return &tr.Response{StatusCode: 200, Body: []byte("")}, nil
		})
		err := client.GetJSON(context.Background(), "test", nil, &struct{}{})
		if err == nil || !strings.Contains(err.Error(), "empty response") {
			t.Errorf("expected empty response error, got %v", err)
		}
	})
}

// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package client

import (
	"net/http"
	"strings"
	"testing"
)

func TestTruncateBody(t *testing.T) {
	tests := []struct {
		name     string
		body     []byte
		maxLen   int
		expected string
	}{
		{
			name:     "short body not truncated",
			body:     []byte("hello"),
			maxLen:   500,
			expected: "hello",
		},
		{
			name:     "exact length not truncated",
			body:     []byte("12345"),
			maxLen:   5,
			expected: "12345",
		},
		{
			name:     "long body truncated",
			body:     []byte("this is a very long body that should be truncated"),
			maxLen:   10,
			expected: "this is a ...[truncated]",
		},
		{
			name:     "empty body",
			body:     []byte{},
			maxLen:   500,
			expected: "",
		},
		{
			name:     "nil body",
			body:     nil,
			maxLen:   500,
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := truncateBody(tt.body, tt.maxLen)
			if result != tt.expected {
				t.Errorf("truncateBody() = %q, want %q", result, tt.expected)
			}
		})
	}
}

func TestCheckSteamErrors(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		header     http.Header
		body       []byte
		wantErr    bool
		errMsg     string
	}{
		{
			name:       "rate limit",
			statusCode: http.StatusTooManyRequests,
			header:     http.Header{},
			body:       []byte(""),
			wantErr:    true,
			errMsg:     "Rate limit exceeded",
		},
		{
			name:       "server error",
			statusCode: http.StatusInternalServerError,
			header:     http.Header{},
			body:       []byte(""),
			wantErr:    true,
			errMsg:     "Steam is down or in maintenance",
		},
		{
			name:       "auth redirect",
			statusCode: http.StatusFound,
			header:     http.Header{"Location": []string{"https://store.steampowered.com/login"}},
			body:       []byte(""),
			wantErr:    true,
			errMsg:     "Session expired",
		},
		{
			name:       "family view",
			statusCode: http.StatusForbidden,
			header:     http.Header{},
			body: []byte(
				`<div id="parental_notice_instructions">Enter your PIN below to exit Family View.</div>`,
			),
			wantErr: true,
			errMsg:  "Family View enabled",
		},
		{
			name:       "soft auth failure - guest",
			statusCode: http.StatusOK,
			header:     http.Header{},
			body:       []byte(`g_steamID = false;`),
			wantErr:    true,
			errMsg:     "Session expired",
		},
		{
			name:       "sorry page with reason",
			statusCode: http.StatusOK,
			header:     http.Header{},
			body:       []byte(`<h1>Sorry!</h1><div>Some content</div><h3>An error occurred</h3>`),
			wantErr:    true,
			errMsg:     "An error occurred",
		},
		{
			name:       "sorry page without reason",
			statusCode: http.StatusOK,
			header:     http.Header{},
			body:       []byte(`<h1>Sorry!</h1><div>Some content without h3</div>`),
			wantErr:    true,
			errMsg:     "unknown steam community error (Sorry page)",
		},
		{
			name:       "bad request with body truncation",
			statusCode: http.StatusBadRequest,
			header:     http.Header{},
			body: []byte(
				"This is a very long error body that should definitely be truncated to prevent information leakage in logs and error messages. The body contains sensitive data that should not be exposed to users or logged in plain text. This repeated content ensures the body exceeds 500 characters for testing purposes. Adding more text here to make sure we cross the threshold and trigger the truncation behavior that we want to verify works correctly in production environments. This additional text is added to ensure we have enough characters to trigger the truncation logic.",
			),
			wantErr: true,
			errMsg:  "This additional text is added to en...[truncated]",
		},
		{
			name:       "success",
			statusCode: http.StatusOK,
			header:     http.Header{},
			body:       []byte(`{"success": true}`),
			wantErr:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := checkSteamErrors(tt.statusCode, tt.header, tt.body)
			if (err != nil) != tt.wantErr {
				t.Errorf("checkSteamErrors() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if tt.wantErr && err != nil {
				if tt.errMsg != "" {
					if !strings.Contains(err.Error(), tt.errMsg) {
						t.Errorf("checkSteamErrors() error message = %q, want to contain %q", err.Error(), tt.errMsg)
					}
				}
			}
		})
	}
}

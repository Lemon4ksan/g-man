// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license[s] that can be found in the LICENSE file.

package log

import (
	"net/url"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestMaskQueryParams(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "masks API key",
			input:    "https://api.steampowered.com/ISteamWebAPI?key=abc123&format=json",
			expected: "https://api.steampowered.com/ISteamWebAPI?format=json&key=%2A%2A%2A",
		},
		{
			name:     "masks access_token",
			input:    "https://api.steampowered.com/ISteamWebAPI?access_token=secret123&format=json",
			expected: "https://api.steampowered.com/ISteamWebAPI?access_token=%2A%2A%2A&format=json",
		},
		{
			name:     "masks token",
			input:    "https://api.steampowered.com/ISteamWebAPI?token=secret123&format=json",
			expected: "https://api.steampowered.com/ISteamWebAPI?format=json&token=%2A%2A%2A",
		},
		{
			name:     "preserves non-sensitive params",
			input:    "https://api.steampowered.com/ISteamWebAPI?format=json&language=english",
			expected: "https://api.steampowered.com/ISteamWebAPI?format=json&language=english",
		},
		{
			name:     "masks multiple sensitive params",
			input:    "https://api.steampowered.com/ISteamWebAPI?key=abc&access_token=xyz&format=json",
			expected: "https://api.steampowered.com/ISteamWebAPI?access_token=%2A%2A%2A&format=json&key=%2A%2A%2A",
		},
		{
			name:     "handles nil URL",
			input:    "",
			expected: "",
		},
		{
			name:     "handles URL without query params",
			input:    "https://api.steampowered.com/ISteamWebAPI",
			expected: "https://api.steampowered.com/ISteamWebAPI",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var u *url.URL
			if tt.input != "" {
				var err error

				u, err = url.Parse(tt.input)
				if err != nil {
					t.Fatalf("failed to parse URL: %v", err)
				}
			}

			result := maskQueryParams(u)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestMaskQueryParams_DoesNotModifyOriginal(t *testing.T) {
	t.Parallel()

	original := "https://api.steampowered.com/ISteamWebAPI?key=abc123&format=json"

	u, err := url.Parse(original)
	if err != nil {
		t.Fatalf("failed to parse URL: %v", err)
	}

	originalQuery := u.Query().Get("key")

	_ = maskQueryParams(u)

	if u.Query().Get("key") != originalQuery {
		t.Error("maskQueryParams should not modify the original URL")
	}
}

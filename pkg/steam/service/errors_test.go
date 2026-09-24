// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package service

import (
	"errors"
	"testing"

	"github.com/lemon4ksan/g-man/pkg/steam/protocol/enums"
)

func TestAuthErrors(t *testing.T) {
	t.Parallel()

	authErrors := []enums.EResult{
		enums.EResult_NotLoggedOn,
		enums.EResult_Expired,
		enums.EResult_InvalidPassword,
	}

	for _, res := range authErrors {
		if !IsAuthError(res) {
			t.Errorf("expected %v to be an auth error", res)
		}
	}

	if IsAuthError(enums.EResult_OK) {
		t.Error("EResult_OK should not be an auth error")
	}
}

func TestErrorStructures(t *testing.T) {
	t.Parallel()

	t.Run("EResultError", func(t *testing.T) {
		t.Parallel()

		baseErr := errors.New("underlying")
		err := NewEResultError(enums.EResult_Busy, baseErr)

		if !errors.Is(err, baseErr) {
			t.Error("EResultError unwrap failed")
		}

		if err.Error() == "" {
			t.Error("empty error string")
		}
	})

	t.Run("SteamAPIError", func(t *testing.T) {
		t.Parallel()

		baseErr := errors.New("network_fail")
		err := NewSteamAPIError("fail", 500, baseErr)

		if !errors.Is(err, baseErr) {
			t.Error("SteamAPIError unwrap failed")
		}

		expected := "steam API error: message=fail, status=500: network_fail"
		if err.Error() != expected {
			t.Errorf("expected %s, got %s", expected, err.Error())
		}
	})
}

func TestParseEResultFromMessage(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		msg      string
		wantCode enums.EResult
		wantOK   bool
	}{
		{
			name:     "invalid_state_11",
			msg:      "There was an error accepting this offer. Please try again later. (11)",
			wantCode: enums.EResult_InvalidState,
			wantOK:   true,
		},
		{
			name:     "timeout_16",
			msg:      "There was an error accepting this offer. Please try again later. (16)",
			wantCode: enums.EResult_Timeout,
			wantOK:   true,
		},
		{
			name:     "service_unavailable_20",
			msg:      "There was an error communicating with the network. Please try again later. (20)",
			wantCode: enums.EResult_ServiceUnavailable,
			wantOK:   true,
		},
		{
			name:     "limit_exceeded_28",
			msg:      "You cannot accept this trade offer because your active trade offers limit has been exceeded. (28)",
			wantCode: enums.EResult(28),
			wantOK:   true,
		},
		{
			name:     "pattern_sent_too_many_offers",
			msg:      "You have sent too many trade offers recently.",
			wantCode: enums.EResult_LimitExceeded,
			wantOK:   true,
		},
		{
			name:     "pattern_item_server_unavailable",
			msg:      "We were unable to contact the game's item server.",
			wantCode: enums.EResult_ServiceUnavailable,
			wantOK:   true,
		},
		{
			name:     "no_code",
			msg:      "Access Denied",
			wantCode: enums.EResult_Invalid,
			wantOK:   false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			code, ok := ParseEResultFromMessage(tc.msg)
			if ok != tc.wantOK {
				t.Fatalf("expected ok=%v, got %v", tc.wantOK, ok)
			}

			if ok && code != tc.wantCode {
				t.Fatalf("expected code=%v, got %v", tc.wantCode, code)
			}
		})
	}
}

func TestExtractEResult(t *testing.T) {
	t.Parallel()

	eErr := NewEResultError(enums.EResult_Timeout, nil)

	code, ok := ExtractEResult(eErr)
	if !ok || code != enums.EResult_Timeout {
		t.Fatalf("expected EResult_Timeout, got code=%v ok=%v", code, ok)
	}

	wrapped := NewSteamAPIError("fail", 500, eErr)

	codeW, okW := ExtractEResult(wrapped)
	if !okW || codeW != enums.EResult_Timeout {
		t.Fatalf("expected EResult_Timeout from wrapped, got code=%v ok=%v", codeW, okW)
	}

	plainErr := errors.New("something went wrong")

	_, okPlain := ExtractEResult(plainErr)
	if okPlain {
		t.Fatalf("expected ok=false for plain error")
	}
}

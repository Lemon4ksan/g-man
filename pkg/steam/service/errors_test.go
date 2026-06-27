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

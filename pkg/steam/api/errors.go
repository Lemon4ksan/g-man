// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package api

import (
	"errors"
	"fmt"

	"github.com/lemon4ksan/g-man/pkg/steam/protocol"
)

var (
	// ErrNotLoggedIn indicates the session cookie is missing or invalid.
	ErrNotLoggedIn = errors.New("steam community: not logged in (session expired)")

	// ErrFamilyViewRestricted indicates the account is currently in Family View mode.
	ErrFamilyViewRestricted = errors.New("steam community: family view restricted")

	// ErrRateLimit indicates Steam is blocking requests due to high frequency.
	ErrRateLimit = errors.New("steam community: rate limit exceeded")
)

// EResultError wraps a Steam EResult code into a Go error.
type EResultError struct {
	EResult protocol.EResult
}

func (e EResultError) Error() string {
	return e.EResult.String()
}

// SteamAPIError represents a structured error returned by Steam's internal
// APIs (often seen in mobile confirmations or trading).
type SteamAPIError struct {
	// Message is the human-readable error description from Steam.
	Message string

	// NeedAuth indicates that the session is invalid and requires a login refresh.
	NeedAuth bool

	// StatusCode is the raw HTTP status code.
	StatusCode int
}

func (e SteamAPIError) Error() string {
	return fmt.Sprintf("steam API error: %s (need_refresh=%v, status=%d)",
		e.Message, e.NeedAuth, e.StatusCode)
}

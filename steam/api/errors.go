// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package api

import (
	"fmt"

	"github.com/lemon4ksan/g-man/steam/protocol"
)

type EResultError struct {
	EResult protocol.EResult
}

func (e EResultError) Error() string {
	return e.EResult.String()
}

// SteamAPIError represents a structured error from Steam's confirmation API.
type SteamAPIError struct {
	// Message is the error description from Steam.
	Message string

	// NeedAuth indicates that the access token has expired and needs refresh.
	NeedAuth bool

	// StatusCode is the HTTP status code from the response.
	StatusCode int
}

func (e *SteamAPIError) Error() string {
	return fmt.Sprintf("steam API error: %s (need_refresh=%v, status=%d)",
		e.Message, e.NeedAuth, e.StatusCode)
}

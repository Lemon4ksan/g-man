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
	// ErrSessionExpired signals that the current AccessToken or CM
	// session is no longer valid. This is the trigger for an update.
	ErrSessionExpired = errors.New("steam: session expired or invalid")
)

// IsAuthError checks whether EResult is a signal for reauthorization.
func IsAuthError(res protocol.EResult) bool {
	switch res {
	case protocol.EResult_NotLoggedOn, // 21
		protocol.EResult_Expired,              // 27
		protocol.EResult_LogonSessionReplaced, // 34
		protocol.EResult_InvalidPassword,      // 5
		protocol.EResult_AccountLogonDenied:   // 63
		return true
	}
	return false
}

// EResultError wraps a Steam EResult code into a Go error.
type EResultError struct {
	EResult protocol.EResult
	Err     error
}

func (e EResultError) Error() string {
	return "EMsg: " + e.EResult.String()
}

func (e EResultError) Unwrap() error {
	return e.Err
}

// SteamAPIError represents a structured error returned by Steam's internal
// APIs (often seen in mobile confirmations or trading).
type SteamAPIError struct {
	// Message is the human-readable error description from Steam.
	Message string

	// StatusCode is the raw HTTP status code.
	StatusCode int

	// Special error that can be unwrapped.
	Err error
}

func (e SteamAPIError) Error() string {
	return fmt.Sprintf("steam API error: message=%s, status=%d",
		e.Message, e.StatusCode)
}

func (e SteamAPIError) Unwrap() error {
	return e.Err
}

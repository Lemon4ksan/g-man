// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package api

import (
	"errors"
	"fmt"

	"github.com/lemon4ksan/g-man/pkg/steam/protocol/enums"
)

var (
	// ErrSessionExpired signals that the current AccessToken or CM
	// session is no longer valid. This is the trigger for an update.
	ErrSessionExpired = errors.New("api: session expired or invalid")

	// ErrFormat is returned when the response doesn't
	// match the specified format or the target is invalid.
	ErrFormat = errors.New("api: response format error")
)

// IsAuthError checks whether EResult is a signal for reauthorization.
func IsAuthError(res enums.EResult) bool {
	switch res {
	case enums.EResult_NotLoggedOn, // 21
		enums.EResult_Expired,              // 27
		enums.EResult_LogonSessionReplaced, // 34
		enums.EResult_InvalidPassword,      // 5
		enums.EResult_AccountLogonDenied:   // 63
		return true
	}

	return false
}

// EResultError wraps a Steam EResult code into a Go error.
type EResultError struct {
	Result enums.EResult
	Err    error
}

func (e EResultError) Error() string {
	return "EMsg: " + e.Result.String()
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

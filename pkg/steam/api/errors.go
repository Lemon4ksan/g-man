// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package api

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/lemon4ksan/g-man/pkg/steam/protocol/enums"
)

var (
	// ErrSessionExpired signals that the current AccessToken or CM
	// session is no longer valid. This is the trigger for an update.
	ErrSessionExpired = errors.New("api: session expired or invalid")

	// ErrFormat is returned when the response doesn't
	// match the specified format or the target is invalid.
	ErrFormat = errors.New("api: response format error")

	// ErrRateLimited indicates Steam is blocking requests due to high frequency.
	ErrRateLimited = errors.New("api: rate limit exceeded")
)

// RetriableError defines an interface for errors that represent transient issues.
type RetriableError interface {
	IsRetriable() bool
}

// IsRetriable is a helper that checks if an error (or any error wrapped inside it)
// implements RetriableError and is safe to retry.
func IsRetriable(err error) bool {
	var re RetriableError
	if errors.As(err, &re) {
		return re.IsRetriable()
	}

	return false
}

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
	Err    error // Optional underlying error or context
}

// NewEResultError creates a new EResultError.
func NewEResultError(res enums.EResult, err error) *EResultError {
	return &EResultError{Result: res, Err: err}
}

func (e *EResultError) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("steam error %s (%d): %v", e.Result.String(), e.Result, e.Err)
	}

	return fmt.Sprintf("steam error %s (%d)", e.Result.String(), e.Result)
}

// Unwrap returns the underlying error, if any.
func (e *EResultError) Unwrap() error {
	return e.Err
}

// Is allows errors.Is to match specific EResult values wrapped in EResultError.
func (e *EResultError) Is(target error) bool {
	var t *EResultError
	if errors.As(target, &t) {
		return e.Result == t.Result
	}

	return false
}

// IsRetriable implements RetriableError. Returns true if the EResult is typically a transient network or server issue.
func (e *EResultError) IsRetriable() bool {
	switch e.Result {
	case enums.EResult_Timeout,
		enums.EResult_TryAnotherCM,
		enums.EResult_ServiceUnavailable,
		enums.EResult_Pending,
		enums.EResult_Busy,
		enums.EResult_LimitExceeded:
		return true
	}

	return false
}

// SteamAPIError is a structured error returned by Steam's internal APIs.
type SteamAPIError struct {
	// Message is the human-readable error description from Steam.
	Message string

	// StatusCode is the raw HTTP status code.
	StatusCode int

	// Special error that can be unwrapped.
	Err error
}

// NewSteamAPIError creates a new SteamAPIError.
func NewSteamAPIError(message string, statusCode int, err error) *SteamAPIError {
	return &SteamAPIError{Message: message, StatusCode: statusCode, Err: err}
}

// Error returns the error message.
func (e *SteamAPIError) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("steam API error: message=%q, status=%d: %v", e.Message, e.StatusCode, e.Err)
	}

	return fmt.Sprintf("steam API error: message=%q, status=%d", e.Message, e.StatusCode)
}

// Unwrap returns the underlying error, if any.
func (e *SteamAPIError) Unwrap() error {
	return e.Err
}

// IsRetriable implements RetriableError.
func (e *SteamAPIError) IsRetriable() bool {
	return e.StatusCode >= http.StatusInternalServerError || e.StatusCode == http.StatusTooManyRequests
}

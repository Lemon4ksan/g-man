// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package service

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/lemon4ksan/g-man/pkg/steam/protocol/enums"
)

var (
	// ErrSessionExpired signals that the active session or OAuth2 access token has expired.
	ErrSessionExpired = errors.New("api: session expired or invalid")
	// ErrRateLimited signals that Steam rate limits were hit.
	ErrRateLimited = errors.New("api: rate limit exceeded")
)

// RetriableError identifies transient errors safe for automated retries.
type RetriableError interface {
	IsRetriable() bool
}

// IsRetriable checks whether err implements RetriableError and returns true.
func IsRetriable(err error) bool {
	var re RetriableError
	if errors.As(err, &re) {
		return re.IsRetriable()
	}

	return false
}

// IsAuthError reports whether an EResult indicates session authentication expiry or invalidation.
func IsAuthError(res enums.EResult) bool {
	switch res {
	case enums.EResult_NotLoggedOn,
		enums.EResult_Expired,
		enums.EResult_LogonSessionReplaced,
		enums.EResult_InvalidPassword,
		enums.EResult_AccountLogonDenied:
		return true
	}

	return false
}

// EResultError wraps Steam EResult error response codes.
type EResultError struct {
	Result enums.EResult
	Err    error
}

func NewEResultError(res enums.EResult, err error) *EResultError {
	return &EResultError{Result: res, Err: err}
}

func (e *EResultError) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("steam error %s (%d): %v", e.Result.String(), e.Result, e.Err)
	}

	return fmt.Sprintf("steam error %s (%d)", e.Result.String(), e.Result)
}

func (e *EResultError) Unwrap() error {
	return e.Err
}

func (e *EResultError) Is(target error) bool {
	var t *EResultError
	if errors.As(target, &t) {
		return e.Result == t.Result
	}

	return false
}

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

// SteamAPIError represents structured HTTP/WebAPI error status payloads returned by Steam.
type SteamAPIError struct {
	Message    string
	StatusCode int
	Err        error
}

func NewSteamAPIError(message string, statusCode int, err error) *SteamAPIError {
	return &SteamAPIError{Message: message, StatusCode: statusCode, Err: err}
}

func (e *SteamAPIError) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("steam API error: message=%s, status=%d: %v", e.Message, e.StatusCode, e.Err)
	}

	return fmt.Sprintf("steam API error: message=%s, status=%d", e.Message, e.StatusCode)
}

func (e *SteamAPIError) Unwrap() error {
	return e.Err
}

func (e *SteamAPIError) IsRetriable() bool {
	return e.StatusCode >= http.StatusInternalServerError || e.StatusCode == http.StatusTooManyRequests
}

func (e *SteamAPIError) Is(target error) bool {
	var t *SteamAPIError
	if errors.As(target, &t) {
		return e.StatusCode == t.StatusCode && (t.Message == "" || e.Message == t.Message)
	}

	if e.Err != nil && errors.Is(e.Err, target) {
		return true
	}

	return false
}

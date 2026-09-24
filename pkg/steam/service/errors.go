// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package service

import (
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"github.com/lemon4ksan/g-man/pkg/steam/protocol/enums"
)

var eResultPattern = regexp.MustCompile(`\((\d+)\)`)

// ParseEResultFromMessage extracts an EResult code enclosed in parentheses from an error string.
// E.g. "There was an error accepting this offer. Please try again later. (11)" -> EResult_InvalidState (11).
// Also parses common Steam message patterns matching node-steam-tradeoffer-manager.
func ParseEResultFromMessage(msg string) (enums.EResult, bool) {
	if match := eResultPattern.FindStringSubmatch(msg); len(match) > 1 {
		if code, err := strconv.Atoi(match[1]); err == nil {
			return enums.EResult(code), true
		}
	}

	if strings.Contains(msg, "sent too many trade offers") {
		return enums.EResult_LimitExceeded, true
	}

	if strings.Contains(msg, "unable to contact the game's item server") {
		return enums.EResult_ServiceUnavailable, true
	}

	return enums.EResult_Invalid, false
}

// ExtractEResult extracts the EResult enum from err if it wraps an EResultError.
func ExtractEResult(err error) (enums.EResult, bool) {
	if eresultErr, ok := errors.AsType[*EResultError](err); ok {
		return eresultErr.Result, true
	}

	return enums.EResult_Invalid, false
}

var (
	// ErrSessionExpired signals that the active session or OAuth2 access token has expired.
	ErrSessionExpired = errors.New("api: session expired or invalid")
	// ErrRateLimited signals that Steam rate limits were hit.
	ErrRateLimited = errors.New("api: rate limit exceeded")
)

// RetriableError identifies transient errors safe for automated retries.
type RetriableError interface {
	error
	IsRetriable() bool
}

// IsRetriable checks whether err implements RetriableError and returns true.
func IsRetriable(err error) bool {
	if re, ok := errors.AsType[RetriableError](err); ok {
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
	if t, ok := errors.AsType[*EResultError](target); ok {
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
		enums.EResult_LimitExceeded,
		enums.EResult(28):
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
	if t, ok := errors.AsType[*SteamAPIError](target); ok {
		return e.StatusCode == t.StatusCode && (t.Message == "" || e.Message == t.Message)
	}

	if e.Err != nil && errors.Is(e.Err, target) {
		return true
	}

	return false
}

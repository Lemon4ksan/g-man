// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package rest

import (
	"errors"
	"fmt"
	"net/http"
)

// ErrProxyAuthRequired is returned when the request is declined by a proxy.
var ErrProxyAuthRequired = errors.New("rest: proxy authentication required (HTTP 407)")

// APIError represents an unsuccessful HTTP response (status code outside 2xx).
// It captures the raw response body, which often contains error details from the server.
type APIError struct {
	// StatusCode is the HTTP status code returned by the server.
	StatusCode int
	// Body is the raw response body.
	Body []byte
}

func (e *APIError) Error() string {
	if len(e.Body) > 0 {
		return fmt.Sprintf("rest: status %d, body: %s", e.StatusCode, string(e.Body))
	}
	return fmt.Sprintf("rest: unexpected status code %d", e.StatusCode)
}

func (e *APIError) Is(target error) bool {
	if target == ErrProxyAuthRequired && e.StatusCode == http.StatusProxyAuthRequired { // 407
		return true
	}
	return false
}

// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package rest

import (
	"fmt"
)

// APIError represents an unsuccessful HTTP response (status code outside 2xx).
// It captures the raw response body, which often contains error details from the server.
type APIError struct {
	// StatusCode is the HTTP status code returned by the server.
	StatusCode int
	// Body is the raw response body.
	Body []byte
}

func (e *APIError) Error() string {
	return fmt.Sprintf("rest: status %d", e.StatusCode)
}

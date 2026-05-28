// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package log

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"time"
)

type correlationIDKey struct{}

// WithCorrelationID returns a new context containing the correlation ID.
func WithCorrelationID(ctx context.Context, id string) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}

	return context.WithValue(ctx, correlationIDKey{}, id)
}

// CorrelationID extracts the correlation ID from context.
func CorrelationID(ctx context.Context) (string, bool) {
	if ctx == nil {
		return "", false
	}

	id, ok := ctx.Value(correlationIDKey{}).(string)

	return id, ok
}

// GenerateCorrelationID generates a secure, fast 16-byte hex correlation ID.
func GenerateCorrelationID() string {
	var bytes [16]byte
	if _, err := rand.Read(bytes[:]); err != nil {
		// Fallback to time-based key if crypto/rand fails
		return fmt.Sprintf("fallback-%d", time.Now().UnixNano())
	}

	return hex.EncodeToString(bytes[:])
}

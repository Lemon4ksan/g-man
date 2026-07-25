// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package network

import (
	"errors"
	"fmt"
	"strings"
)

// Op represents the network operation that failed.
type Op string

// Operations that can fail within the network package.
const (
	// OpDial represents a network dial or connection establishment operation.
	OpDial Op = "dial"
	// OpSend represents a network write or data transmission operation.
	OpSend Op = "send"
	// OpRead represents a network read or data reception operation.
	OpRead Op = "read"
	// OpClose represents a network connection closure operation.
	OpClose Op = "close"
	// OpEncrypt represents a message encryption operation.
	OpEncrypt Op = "encrypt"
	// OpDecrypt represents a message decryption operation.
	OpDecrypt Op = "decrypt"
	// OpDeadline represents setting a connection read or write deadline.
	OpDeadline Op = "set deadline"
	// OpFramer represents a message framing or unframing operation.
	OpFramer Op = "framer"
	// OpProxy represents a proxy connection or handshake operation.
	OpProxy Op = "proxy"
)

// Error represents a structured error within the network package.
//
// Create new instances of the error using the [NewError] constructor.
// It provides detailed context about a network failure, including the operation
// that failed, the transport protocol, and the underlying cause.
type Error struct {
	// Op is the network operation that failed.
	Op Op
	// Net is the network protocol or transport type (e.g., "TCP", "WS").
	Net string
	// Err is the underlying error that caused the failure.
	Err error
}

// NewError returns a new structured Error initialized with the specified
// operation, transport protocol, and underlying error.
func NewError(op Op, net string, err error) *Error {
	return &Error{Op: op, Net: net, Err: err}
}

// Error returns the string representation of the Error.
func (e *Error) Error() string {
	netName := "network"
	if e.Net != "" {
		netName = strings.ToLower(e.Net)
	}

	if e.Err == nil {
		return fmt.Sprintf("%s: %s failed", netName, e.Op)
	}

	return fmt.Sprintf("%s: %s failed: %v", netName, e.Op, e.Err)
}

// Unwrap returns the underlying error causing this failure, if any.
func (e *Error) Unwrap() error {
	return e.Err
}

// Is reports whether the target error matches the current error.
//
// It matches if the target is an *Error with the same Op and either a matching Net
// (case-insensitive) or an empty Net. If the target is not an *Error, it checks
// if the underlying error matches the target.
func (e *Error) Is(target error) bool {
	var t *Error
	if errors.As(target, &t) {
		return e.Op == t.Op && (t.Net == "" || strings.EqualFold(e.Net, t.Net))
	}

	if e.Err != nil && errors.Is(e.Err, target) {
		return true
	}

	return false
}

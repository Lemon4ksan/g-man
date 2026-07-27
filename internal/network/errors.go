// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package network

import (
	"errors"
	"fmt"
	"strings"
)

// Op identifies a failed network operation type.
type Op string

const (
	OpDial     Op = "dial"
	OpSend     Op = "send"
	OpRead     Op = "read"
	OpClose    Op = "close"
	OpEncrypt  Op = "encrypt"
	OpDecrypt  Op = "decrypt"
	OpDeadline Op = "set deadline"
	OpFramer   Op = "framer"
	OpProxy    Op = "proxy"
)

// Error provides context-rich details regarding network, protocol, and framing errors.
type Error struct {
	Op  Op
	Net string
	Err error
}

// NewError constructs an Error with operation, network type, and cause.
func NewError(op Op, net string, err error) *Error {
	return &Error{Op: op, Net: net, Err: err}
}

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

func (e *Error) Unwrap() error {
	return e.Err
}

// Is supports matching via errors.Is against target operations and transport names.
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

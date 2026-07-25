// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package network

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestError(t *testing.T) {
	t.Run("Formatting", func(t *testing.T) {
		err1 := NewError(OpDial, "TCP", errors.New("connection timed out"))
		assert.Equal(t, "tcp: dial failed: connection timed out", err1.Error())

		err2 := NewError(OpClose, "WS", nil)
		assert.Equal(t, "ws: close failed", err2.Error())

		err3 := NewError(OpRead, "", errors.New("EOF"))
		assert.Equal(t, "network: read failed: EOF", err3.Error())
	})

	t.Run("Unwrap", func(t *testing.T) {
		inner := errors.New("some error")
		err := NewError(OpSend, "TCP", inner)
		assert.Equal(t, inner, err.Unwrap())
	})

	t.Run("Is", func(t *testing.T) {
		inner := errors.New("timeout")
		err := NewError(OpDial, "TCP", inner)

		// Matches by target Error containing exact Op and Net
		assert.ErrorIs(t, err, &Error{Op: OpDial, Net: "TCP"})
		assert.ErrorIs(t, err, &Error{Op: OpDial, Net: "tcp"})

		// Matches by underlying error
		assert.ErrorIs(t, err, inner)

		// General matches (target has empty Net)
		assert.ErrorIs(t, err, &Error{Op: OpDial, Net: ""})

		// Non-matches
		assert.NotErrorIs(t, err, &Error{Op: OpSend, Net: "TCP"})
		assert.NotErrorIs(t, err, &Error{Op: OpDial, Net: "WS"})
		assert.NotErrorIs(t, err, errors.New("other"))
	})

	t.Run("Is_MoreCoverage", func(t *testing.T) {
		t.Parallel()

		errNilInner := NewError(OpClose, "WS", nil)
		assert.NotErrorIs(t, errNilInner, errors.New("other"))

		// Case where target is *Error, but Op matches and Net is different
		assert.NotErrorIs(t, errNilInner, &Error{Op: OpClose, Net: "TCP"})

		// Case where target is *Error, Op is different, Net matches
		assert.NotErrorIs(t, errNilInner, &Error{Op: OpDial, Net: "WS"})
	})
}

// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package clock provides mockable time primitives and offset-adjusted clocks for Steam server synchronization.
package clock

import (
	"sync/atomic"
	"time"
)

// Clock defines the contract for acquiring current time timestamps.
type Clock interface {
	Now() time.Time
}

// OffsetClock implements Clock with atomic time offset tracking to align local system time with Steam Connection Manager time.
//
// Thread Safety:
//   - Fully thread-safe. Reads and writes use atomic operations.
type OffsetClock struct {
	offset atomic.Int64
}

// SetOffset sets the time duration added to system time.
func (c *OffsetClock) SetOffset(offset time.Duration) {
	c.offset.Store(int64(offset))
}

// Now returns the current time shifted by the configured server offset.
func (c *OffsetClock) Now() time.Time {
	return time.Now().Add(time.Duration(c.offset.Load()))
}

// SystemClock implements Clock using unadjusted local system time.
type SystemClock struct{}

// Now returns the current local system time.
func (SystemClock) Now() time.Time { return time.Now() }

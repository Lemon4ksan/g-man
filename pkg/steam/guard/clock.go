// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package guard

import (
	"sync/atomic"
	"time"
)

// Clock is an interface for getting the current time.
// It allows mocking time in tests and handling Steam's time offsets.
type Clock interface {
	Now() time.Time
}

// OffsetClock implements [Clock], adding a fixed offset to the system time.
// This is used to synchronize with Steam's server time.
type OffsetClock struct {
	offset atomic.Int64
}

// SetOffset updates the time offset applied to system time.
func (c *OffsetClock) SetOffset(offset time.Duration) {
	c.offset.Store(int64(offset))
}

// Now returns the current time with the configured offset.
func (c *OffsetClock) Now() time.Time {
	return time.Now().Add(time.Duration(c.offset.Load()))
}

// SystemClock implements [Clock] using the local system time without any synchronization.
type SystemClock struct{}

// Now returns the current system time.
func (SystemClock) Now() time.Time { return time.Now() }

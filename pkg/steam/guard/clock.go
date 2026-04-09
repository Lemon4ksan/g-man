// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package guard

import (
	"sync/atomic"
	"time"
)

// Clock is an interface for getting the current time.
type Clock interface {
	Now() time.Time
}

// OffsetClock implements [models.Clock], adding a fixed offset to the system time.
type OffsetClock struct {
	offset atomic.Int64
}

func (c *OffsetClock) SetOffset(offset time.Duration) {
	c.offset.Store(int64(offset))
}

func (c *OffsetClock) Now() time.Time {
	return time.Now().Add(time.Duration(c.offset.Load()))
}

// SystemClock implements [models.Clock] in case synchronization is not needed (uses local time).
type SystemClock struct{}

func (SystemClock) Now() time.Time { return time.Now() }

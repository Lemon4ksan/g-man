// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package status

import (
	"time"

	"github.com/lemon4ksan/miyako/bus"
)

// StatusUpdatedEvent is published on the event bus whenever the Steam status is successfully refreshed.
type StatusUpdatedEvent struct {
	bus.BaseEvent
	StatusText string
	IdleAppIDs []uint32
	Timestamp  time.Time
}

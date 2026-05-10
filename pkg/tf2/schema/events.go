// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package schema

import (
	"time"

	"github.com/lemon4ksan/g-man/pkg/bus"
)

// ReadyEvent is emitted when the schema is ready.
type ReadyEvent struct {
	bus.BaseEvent
}

// UpdatedEvent is emitted when the schema is updated.
type UpdatedEvent struct {
	bus.BaseEvent
	Timestamp time.Time
}

// UpdateFailedEvent is emitted when the schema update fails.
type UpdateFailedEvent struct {
	bus.BaseEvent
	Error error
}

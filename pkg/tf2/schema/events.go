// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package schema

import (
	"time"

	"github.com/lemon4ksan/g-man/pkg/bus"
)

type ReadyEvent struct {
	bus.BaseEvent
}

func (e *ReadyEvent) Topic() string { return "schema.ready" }

type UpdatedEvent struct {
	bus.BaseEvent
	Timestamp time.Time
}

func (e *UpdatedEvent) Topic() string { return "schema.updated" }

type UpdateFailedEvent struct {
	bus.BaseEvent
	Error error
}

func (e *UpdateFailedEvent) Topic() string { return "schema.failed" }

// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package tf2schema

import (
	"time"

	"github.com/lemon4ksan/g-man/pkg/steam/bus"
)

type SchemaReadyEvent struct {
	bus.BaseEvent
}

func (e *SchemaReadyEvent) Topic() string { return "tf2schema.ready" }

type SchemaUpdatedEvent struct {
	bus.BaseEvent
	Timestamp time.Time
}

func (e *SchemaUpdatedEvent) Topic() string { return "tf2schema.updated" }

type SchemaUpdateFailedEvent struct {
	bus.BaseEvent
	Error error
}

func (e *SchemaUpdateFailedEvent) Topic() string { return "tf2schema.failed" }

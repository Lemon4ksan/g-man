package tf2schema

import (
	"github.com/lemon4ksan/g-man/steam/bus"
	"time"
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

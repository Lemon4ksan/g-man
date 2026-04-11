// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package guard

import (
	"github.com/lemon4ksan/g-man/pkg/bus"
)

// Event is the base interface that all guard events must implement.
type Event interface {
	bus.Event
	isGuardEvent()
}

// StateEvent is emitted whenever the guard transitions between states.
type StateEvent struct {
	bus.BaseEvent
	New State
}

func (e *StateEvent) isGuardEvent() {}

// ConfirmationReceivedEvent is emitted when guardian receives a new confirmation.
type ConfirmationReceivedEvent struct {
	bus.BaseEvent
	Confirmation *Confirmation
}

func (e *ConfirmationReceivedEvent) isGuardEvent() {}

// NeedAuthEvent is emitted when confirmation is returned with NeedAuth field set to True.
type NeedAuthEvent struct {
	bus.BaseEvent
	Message string
}

func (e *NeedAuthEvent) isGuardEvent() {}

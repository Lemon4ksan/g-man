// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package socket

import (
	"time"

	"github.com/lemon4ksan/g-man/pkg/bus"
	"github.com/lemon4ksan/g-man/pkg/steam/protocol/enums"
)

// Event defines the base interface that socket events must implement.
type Event interface {
	bus.Event
	isSocketEvent()
}

// StateEvent is emitted whenever the socket transitions between states
// (e.g., from Connecting to Connected).
type StateEvent struct {
	bus.BaseEvent
	Old State
	New State
}

func (e StateEvent) Topic() string { return "socket.state" }

// ConnectedEvent is emitted when the socket successfully establishes a transport
// connection to a Steam CM Server.
type ConnectedEvent struct {
	bus.BaseEvent
	Server string // The endpoint the socket connected to (Host:Port)
}

func (e ConnectedEvent) Topic() string { return "socket.connected" }

// NetworkErrorEvent is emitted when a non-fatal underlying network error occurs
// during active communication.
type NetworkErrorEvent struct {
	bus.BaseEvent
	Error error
}

func (e NetworkErrorEvent) Topic() string { return "socket.networkError" }

// DisconnectedEvent is emitted when the socket connection is closed.
// This can happen intentionally or due to a network/Steam drop.
type DisconnectedEvent struct {
	bus.BaseEvent

	// Error contains the reason for the disconnect, if any.
	// Nil if the disconnect was triggered gracefully by the client.
	Error error

	// EResult contains the Steam result code if the disconnection was
	// initiated by the Steam server (e.g., LoggedOff, InvalidPassword).
	EResult enums.EResult
}

func (e DisconnectedEvent) Topic() string { return "socket.disconnected" }

// ReconnectAttemptEvent is published whenever an attempt is made to reconnect.
type ReconnectAttemptEvent struct {
	bus.BaseEvent
	Attempt int
	Delay   time.Duration
}

func (e ReconnectAttemptEvent) Topic() string { return "socket.reconnectAttempt" }

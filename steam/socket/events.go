// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package socket provides event structures for network lifecycle monitoring.
package socket

import (
	"github.com/lemon4ksan/g-man/steam/bus"
	"github.com/lemon4ksan/g-man/steam/protocol"
)

type SocketEvent interface {
	bus.Event
	IsSocketEvent()
}

// StateEvent is emitted whenever the socket transitions between states
// (e.g., from Connecting to Connected).
type StateEvent struct {
	bus.BaseEvent
	Old State
	New State
}

func (e StateEvent) Topic() string  { return "socket.state" }
func (e StateEvent) IsSocketEvent() {}

// ConnectedEvent is emitted when the socket successfully establishes a transport
// connection to a Steam CM Server.
type ConnectedEvent struct {
	bus.BaseEvent
	Server string // The endpoint the socket connected to (Host:Port)
}

func (e ConnectedEvent) Topic() string  { return "socket.connected" }
func (e ConnectedEvent) IsSocketEvent() {}

// NetworkErrorEvent is emitted when a non-fatal underlying network error occurs
// during active communication.
type NetworkErrorEvent struct {
	bus.BaseEvent
	Error error
}

// Topic fixed: Added missing Topic method for the event bus.
func (e NetworkErrorEvent) Topic() string  { return "socket.network_error" }
func (e NetworkErrorEvent) IsSocketEvent() {}

// DisconnectedEvent is emitted when the socket connection is closed.
// This can happen intentionally or due to a network/Steam drop.
type DisconnectedEvent struct {
	bus.BaseEvent

	// Error contains the reason for the disconnect, if any.
	// Nil if the disconnect was triggered gracefully by the client.
	Error error

	// EResult contains the Steam result code if the disconnection was
	// initiated by the Steam server (e.g., LoggedOff, InvalidPassword).
	EResult protocol.EResult
}

func (e DisconnectedEvent) Topic() string  { return "socket.disconnected" }
func (e DisconnectedEvent) IsSocketEvent() {}

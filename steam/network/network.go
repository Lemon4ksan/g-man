// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package network

import (
	"context"
	"sync/atomic"
)

// globalConnectionID is an atomic counter used to generate unique connection IDs.
var globalConnectionID atomic.Int64

// NetMessage represents a binary message received over the network.
type NetMessage []byte

// Handler defines the callbacks that a connection will invoke for network events.
type Handler interface {
	// OnNetMessage is called when a complete message is received.
	OnNetMessage(msg NetMessage)

	// OnNetError is called when a non-fatal error occurs on the connection.
	// Fatal errors typically result in the connection being closed automatically.
	OnNetError(err error)

	// OnNetClose is called when the connection is closed, either
	// by the remote peer or due to an unrecoverable error.
	OnNetClose()
}

// Connection defines the interface that all network connections must implement.
type Connection interface {
	// Send transmits the provided data over the connection.
	// Returns an error if the data could not be sent.
	Send(ctx context.Context, data []byte) error

	// Close gracefully terminates the connection and releases any resources.
	// Returns an error if the closure fails.
	Close() error

	// ID returns a unique identifier for this connection instance.
	// IDs are guaranteed to be unique across all connections created
	// during the program's lifetime.
	ID() int64

	// Name returns the protocol name (e.g., "TCP", "WS") for this connection.
	Name() string
}

// Encryptable is an optional interface for encrypted connections.
type Encryptable interface {
	SetEncryptionKey(key []byte)
}

// BaseConnection provides common functionality and state shared by all connection implementations.
type BaseConnection struct {
	id   int64
	name string
}

// NewBaseConnection creates a new BaseConnection with a unique ID and the provided name.
func NewBaseConnection(name string) BaseConnection {
	return BaseConnection{
		id:   globalConnectionID.Add(1),
		name: name,
	}
}

// ID returns the unique identifier for this connection.
func (b *BaseConnection) ID() int64 {
	return b.id
}

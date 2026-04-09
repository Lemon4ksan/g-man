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

// NetMessage represents a complete, raw binary message received from the network.
// It is a slice of bytes that a Handler can process.
type NetMessage []byte

// Handler defines the event-driven callbacks that a network connection will invoke.
// This interface decouples the raw network I/O from the application's logic.
type Handler interface {
	// OnNetMessage is called when a complete message is framed and received.
	OnNetMessage(msg NetMessage)

	// OnNetError is called when a non-fatal error occurs on the connection.
	// Fatal errors typically result in the connection being closed automatically.
	OnNetError(err error)

	// OnNetClose is called exactly once when the connection is terminated,
	// either by the remote peer or due to an unrecoverable local error.
	OnNetClose()
}

// Connection defines the standard interface that all network connection types must implement.
// It provides methods for sending data, closing the connection, and identification.
type Connection interface {
	// Send transmits the provided data over the connection.
	// It is the responsibility of the implementation to handle message framing and encryption.
	// This method must be safe for concurrent use.
	Send(ctx context.Context, data []byte) error

	// Close gracefully terminates the connection and releases any associated resources.
	// This method should be idempotent (safe to call multiple times).
	Close() error

	// ID returns a unique identifier for this connection instance.
	// IDs are guaranteed to be unique across all connections created
	// during the program's lifetime.
	ID() int64

	// Name returns the protocol name (e.g., "TCP", "WS") for this connection.
	Name() string
}

// Encryptable is an optional interface that connections can implement to support
// session-based symmetric encryption.
type Encryptable interface {
	// SetEncryptionKey provides the connection with the secret key used to
	// encrypt outgoing messages and decrypt incoming ones.
	SetEncryptionKey(key []byte)
}

// BaseConnection provides common functionality and state shared by all connection implementations.
// It should be embedded in concrete connection types (e.g., TCPConnection).
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

// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package network implements low-level network connections and raw data framing for TCP and WebSockets.
package network

import (
	"context"
	"io"
	"sync/atomic"
)

// globalConnectionID is an atomic counter used to generate unique connection IDs.
var globalConnectionID atomic.Int64

// Message represents a complete, raw binary message received from the network.
// It is a slice of bytes that represents a framed or protocol-specific network packet.
type Message []byte

// Cipher defines an interface for symmetric encryption and decryption.
// It abstracts the cryptographic implementation from the transport layer.
type Cipher interface {
	// Encrypt encrypts the given plaintext data and returns the ciphertext.
	Encrypt(data []byte) ([]byte, error)

	// Decrypt decrypts the given ciphertext data and returns the plaintext.
	Decrypt(data []byte) ([]byte, error)
}

// Framer defines an interface for reading and writing discrete frames
// over a stream-oriented connection like TCP.
type Framer interface {
	// ReadFrame reads a single framed message from the reader and returns its payload.
	// It returns an error if reading fails or if the frame is invalid.
	ReadFrame(r io.Reader) ([]byte, error)

	// WriteFrame frames and writes the payload to the writer.
	// It returns an error if writing fails.
	WriteFrame(w io.Writer, data []byte) error
}

// Connection represents a bi-directional network connection that can send
// and receive discrete messages.
//
// Standard implementations include [TCP] (created via [NewTCP]) and [WS] (created via [NewWS]).
// Implementations of Connection are expected to handle transport-specific details,
// such as read/write deadlines, encryption, and framing, in a concurrent-safe manner.
type Connection interface {
	// Send transmits the provided message over the connection.
	//
	// Send must be safe for concurrent use. If the context is canceled or
	// its deadline is reached before the send completes, Send must return
	// the context error.
	Send(ctx context.Context, data []byte) error

	// Close gracefully terminates the connection, closes all channels, and releases
	// all associated resources.
	//
	// Close must be safe for concurrent use and idempotent. After Close is called,
	// subsequent calls should return nil or the original close error.
	Close() error

	// ID returns a unique identifier for this connection instance.
	// IDs are guaranteed to be unique within a single execution of the program.
	ID() int64

	// Name returns the name of the transport protocol (e.g., "TCP" or "WS").
	Name() string

	// Messages returns a channel that receives incoming messages from the network.
	// The channel is closed when the connection is terminated.
	Messages() <-chan Message

	// Errors returns a channel that receives non-fatal errors encountered during
	// connection read or write operations. The channel is closed when the
	// connection is terminated.
	Errors() <-chan error

	// Closed returns a channel that is closed when the connection is terminated
	// and all cleanup operations have completed.
	Closed() <-chan struct{}
}

// Encryptable is an optional interface that can be implemented by a [Connection]
// to support post-handshake, session-based symmetric encryption.
type Encryptable interface {
	// SetCipher configures the Connection to use the specified [Cipher] for
	// encrypting all subsequent outgoing messages and decrypting incoming ones.
	// Pass nil to disable encryption on the connection.
	// It returns true if the cipher was successfully applied.
	SetCipher(cipher Cipher) bool
}

// BaseConnection provides common fields and methods shared by all connection
// implementations, such as connection tracking and identity.
//
// It is intended to be embedded in concrete [Connection] implementations.
type BaseConnection struct {
	id   int64
	name string
}

// NewBaseConnection returns a new BaseConnection initialized with a unique identifier
// and the specified protocol name.
//
// The unique identifier is generated using a global, thread-safe atomic counter.
func NewBaseConnection(name string) BaseConnection {
	return BaseConnection{
		id:   globalConnectionID.Add(1),
		name: name,
	}
}

// ID returns the unique identifier for the connection.
func (b *BaseConnection) ID() int64 {
	return b.id
}

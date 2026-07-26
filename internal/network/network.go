// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package network provides network utilities for the g-man client.
package network

import (
	"context"
	"io"
	"sync/atomic"

	"github.com/lemon4ksan/g-man/internal/framer"
)

var globalConnectionID atomic.Int64

// Message represents a complete, raw binary message received from the network.
type Message = *framer.FrameBuffer

// Cipher defines an interface for symmetric encryption and decryption.
type Cipher interface {
	Encrypt(data []byte) ([]byte, error)
	Decrypt(data *framer.FrameBuffer) (*framer.FrameBuffer, error)
}

// Framer defines an interface for reading and writing discrete frames.
type Framer interface {
	ReadFrame(r io.Reader) (*framer.FrameBuffer, error)
	WriteFrame(w io.Writer, data []byte) error
}

// Connection represents a bi-directional network connection.
type Connection interface {
	Send(ctx context.Context, data []byte) error
	Close() error
	ID() int64
	Name() string
	Messages() <-chan Message
	Errors() <-chan error
	Closed() <-chan struct{}
}

// Encryptable is an optional interface to support session-based encryption.
type Encryptable interface {
	SetCipher(cipher Cipher) bool
}

// BaseConnection provides common fields shared by all connection implementations.
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

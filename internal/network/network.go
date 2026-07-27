// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package network provides socket primitives, packet framing, and encrypted transport abstractions.
package network

import (
	"context"
	"io"
	"sync/atomic"

	"github.com/lemon4ksan/g-man/internal/framer"
)

var globalConnectionID atomic.Int64

// Message represents a framed byte buffer.
type Message = *framer.FrameBuffer

// Cipher defines symmetric encryption and decryption methods for framed messages.
type Cipher interface {
	Encrypt(data []byte) ([]byte, error)
	Decrypt(data *framer.FrameBuffer) (*framer.FrameBuffer, error)
}

// Framer defines reading and writing packet framing contracts over byte streams.
type Framer interface {
	ReadFrame(r io.Reader) (*framer.FrameBuffer, error)
	WriteFrame(w io.Writer, data []byte) error
}

// Connection represents a bidirectional network connection.
type Connection interface {
	Send(ctx context.Context, data []byte) error
	Close() error
	ID() int64
	Name() string
	Messages() <-chan Message
	Errors() <-chan error
	Closed() <-chan struct{}
}

// Encryptable provides dynamic cipher assignment capabilities for connections.
type Encryptable interface {
	SetCipher(cipher Cipher) bool
}

// BaseConnection tracks unique connection IDs and network protocol labels.
type BaseConnection struct {
	id   int64
	name string
}

// NewBaseConnection constructs a BaseConnection with an auto-incremented atomic ID.
func NewBaseConnection(name string) BaseConnection {
	return BaseConnection{
		id:   globalConnectionID.Add(1),
		name: name,
	}
}

// ID returns the connection's atomic identifier.
func (b *BaseConnection) ID() int64 {
	return b.id
}

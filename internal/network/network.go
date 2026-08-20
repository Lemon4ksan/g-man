// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package network provides socket primitives, packet framing, and encrypted transport abstractions.
package network

import (
	"context"
	"io"
	"sync/atomic"

	json "github.com/goccy/go-json"
	"github.com/lemon4ksan/aoni"
	"github.com/lemon4ksan/aoni/codec/decode"
	"github.com/lemon4ksan/aoni/option"
	"github.com/lemon4ksan/aoni/realtime/socket"

	"github.com/lemon4ksan/g-man/internal/framer"
)

var globalConnectionID atomic.Int64

// GoJSONDecoder wraps github.com/goccy/go-json as an aoni response decoder for SIMD-accelerated JSON unmarshaling.
var GoJSONDecoder decode.Decoder = decode.DecoderFunc(func(reader io.Reader, target any) error {
	return json.NewDecoder(reader).Decode(target)
})

// DefaultClientOptions returns standard aoni options registering go-json for standard JSON MIME types.
func DefaultClientOptions() []aoni.ClientOption {
	return []aoni.ClientOption{
		option.WithDecoder("application/json", GoJSONDecoder),
		option.WithDecoder("text/javascript", GoJSONDecoder),
	}
}

// NewClient constructs a new aoni.Client preconfigured with the go-json SIMD decoder.
func NewClient(doer aoni.HTTPDoer, opts ...aoni.ClientOption) *aoni.Client {
	allOpts := append(DefaultClientOptions(), opts...)
	return aoni.NewClient(doer, allOpts...)
}

// Message represents a framed byte buffer.
type Message = *framer.FrameBuffer

// Cipher defines symmetric encryption and decryption methods for framed messages.
type Cipher = socket.Cipher

// Framer defines reading and writing packet framing contracts over byte streams.
type Framer = socket.Framer

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

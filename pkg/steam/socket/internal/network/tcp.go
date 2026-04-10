// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package network

import (
	"bufio"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"sync"
	"time"

	"github.com/lemon4ksan/g-man/pkg/crypto"
	"github.com/lemon4ksan/g-man/pkg/log"
)

const (
	// Magic are the 4 bytes that prefix every Steam TCP packet header.
	Magic = "VT01"
	// ReadTimeout is the maximum duration to wait for data before closing the connection.
	ReadTimeout = 60 * time.Second
	// WriteTimeout is the default duration to wait for a write to complete.
	WriteTimeout = 5 * time.Second
)

// Compile-time checks to ensure interface satisfaction.
var _ Connection = (*TCP)(nil)
var _ Encryptable = (*TCP)(nil)

// TCP implements the Connection interface for Steam's custom TCP protocol.
// It handles length-prefixed message framing and optional symmetric encryption.
type TCP struct {
	BaseConnection
	conn    net.Conn
	handler Handler
	logger  log.Logger

	writeMu    sync.Mutex   // Ensures atomic writes of header + payload.
	keyMu      sync.RWMutex // Protects sessionKey for concurrent reads/writes.
	sessionKey []byte
}

// NewTCP establishes a TCP connection to the given endpoint and starts its read loop.
func NewTCP(handler Handler, logger log.Logger, endpoint string) (*TCP, error) {
	conn, err := net.DialTimeout("tcp", endpoint, 5*time.Second)
	if err != nil {
		return nil, err
	}

	t := &TCP{
		BaseConnection: NewBaseConnection("TCP"),
		conn:           conn,
		handler:        handler,
		logger:         logger.With(log.String("transport", "TCP"), log.String("endpoint", endpoint)),
	}

	go t.readLoop()
	return t, nil
}

func (t *TCP) Name() string { return "TCP" }

// SetEncryptionKey enables symmetric encryption for all subsequent messages.
func (t *TCP) SetEncryptionKey(key []byte) {
	t.keyMu.Lock()
	t.sessionKey = key
	t.keyMu.Unlock()
	t.logger.Debug("Encryption enabled")
}

// Send encrypts (if a key is set) and frames the data before sending it over the TCP socket.
// The frame format is: [4-byte length][4-byte magic][payload].
func (t *TCP) Send(ctx context.Context, data []byte) error {
	t.keyMu.RLock()
	key := t.sessionKey
	t.keyMu.RUnlock()

	var err error
	if key != nil {
		data, err = crypto.SymmetricEncryptWithHmacIv(data, key)
		if err != nil {
			return fmt.Errorf("tcp: encrypt failed: %w", err)
		}
	}

	var header [8]byte
	binary.LittleEndian.PutUint32(header[0:4], uint32(len(data)))
	copy(header[4:8], Magic)

	t.writeMu.Lock()
	defer t.writeMu.Unlock()

	if deadline, ok := ctx.Deadline(); ok {
		t.conn.SetWriteDeadline(deadline)
	} else {
		t.conn.SetWriteDeadline(time.Now().Add(WriteTimeout))
	}

	// Use net.Buffers for a "gather write", which is more efficient than two separate writes.
	buffers := net.Buffers{header[:], data}
	if _, err := buffers.WriteTo(t.conn); err != nil {
		return err
	}

	return nil
}

// Close terminates the underlying TCP connection.
func (t *TCP) Close() error {
	if t.conn == nil {
		return nil
	}
	return t.conn.Close()
}

// readLoop runs in a dedicated goroutine, continuously reading and decoding packets.
func (t *TCP) readLoop() {
	defer func() {
		t.conn.Close()
		t.handler.OnNetClose()
	}()

	reader := bufio.NewReaderSize(t.conn, 64*1024)
	var header [8]byte

	for {
		t.conn.SetReadDeadline(time.Now().Add(ReadTimeout))

		// Read the fixed-size header first.
		if _, err := io.ReadFull(reader, header[:]); err != nil {
			if !isIgnorableError(err) {
				t.handler.OnNetError(err)
			}
			return
		}

		if string(header[4:8]) != Magic {
			t.handler.OnNetError(errors.New("tcp: invalid magic bytes"))
			return
		}

		length := binary.LittleEndian.Uint32(header[0:4])
		if length > 10*1024*1024 { // 10MB sanity limit
			t.handler.OnNetError(fmt.Errorf("tcp: packet too large (%d bytes)", length))
			return
		}

		// Read the variable-length payload.
		payload := make([]byte, length)
		if _, err := io.ReadFull(reader, payload); err != nil {
			t.handler.OnNetError(err)
			return
		}

		t.keyMu.RLock()
		key := t.sessionKey
		t.keyMu.RUnlock()

		if key != nil {
			var err error
			payload, err = crypto.SymmetricDecrypt(payload, key, true)
			if err != nil {
				t.handler.OnNetError(fmt.Errorf("tcp: decrypt failed: %w", err))
				// Don't return, as this might be a single corrupt packet.
				// Depending on the protocol, you might want to continue or disconnect.
				continue
			}
		}

		t.handler.OnNetMessage(payload)
	}
}

// isIgnorableError checks for errors that are expected during a normal connection closure.
func isIgnorableError(err error) bool {
	return errors.Is(err, net.ErrClosed) || errors.Is(err, io.EOF)
}

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

	"github.com/lemon4ksan/g-man/pkg/log"
	"github.com/lemon4ksan/g-man/pkg/steam/crypto"
)

const (
	Magic        = "VT01" // Steam TCP packet magic
	ReadTimeout  = 60 * time.Second
	WriteTimeout = 5 * time.Second
)

var _ Connection = (*TCPConnection)(nil)
var _ Encryptable = (*TCPConnection)(nil)

type TCPConnection struct {
	BaseConnection
	conn    net.Conn
	handler Handler
	logger  log.Logger

	writeMu    sync.Mutex   // Ensures atomic packet writes
	keyMu      sync.RWMutex // Protects sessionKey
	sessionKey []byte
}

func NewTCPConnection(handler Handler, logger log.Logger, endpoint string) (*TCPConnection, error) {
	conn, err := net.DialTimeout("tcp", endpoint, 5*time.Second)
	if err != nil {
		return nil, err
	}

	t := &TCPConnection{
		BaseConnection: NewBaseConnection("TCP"),
		conn:           conn,
		handler:        handler,
		logger:         logger.With(log.String("transport", "TCP"), log.String("endpoint", endpoint)),
	}

	go t.readLoop()
	return t, nil
}

func (t *TCPConnection) Name() string { return "TCP" }

func (t *TCPConnection) SetEncryptionKey(key []byte) {
	t.keyMu.Lock()
	t.sessionKey = key
	t.keyMu.Unlock()
	t.logger.Debug("Encryption enabled")
}

func (t *TCPConnection) Send(ctx context.Context, data []byte) error {
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

	packetLen := uint32(len(data))

	// [4-byte length] + [4-byte magic] + [payload]
	var header [8]byte
	binary.LittleEndian.PutUint32(header[0:4], packetLen)
	copy(header[4:8], Magic)

	t.writeMu.Lock()
	defer t.writeMu.Unlock()

	if deadline, ok := ctx.Deadline(); ok {
		t.conn.SetWriteDeadline(deadline)
	} else {
		t.conn.SetWriteDeadline(time.Now().Add(WriteTimeout))
	}

	buffers := net.Buffers{header[:], data}
	if _, err := buffers.WriteTo(t.conn); err != nil {
		return err
	}

	return nil
}

func (t *TCPConnection) Close() error {
	if t.conn == nil {
		return nil
	}
	return t.conn.Close()
}

func (t *TCPConnection) readLoop() {
	defer func() {
		t.conn.Close()
		t.handler.OnNetClose()
	}()

	reader := bufio.NewReaderSize(t.conn, 64*1024)
	var header [8]byte

	for {
		t.conn.SetReadDeadline(time.Now().Add(ReadTimeout))

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
		if length > 10*1024*1024 { // 10MB limit
			t.handler.OnNetError(fmt.Errorf("tcp: packet too large (%d bytes)", length))
			return
		}

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
				return
			}
		}

		t.handler.OnNetMessage(payload)
	}
}

func isIgnorableError(err error) bool {
	return errors.Is(err, net.ErrClosed) || errors.Is(err, io.EOF)
}

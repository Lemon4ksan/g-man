// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package network

import (
	"context"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lemon4ksan/g-man/pkg/log"
)

func TestTCP_NewTCP_Fail(t *testing.T) {
	// Dialing a non-existent endpoint
	_, err := NewTCP(context.Background(), NewMockHandler(), log.Discard, "127.0.0.1:1")
	assert.Error(t, err)
}

func TestTCP_Name(t *testing.T) {
	tcp := &TCP{}
	assert.Equal(t, "TCP", tcp.Name())
}

func TestTCP_Send_Oversized(t *testing.T) {
	tcp := &TCP{sessionKey: nil}
	data := make([]byte, 11*1024*1024) // > 10MB
	err := tcp.Send(context.Background(), data)
	assert.ErrorContains(t, err, "exceeds maximum packet size")
}

func TestTCP_ReadLoop_Coverage(t *testing.T) {
	logger := log.Discard

	t.Run("Decryption Branch", func(t *testing.T) {
		handler := NewMockHandler()
		s, c := net.Pipe()
		tcp := &TCP{conn: c, handler: handler, logger: logger, BaseConnection: NewBaseConnection("TCP")}

		key := make([]byte, 32)
		tcp.SetEncryptionKey(key)

		go tcp.readLoop()

		// Send garbage that will fail decryption
		data := []byte("garbage_data_that_is_not_encrypted_correctly")
		header := make([]byte, 8)
		binary.LittleEndian.PutUint32(header[0:4], uint32(len(data)))
		copy(header[4:8], Magic)

		_, _ = s.Write(header)
		_, _ = s.Write(data)

		select {
		case err := <-handler.ErrChan():
			assert.ErrorContains(t, err, "tcp: decrypt failed")
		case <-time.After(time.Second):
			t.Fatal("timeout")
		}
	})

	t.Run("Payload Unexpected EOF", func(t *testing.T) {
		handler := NewMockHandler()
		s, c := net.Pipe()

		tcp := &TCP{conn: c, handler: handler, logger: logger, BaseConnection: NewBaseConnection("TCP")}
		go tcp.readLoop()

		header := make([]byte, 8)
		binary.LittleEndian.PutUint32(header[0:4], 100)
		copy(header[4:8], Magic)
		_, _ = s.Write(header)
		// Write only 1 byte of the 100 expected, then close
		_, _ = s.Write([]byte{0x01})
		_ = s.Close()

		select {
		case err := <-handler.ErrChan():
			// io.ReadFull returns ErrUnexpectedEOF if at least 1 byte was read
			assert.ErrorIs(t, err, io.ErrUnexpectedEOF)
		case <-time.After(time.Second):
			t.Fatal("timeout")
		}
	})

	t.Run("Magic Mismatch", func(t *testing.T) {
		h := NewMockHandler()
		s, c := net.Pipe()

		tcp := &TCP{conn: c, handler: h, logger: log.Discard}
		go tcp.readLoop()

		_, _ = s.Write([]byte{4, 0, 0, 0, 'B', 'A', 'A', 'D'})

		select {
		case err := <-h.ErrChan():
			assert.ErrorContains(t, err, "invalid magic")
		case <-time.After(time.Second):
			t.Fatal("timeout")
		}
	})

	t.Run("Read Header Error", func(t *testing.T) {
		h := NewMockHandler()
		s, c := net.Pipe()

		tcp := &TCP{conn: c, handler: h, logger: log.Discard}
		go tcp.readLoop()

		_ = s.Close() // Immediate EOF on header read

		// Should not send error to handler because EOF is ignorable
		time.Sleep(50 * time.Millisecond)
		h.mu.Lock()
		assert.Empty(t, h.errors)
		h.mu.Unlock()
	})
}

func TestTCP_SetEncryptionKey(t *testing.T) {
	tcp := &TCP{logger: log.Discard}
	key := []byte("secret-key-12345")
	ok := tcp.SetEncryptionKey(key)
	assert.True(t, ok)
	assert.Equal(t, key, tcp.sessionKey)
}

func TestTCP_Send_Deadline(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)

	defer l.Close()

	go func() {
		conn, _ := l.Accept()
		if conn != nil {
			conn.Close()
		}
	}()

	clientConn, err := net.Dial("tcp", l.Addr().String())
	require.NoError(t, err)

	tcp := &TCP{
		conn:           clientConn,
		logger:         log.Discard,
		BaseConnection: NewBaseConnection("TCP"),
	}
	defer tcp.Close()

	t.Run("Immediate_Context_Cancel", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel() // Cancel before calling Send

		assert.ErrorIs(t, tcp.Send(ctx, []byte("some data")), context.Canceled)
	})

	t.Run("No_Context_Deadline_Branch", func(t *testing.T) {
		assert.NotPanics(t, func() {
			tcp.Send(context.Background(), []byte("data"))
		})
	})
}

func TestTCP_ReadLoop_ErrorBranches(t *testing.T) {
	h := NewMockHandler()
	s, c := net.Pipe()
	tcp := &TCP{conn: c, handler: h, logger: log.Discard}

	t.Run("Packet Too Large", func(t *testing.T) {
		go tcp.readLoop()

		header := make([]byte, 8)
		binary.LittleEndian.PutUint32(header[0:4], 11*1024*1024) // 11MB
		copy(header[4:8], Magic)
		_, _ = s.Write(header)

		select {
		case err := <-h.ErrChan():
			assert.Contains(t, err.Error(), "too large")
		case <-time.After(time.Second):
			t.Fatal("timeout")
		}
	})
}

func TestIsIgnorableError(t *testing.T) {
	assert.True(t, isIgnorableError(io.EOF))
	assert.True(t, isIgnorableError(net.ErrClosed))
	assert.False(t, isIgnorableError(context.DeadlineExceeded))
	assert.False(t, isIgnorableError(errors.New("random error")))
}

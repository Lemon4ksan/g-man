// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package network

import (
	"context"
	"encoding/binary"
	"io"
	"net"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lemon4ksan/g-man/pkg/log"
)

func TestTCPConnection_SendAndReceive(t *testing.T) {
	handler := NewMockHandler()
	logger := log.Discard

	lc := net.ListenConfig{}
	l, err := lc.Listen(t.Context(), "tcp", "127.0.0.1:0")
	require.NoError(t, err)

	defer l.Close()

	serverDone := make(chan struct{})

	go func() {
		defer close(serverDone)

		conn, err := l.Accept()
		if err != nil {
			return
		}
		defer conn.Close()

		var header [8]byte

		_, _ = io.ReadFull(conn, header[:])

		length := binary.LittleEndian.Uint32(header[0:4])
		payload := make([]byte, length)
		_, _ = io.ReadFull(conn, payload)

		_, _ = conn.Write(header[:])
		_, _ = conn.Write(payload)
	}()

	client, err := NewTCP(t.Context(), handler, logger, l.Addr().String())
	require.NoError(t, err)

	defer client.Close()

	testData := []byte("hello steam tcp")
	err = client.Send(context.Background(), testData)
	assert.NoError(t, err)

	select {
	case msg := <-handler.msgChan:
		assert.Equal(t, testData, msg)
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for tcp message")
	}
}

func TestTCPConnection_InvalidMagic(t *testing.T) {
	handler := NewMockHandler()

	lc := net.ListenConfig{}

	l, _ := lc.Listen(t.Context(), "tcp", "127.0.0.1:0")
	defer l.Close()

	go func() {
		conn, _ := l.Accept()
		_, _ = conn.Write([]byte{0, 0, 0, 4, 'B', 'A', 'A', 'D', 'd', 'a', 't', 'a'})
		_ = conn.Close()
	}()

	client, _ := NewTCP(t.Context(), handler, log.Discard, l.Addr().String())
	defer client.Close()

	time.Sleep(100 * time.Millisecond)
	handler.mu.Lock()
	assert.True(t, len(handler.errors) > 0)
	assert.Contains(t, handler.errors[0].Error(), "invalid magic")
	handler.mu.Unlock()
}

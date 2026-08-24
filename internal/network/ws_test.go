// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package network

import (
	"context"
	"errors"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/lemon4ksan/aoni/fast"
	"github.com/lemon4ksan/aoni/realtime/ws"
	"github.com/lemon4ksan/foundation/async/log"
	"github.com/stretchr/testify/assert"

	"github.com/lemon4ksan/g-man/internal/framer"
)

type mockWSConn struct {
	setWriteDeadlineFunc func(t time.Time) error
	writeMessageFunc     func(messageType int, data []byte) error
	closeFunc            func() error
	readMessageFunc      func() (messageType int, payload []byte, err error)
}

func (m *mockWSConn) SetWriteDeadline(t time.Time) error {
	if m.setWriteDeadlineFunc != nil {
		return m.setWriteDeadlineFunc(t)
	}

	return nil
}

func (m *mockWSConn) WriteMessage(messageType int, data []byte) error {
	if m.writeMessageFunc != nil {
		return m.writeMessageFunc(messageType, data)
	}

	return nil
}

func (m *mockWSConn) Close() error {
	if m.closeFunc != nil {
		return m.closeFunc()
	}

	return nil
}

func (m *mockWSConn) ReadMessage() (messageType int, payload []byte, err error) {
	if m.readMessageFunc != nil {
		return m.readMessageFunc()
	}

	return 0, nil, io.EOF
}

func TestWS_NewWS(t *testing.T) {
	t.Parallel()

	// Attempt to dial a bad endpoint
	_, err := NewWS(shortCtx(t), log.Discard, "invalid:80", "", nil)
	assert.Error(t, err)
}

func TestWS_NewWS_URLSchemaAndProxy(t *testing.T) {
	t.Parallel()

	t.Run("invalid_endpoint_url_parse", func(t *testing.T) {
		t.Parallel()
		_, err := NewWS(shortCtx(t), log.Discard, "wss://%", "", nil)
		assert.Error(t, err)
	})

	t.Run("http_schema_normalization", func(t *testing.T) {
		t.Parallel()
		_, err := NewWS(shortCtx(t), log.Discard, "http://localhost:1", "", nil)
		assert.Error(t, err)
	})

	t.Run("https_schema_normalization", func(t *testing.T) {
		t.Parallel()
		_, err := NewWS(shortCtx(t), log.Discard, "https://localhost:1", "", nil)
		assert.Error(t, err)
	})

	t.Run("invalid_proxy_url_parse", func(t *testing.T) {
		t.Parallel()
		_, err := NewWS(shortCtx(t), log.Discard, "localhost:1", "https://%", nil)
		assert.Error(t, err)
	})

	t.Run("valid_proxy_url", func(t *testing.T) {
		t.Parallel()
		_, err := NewWS(shortCtx(t), log.Discard, "localhost:1", "http://127.0.0.1:8888", nil)
		assert.Error(t, err)
	})
}

func TestWS_Name(t *testing.T) {
	t.Parallel()

	wsConn := &WS{}
	assert.Equal(t, "WS", wsConn.Name())
	assert.Nil(t, wsConn.Closed())
	assert.Nil(t, wsConn.Messages())
	assert.Nil(t, wsConn.Errors())
}

func TestWS_Send_Closed(t *testing.T) {
	t.Parallel()

	wsConn := &WS{conn: nil}
	err := wsConn.Send(t.Context(), []byte("data"))
	assert.ErrorContains(t, err, "connection closed")
	assert.Nil(t, wsConn.Errors())
}

func TestWS_Send_Deadline(t *testing.T) {
	t.Parallel()

	t.Run("conn_nil", func(t *testing.T) {
		t.Parallel()

		wsConn := &WS{conn: nil}
		err := wsConn.Send(t.Context(), []byte("data"))
		assert.ErrorContains(t, err, "connection closed")
	})

	t.Run("set_deadline_failure", func(t *testing.T) {
		t.Parallel()
		mockConn := &mockWSConn{
			setWriteDeadlineFunc: func(t time.Time) error {
				return errors.New("deadline failed")
			},
		}

		wsConn := &WS{
			BaseConnection: NewBaseConnection("WS"),
			conn:           mockConn,
			logger:         log.Discard,
		}

		err := wsConn.Send(t.Context(), []byte("data"))
		assert.ErrorContains(t, err, "deadline failed")
	})

	t.Run("write_message_failure", func(t *testing.T) {
		t.Parallel()

		mockConn := &mockWSConn{
			writeMessageFunc: func(messageType int, data []byte) error {
				return errors.New("write failed")
			},
		}

		wsConn := &WS{
			BaseConnection: NewBaseConnection("WS"),
			conn:           mockConn,
			logger:         log.Discard,
		}

		err := wsConn.Send(t.Context(), []byte("data"))
		assert.ErrorContains(t, err, "write failed")
	})

	t.Run("context_with_deadline_success", func(t *testing.T) {
		t.Parallel()

		mockConn := &mockWSConn{}

		wsConn := &WS{
			BaseConnection: NewBaseConnection("WS"),
			conn:           mockConn,
			logger:         log.Discard,
		}

		ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
		defer cancel()

		err := wsConn.Send(ctx, []byte("data"))
		assert.NoError(t, err)
	})
}

func TestWS_ReadLoop(t *testing.T) {
	t.Parallel()

	t.Run("read_binary_and_ignore_text", func(t *testing.T) {
		t.Parallel()

		reads := []struct {
			msgType int
			payload []byte
		}{
			{ws.FrameText, []byte("text")},
			{ws.FrameBinary, []byte("bin")},
		}
		readIdx := 0

		mockConn := &mockWSConn{
			readMessageFunc: func() (int, []byte, error) {
				if readIdx < len(reads) {
					item := reads[readIdx]
					readIdx++
					return item.msgType, item.payload, nil
				}

				time.Sleep(50 * time.Millisecond)

				return 0, nil, io.EOF
			},
		}

		w := &WS{
			BaseConnection: NewBaseConnection("WS"),
			conn:           mockConn,
			logger:         log.Discard,
			msgChan:        make(chan Message, 10),
			errChan:        make(chan error, 10),
			closedChan:     make(chan struct{}),
		}

		go w.readLoop()
		defer func() { _ = w.Close() }()

		select {
		case msg := <-w.Messages():
			assert.Equal(t, &framer.FrameBuffer{B: []byte("bin")}, msg)
		case <-time.After(2 * time.Second):
			t.Fatal("timeout")
		}
	})

	t.Run("new_ws_handshake_failure", func(t *testing.T) {
		t.Parallel()
		_, err := NewWS(shortCtx(t), log.Discard, "localhost:1", "", nil)
		assert.Error(t, err)
	})

	t.Run("new_ws_with_fast_client", func(t *testing.T) {
		t.Parallel()

		headers := make(http.Header)
		headers.Set("X-Test-Header", "G-MAN-TEST")

		_, err := NewWSWithFastClient(shortCtx(t), log.Discard, "invalid:80", "", headers, nil)
		assert.Error(t, err)

		fc := fast.NewClient(nil)
		_, err = NewWSWithFastClient(shortCtx(t), log.Discard, "invalid:80", "", headers, fc)
		assert.Error(t, err)
	})
}

func TestWS_Close(t *testing.T) {
	t.Parallel()

	mockConn := &mockWSConn{
		closeFunc: func() error {
			return nil
		},
		writeMessageFunc: func(messageType int, data []byte) error {
			return nil
		},
	}

	w := &WS{
		BaseConnection: NewBaseConnection("WS"),
		conn:           mockConn,
		logger:         log.Discard,
		msgChan:        make(chan Message, 10),
		errChan:        make(chan error, 10),
		closedChan:     make(chan struct{}),
	}

	// First close
	err := w.Close()
	assert.NoError(t, err)

	// Second call (hits sync.Once and should return immediately without error)
	err = w.Close()
	assert.NoError(t, err)
}

func TestWS_Close_Error(t *testing.T) {
	t.Parallel()

	mockConn := &mockWSConn{
		closeFunc: func() error {
			return errors.New("close failed")
		},
	}

	w := &WS{
		BaseConnection: NewBaseConnection("WS"),
		conn:           mockConn,
		logger:         log.Discard,
	}

	err := w.Close()
	assert.ErrorContains(t, err, "close failed")
}

func TestWS_ReadLoop_Coverage(t *testing.T) {
	t.Parallel()

	t.Run("read_loop_errChan_default_branch", func(t *testing.T) {
		t.Parallel()

		mockConn := &mockWSConn{
			readMessageFunc: func() (int, []byte, error) {
				return 0, nil, errors.New("unexpected EOF")
			},
		}

		w := &WS{
			BaseConnection: NewBaseConnection("WS"),
			conn:           mockConn,
			logger:         log.Discard,
			msgChan:        make(chan Message, 10),
			errChan:        make(chan error), // unbuffered so send will hit default branch because nobody is reading
			closedChan:     make(chan struct{}),
		}

		go w.readLoop()

		select {
		case <-w.Closed():
			// Successfully exited readLoop after encountering the error and taking default branch.
		case <-time.After(2 * time.Second):
			t.Fatal("timeout waiting for readLoop exit")
		}
	})

	t.Run("read_loop_closed_chan_branch", func(t *testing.T) {
		t.Parallel()

		hasSent := false
		mockConn := &mockWSConn{
			readMessageFunc: func() (int, []byte, error) {
				if !hasSent {
					hasSent = true
					return ws.FrameBinary, []byte("data"), nil
				}

				// Keep blocked for subsequent reads
				time.Sleep(1 * time.Second)

				return 0, nil, io.EOF
			},
		}

		w := &WS{
			BaseConnection: NewBaseConnection("WS"),
			conn:           mockConn,
			logger:         log.Discard,
			msgChan:        make(chan Message), // unbuffered so send blocks
			errChan:        make(chan error, 10),
			closedChan:     make(chan struct{}),
		}

		go w.readLoop()

		// Wait for readLoop to block on msgChan <- data
		time.Sleep(50 * time.Millisecond)

		// Send to closedChan to force select exit
		w.closedChan <- struct{}{}

		// Verify that closedChan is closed via readLoop's defer
		select {
		case _, ok := <-w.closedChan:
			assert.False(t, ok, "closedChan should be closed")
		case <-time.After(2 * time.Second):
			t.Fatal("timeout waiting for readLoop exit")
		}
	})
}

// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package network

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/lemon4ksan/miyako/log"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type mockWSConn struct {
	setWriteDeadlineFunc func(t time.Time) error
	writeMessageFunc     func(messageType int, data []byte) error
	closeFunc            func() error
	readMessageFunc      func() (messageType int, p []byte, err error)
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

func (m *mockWSConn) ReadMessage() (messageType int, p []byte, err error) {
	if m.readMessageFunc != nil {
		return m.readMessageFunc()
	}

	return 0, nil, nil
}

func TestWS_NewWS(t *testing.T) {
	t.Parallel()

	// Attempt to dial a bad endpoint
	_, err := NewWS(t.Context(), log.Discard, "invalid:80", "", nil)
	assert.Error(t, err)
}

func TestWS_NewWS_URLSchemaAndProxy(t *testing.T) {
	t.Parallel()

	t.Run("invalid_endpoint_url_parse", func(t *testing.T) {
		t.Parallel()
		_, err := NewWS(t.Context(), log.Discard, "wss://%", "", nil)
		assert.Error(t, err)
	})

	t.Run("http_schema_normalization", func(t *testing.T) {
		t.Parallel()
		_, err := NewWS(t.Context(), log.Discard, "http://localhost:1", "", nil)
		assert.Error(t, err)
	})

	t.Run("https_schema_normalization", func(t *testing.T) {
		t.Parallel()
		_, err := NewWS(t.Context(), log.Discard, "https://localhost:1", "", nil)
		assert.Error(t, err)
	})

	t.Run("invalid_proxy_url_parse", func(t *testing.T) {
		t.Parallel()
		_, err := NewWS(t.Context(), log.Discard, "localhost:1", "https://%", nil)
		assert.Error(t, err)
	})

	t.Run("valid_proxy_url", func(t *testing.T) {
		t.Parallel()
		_, err := NewWS(t.Context(), log.Discard, "localhost:1", "http://127.0.0.1:8888", nil)
		assert.Error(t, err)
	})
}

func TestWS_NewWS_HandshakeResponseClose(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
	}))
	defer server.Close()

	_, err := NewWS(t.Context(), log.Discard, server.URL, "", nil)
	assert.Error(t, err)
}

func TestWS_Name(t *testing.T) {
	t.Parallel()

	ws := &WS{}
	assert.Equal(t, "WS", ws.Name())
	assert.Nil(t, ws.Closed())
	assert.Nil(t, ws.Messages())
	assert.Nil(t, ws.Errors())
}

func TestWS_Send_Closed(t *testing.T) {
	t.Parallel()

	ws := &WS{conn: nil}
	err := ws.Send(t.Context(), []byte("data"))
	assert.ErrorContains(t, err, "connection closed")
	assert.Nil(t, ws.Errors())
}

func TestWS_Send_Deadline(t *testing.T) {
	t.Parallel()

	t.Run("conn_nil", func(t *testing.T) {
		t.Parallel()

		ws := &WS{conn: nil}
		err := ws.Send(t.Context(), []byte("data"))
		assert.ErrorContains(t, err, "connection closed")
	})

	t.Run("set_deadline_failure", func(t *testing.T) {
		t.Parallel()
		mockConn := &mockWSConn{
			setWriteDeadlineFunc: func(t time.Time) error {
				return errors.New("deadline failed")
			},
		}

		ws := &WS{
			BaseConnection: NewBaseConnection("WS"),
			conn:           mockConn,
			logger:         log.Discard,
		}

		err := ws.Send(t.Context(), []byte("data"))
		assert.ErrorContains(t, err, "deadline failed")
	})

	t.Run("write_message_failure", func(t *testing.T) {
		t.Parallel()

		mockConn := &mockWSConn{
			writeMessageFunc: func(messageType int, data []byte) error {
				return errors.New("write failed")
			},
		}

		ws := &WS{
			BaseConnection: NewBaseConnection("WS"),
			conn:           mockConn,
			logger:         log.Discard,
		}

		err := ws.Send(t.Context(), []byte("data"))
		assert.ErrorContains(t, err, "write failed")
	})

	t.Run("context_with_deadline_success", func(t *testing.T) {
		t.Parallel()

		mockConn := &mockWSConn{}

		ws := &WS{
			BaseConnection: NewBaseConnection("WS"),
			conn:           mockConn,
			logger:         log.Discard,
		}

		ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
		defer cancel()

		err := ws.Send(ctx, []byte("data"))
		assert.NoError(t, err)
	})
}

func TestWS_ReadLoop(t *testing.T) {
	t.Parallel()

	t.Run("dial_success_and_read_binary", func(t *testing.T) {
		t.Parallel()

		upgrader := websocket.Upgrader{}

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			conn, err := upgrader.Upgrade(w, r, nil)
			if err != nil {
				return
			}

			// Send Text (Ignored)
			_ = conn.WriteMessage(websocket.TextMessage, []byte("text"))
			// Send Binary (Processed)
			_ = conn.WriteMessage(websocket.BinaryMessage, []byte("bin"))
			// Keep open until client closes
			time.Sleep(100 * time.Millisecond)
			conn.Close()
		}))
		defer server.Close()

		endpoint := strings.TrimPrefix(server.URL, "http://")
		u := url.URL{Scheme: "ws", Host: endpoint, Path: "/cmsocket/"}
		conn, _, err := websocket.DefaultDialer.Dial(u.String(), nil)
		require.NoError(t, err)

		ws := &WS{
			BaseConnection: NewBaseConnection("WS"),
			conn:           conn,
			logger:         log.Discard,
			msgChan:        make(chan Message, 10),
			errChan:        make(chan error, 10),
			closedChan:     make(chan struct{}),
		}

		go ws.readLoop()
		defer ws.Close()

		select {
		case msg := <-ws.Messages():
			assert.Equal(t, Message("bin"), msg)
		case <-time.After(2 * time.Second):
			t.Fatal("timeout")
		}
	})

	t.Run("new_ws_handshake_failure", func(t *testing.T) {
		t.Parallel()
		_, err := NewWS(t.Context(), log.Discard, "localhost:1", "", nil)
		assert.Error(t, err)
	})

	t.Run("headers_are_sent", func(t *testing.T) {
		t.Parallel()

		headerKey := "X-Test-Header"
		headerVal := "G-MAN-TEST"

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, headerVal, r.Header.Get(headerKey))

			upgrader := websocket.Upgrader{}

			conn, err := upgrader.Upgrade(w, r, nil)
			if err == nil {
				_ = conn.Close()
			}
		}))
		defer server.Close()

		headers := make(http.Header)
		headers.Set(headerKey, headerVal)

		ws, err := NewWS(t.Context(), log.Discard, server.URL, "", headers)
		require.NoError(t, err)

		_ = ws.Close()
	})
}

func TestWS_Close_MultipleTimes(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upgrader := websocket.Upgrader{}
		conn, _ := upgrader.Upgrade(w, r, nil)
		_ = conn.Close()
	}))
	defer server.Close()

	// Use ws:// for the test server
	endpoint := strings.TrimPrefix(server.URL, "http://")
	u := url.URL{Scheme: "ws", Host: endpoint, Path: "/cmsocket/"}

	// Dial manually to ensure we have a valid connection for the Close test
	conn, _, err := websocket.DefaultDialer.Dial(u.String(), nil)
	require.NoError(t, err)

	ws := &WS{
		BaseConnection: NewBaseConnection("WS"),
		conn:           conn,
		logger:         log.Discard,
		msgChan:        make(chan Message, 10),
		errChan:        make(chan error, 10),
		closedChan:     make(chan struct{}),
	}

	// First close
	err = ws.Close()
	assert.NoError(t, err)

	// Second call (hits sync.Once and should return immediately without panic)
	err = ws.Close()
	assert.NoError(t, err)
}

func TestWS_Close_Error(t *testing.T) {
	t.Parallel()

	mockConn := &mockWSConn{
		closeFunc: func() error {
			return errors.New("close failed")
		},
	}

	ws := &WS{
		BaseConnection: NewBaseConnection("WS"),
		conn:           mockConn,
		logger:         log.Discard,
	}

	err := ws.Close()
	assert.ErrorContains(t, err, "close failed")
}

func TestWS_ReadLoop_Coverage(t *testing.T) {
	t.Parallel()

	t.Run("read_loop_errChan_default_branch", func(t *testing.T) {
		t.Parallel()

		mockConn := &mockWSConn{
			readMessageFunc: func() (messageType int, p []byte, err error) {
				return 0, nil, errors.New("unexpected EOF")
			},
		}

		ws := &WS{
			BaseConnection: NewBaseConnection("WS"),
			conn:           mockConn,
			logger:         log.Discard,
			msgChan:        make(chan Message, 10),
			errChan:        make(chan error), // unbuffered so send will hit default branch because nobody is reading
			closedChan:     make(chan struct{}),
		}

		go ws.readLoop()

		select {
		case <-ws.Closed():
			// Successfully exited readLoop after encountering the error and taking default branch.
		case <-time.After(2 * time.Second):
			t.Fatal("timeout waiting for readLoop exit")
		}
	})

	t.Run("read_loop_closed_chan_branch", func(t *testing.T) {
		t.Parallel()

		hasSent := false
		mockConn := &mockWSConn{
			readMessageFunc: func() (messageType int, p []byte, err error) {
				if !hasSent {
					hasSent = true
					return websocket.BinaryMessage, []byte("data"), nil
				}

				// Keep blocked for subsequent reads
				time.Sleep(1 * time.Second)

				return 0, nil, io.EOF
			},
		}

		ws := &WS{
			BaseConnection: NewBaseConnection("WS"),
			conn:           mockConn,
			logger:         log.Discard,
			msgChan:        make(chan Message), // unbuffered so send blocks
			errChan:        make(chan error, 10),
			closedChan:     make(chan struct{}),
		}

		go ws.readLoop()

		// Wait for readLoop to block on msgChan <- data
		time.Sleep(50 * time.Millisecond)

		// Send to closedChan to force select exit
		ws.closedChan <- struct{}{}

		// Verify that closedChan is closed via readLoop's defer
		select {
		case _, ok := <-ws.closedChan:
			assert.False(t, ok, "closedChan should be closed")
		case <-time.After(2 * time.Second):
			t.Fatal("timeout waiting for readLoop exit")
		}
	})
}

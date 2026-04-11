// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package network

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lemon4ksan/g-man/pkg/log"
)

func TestWSConnection_FullCycle(t *testing.T) {
	handler := NewMockHandler()
	upgrader := websocket.Upgrader{}

	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/cmsocket/" {
			http.NotFound(w, r)
			return
		}

		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()

		for {
			mt, message, err := conn.ReadMessage()
			if err != nil {
				break
			}

			// Echo
			_ = conn.WriteMessage(mt, message)
		}
	}))
	defer server.Close()

	endpoint := strings.TrimPrefix(server.URL, "https://")

	dialer := &websocket.Dialer{
		TLSClientConfig: server.Client().Transport.(*http.Transport).TLSClientConfig,
	}

	client, err := NewWS(t.Context(), handler, log.Discard, endpoint, dialer)
	require.NoError(t, err)

	defer client.Close()

	testData := []byte("hello steam ws")
	err = client.Send(context.Background(), testData)
	assert.NoError(t, err)

	select {
	case msg := <-handler.msgChan:
		assert.Equal(t, testData, msg)
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for ws message")
	}

	_ = client.Close()

	time.Sleep(100 * time.Millisecond)
	handler.mu.Lock()
	assert.True(t, handler.closedCalled)
	handler.mu.Unlock()
}

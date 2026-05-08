// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package network

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/gorilla/websocket"

	"github.com/lemon4ksan/g-man/pkg/log"
)

var _ Connection = (*WS)(nil)

// WS implements a WebSocket-based connection.
// It leverages the gorilla/websocket library for handling the WebSocket protocol details.
type WS struct {
	BaseConnection

	conn    *websocket.Conn
	handler Handler
	logger  log.Logger

	writeMu   sync.Mutex // Protects conn for concurrent writes.
	closeOnce sync.Once  // Ensures Close actions are performed only once.
}

// NewWS establishes a WebSocket connection using the provided context.
func NewWS(
	ctx context.Context,
	handler Handler,
	logger log.Logger,
	endpoint string,
	dialer *websocket.Dialer,
) (*WS, error) {
	u := url.URL{Scheme: "wss", Host: endpoint, Path: "/cmsocket/"}

	if dialer == nil {
		dialer = &websocket.Dialer{
			HandshakeTimeout: 10 * time.Second,
			Proxy:            http.ProxyFromEnvironment,
		}
	}

	conn, resp, err := dialer.DialContext(ctx, u.String(), nil)
	if resp != nil {
		_ = resp.Body.Close()
	}

	if err != nil {
		return nil, fmt.Errorf("ws: dial failed: %w", err)
	}

	w := &WS{
		BaseConnection: NewBaseConnection("WS"),
		conn:           conn,
		handler:        handler,
		logger:         logger.With(log.String("transport", "WS"), log.String("endpoint", endpoint)),
	}

	go w.readLoop()

	return w, nil
}

// Name returns the transport identifier.
func (w *WS) Name() string { return "WS" }

// Send transmits data as a binary message over the WebSocket connection.
func (w *WS) Send(ctx context.Context, data []byte) error {
	w.writeMu.Lock()
	defer w.writeMu.Unlock()

	if w.conn == nil {
		return errors.New("ws: connection closed")
	}

	var err error
	if deadline, ok := ctx.Deadline(); ok {
		err = w.conn.SetWriteDeadline(deadline)
	} else {
		err = w.conn.SetWriteDeadline(time.Now().Add(WriteTimeout))
	}

	if err != nil {
		return err
	}

	return w.conn.WriteMessage(websocket.BinaryMessage, data)
}

// Close sends a standard WebSocket close frame and terminates the connection.
// It is safe to call multiple times.
func (w *WS) Close() error {
	var err error

	w.closeOnce.Do(func() {
		w.writeMu.Lock()
		defer w.writeMu.Unlock()

		if w.conn != nil {
			// Best-effort attempt to send a clean close message.
			msg := websocket.FormatCloseMessage(websocket.CloseNormalClosure, "")
			_ = w.conn.WriteMessage(websocket.CloseMessage, msg)
			err = w.conn.Close()
		}
	})

	return err
}

// readLoop runs in a dedicated goroutine, reading messages from the WebSocket.
func (w *WS) readLoop() {
	defer func() {
		_ = w.Close()
		w.handler.OnNetClose()
	}()

	for {
		msgType, data, err := w.conn.ReadMessage()
		if err != nil {
			// Filter out expected close errors to avoid noisy logs.
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseNormalClosure) {
				w.handler.OnNetError(err)
			}

			return
		}

		if msgType == websocket.BinaryMessage {
			w.handler.OnNetMessage(data)
		}
	}
}

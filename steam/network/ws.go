// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package network

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/lemon4ksan/g-man/log"
)

var _ Connection = (*WSConnection)(nil)

// WSConnection implements a WebSocket-based connection to a CM server.
type WSConnection struct {
	BaseConnection

	conn    *websocket.Conn
	handler Handler
	logger  log.Logger

	writeMu   sync.Mutex // Protects conn
	closeOnce sync.Once  // ensures Close is idempotent
}

func NewWSConnection(handler Handler, logger log.Logger, endpoint string) (*WSConnection, error) {
	u := url.URL{Scheme: "wss", Host: endpoint, Path: "/cmsocket/"}

	dialer := websocket.Dialer{
		HandshakeTimeout: 10 * time.Second,
		Proxy:            http.ProxyFromEnvironment,
	}

	conn, _, err := dialer.Dial(u.String(), nil)
	if err != nil {
		return nil, err
	}

	w := &WSConnection{
		BaseConnection: NewBaseConnection("WS"),
		conn:           conn,
		handler:        handler,
		logger:         logger.With(log.String("transport", "WS"), log.String("endpoint", endpoint)),
	}

	go w.readLoop()
	return w, nil
}

func (w *WSConnection) Name() string { return "WS" }

func (w *WSConnection) Send(ctx context.Context, data []byte) error {
	w.writeMu.Lock()
	defer w.writeMu.Unlock()

	if w.conn == nil {
		return fmt.Errorf("ws: connection closed")
	}

	if deadline, ok := ctx.Deadline(); ok {
		w.conn.SetWriteDeadline(deadline)
	} else {
		w.conn.SetWriteDeadline(time.Now().Add(WriteTimeout))
	}

	return w.conn.WriteMessage(websocket.BinaryMessage, data)
}

func (w *WSConnection) Close() error {
	var err error
	w.closeOnce.Do(func() {
		w.writeMu.Lock()
		defer w.writeMu.Unlock()

		if w.conn != nil {
			msg := websocket.FormatCloseMessage(websocket.CloseNormalClosure, "")
			w.conn.WriteMessage(websocket.CloseMessage, msg)
			err = w.conn.Close()
		}
	})
	return err
}

func (w *WSConnection) readLoop() {
	defer func() {
		w.Close()
		w.handler.OnNetClose()
	}()

	for {
		msgType, data, err := w.conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseNormalClosure, websocket.CloseAbnormalClosure) {
				w.handler.OnNetError(err)
			}
			return
		}

		if msgType == websocket.BinaryMessage {
			w.handler.OnNetMessage(data)
		}
	}
}

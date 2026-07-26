// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package network

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/lemon4ksan/aoni"
	"github.com/lemon4ksan/aoni/fast"
	"github.com/lemon4ksan/aoni/mod"
	"github.com/lemon4ksan/aoni/realtime/ws"
	"github.com/lemon4ksan/miyako/log"

	"github.com/lemon4ksan/g-man/internal/framer"
)

// ConnTypeWS is the connection type for WebSocket connections.
const ConnTypeWS = "WS"

var _ Connection = (*WS)(nil)

// wsConn defines the minimum interface required for a base WebSocket connection.
type wsConn interface {
	SetWriteDeadline(t time.Time) error
	WriteMessage(messageType int, data []byte) error
	Close() error
	NextReader() (messageType int, r io.Reader, err error)
}

// WS implements the [Connection] interface using the WebSocket protocol.
type WS struct {
	BaseConnection

	conn   wsConn
	logger log.Logger

	msgChan    chan Message
	errChan    chan error
	closedChan chan struct{}

	writeMu   sync.Mutex // Protects conn for concurrent writes.
	closeOnce sync.Once  // Ensures Close actions are performed only once.
}

// NewWS establishes a WebSocket connection to the specified endpoint.
func NewWS(
	ctx context.Context,
	logger log.Logger,
	endpoint, proxyURL string,
	headers http.Header,
) (*WS, error) {
	if !strings.Contains(endpoint, "://") {
		endpoint = "wss://" + endpoint
	}

	u, err := url.Parse(endpoint)
	if err != nil {
		return nil, NewError(OpDial, ConnTypeWS, err)
	}

	switch u.Scheme {
	case "http":
		u.Scheme = "ws"
	case "https":
		u.Scheme = "wss"
	}

	dialer := &websocket.Dialer{
		HandshakeTimeout: 10 * time.Second,
		Proxy:            http.ProxyFromEnvironment,
	}

	if proxyURL != "" {
		pu, err := url.Parse(proxyURL)
		if err != nil {
			return nil, NewError(OpProxy, ConnTypeWS, err)
		}

		dialer.Proxy = http.ProxyURL(pu)
	}

	conn, resp, err := dialer.DialContext(ctx, u.String(), headers)
	if resp != nil {
		_ = resp.Body.Close()
	}

	if err != nil {
		return nil, NewError(OpDial, ConnTypeWS, err)
	}

	w := &WS{
		BaseConnection: NewBaseConnection(ConnTypeWS),
		conn:           conn,
		logger:         logger.With(log.String("transport", ConnTypeWS), log.String("endpoint", endpoint)),
		msgChan:        make(chan Message, 100),
		errChan:        make(chan error, 10),
		closedChan:     make(chan struct{}),
	}

	go w.readLoop()

	return w, nil
}

// NewWSWithFastClient establishes a WebSocket connection using fast.Client.
func NewWSWithFastClient(
	ctx context.Context,
	logger log.Logger,
	endpoint, proxyURL string,
	headers http.Header,
	fastClient *fast.Client,
) (*WS, error) {
	if fastClient == nil {
		return NewWS(ctx, logger, endpoint, proxyURL, headers)
	}

	reqMods := make([]aoni.RequestModifier, 0, len(headers)+1)
	if proxyURL != "" {
		reqMods = append(reqMods, mod.WithProxyOverride(proxyURL))
	}

	for k, vv := range headers {
		for _, v := range vv {
			reqMods = append(reqMods, mod.WithHeader(k, v))
		}
	}

	conn, _, err := ws.DialWebSocket(ctx, fastClient, endpoint, reqMods...) //nolint:bodyclose
	if err != nil {
		return nil, NewError(OpDial, ConnTypeWS, err)
	}

	wsConnAdapter, ok := conn.(wsConn)
	if !ok {
		_ = conn.Close()
		return nil, NewError(OpDial, ConnTypeWS, errors.New("websocket connection does not satisfy wsConn interface"))
	}

	w := &WS{
		BaseConnection: NewBaseConnection(ConnTypeWS),
		conn:           wsConnAdapter,
		logger:         logger.With(log.String("transport", ConnTypeWS), log.String("endpoint", endpoint)),
		msgChan:        make(chan Message, 100),
		errChan:        make(chan error, 10),
		closedChan:     make(chan struct{}),
	}

	go w.readLoop()

	return w, nil
}

// Name returns the protocol name [ConnTypeWS].
func (w *WS) Name() string { return ConnTypeWS }

// Messages returns a channel that receives incoming binary messages from the WebSocket.
func (w *WS) Messages() <-chan Message { return w.msgChan }

// Errors returns a channel that receives non-fatal errors from the WebSocket read loop.
func (w *WS) Errors() <-chan error { return w.errChan }

// Closed returns a channel that is closed once the WebSocket connection has terminated.
func (w *WS) Closed() <-chan struct{} { return w.closedChan }

// Send transmits the message payload as a binary frame over the WebSocket.
func (w *WS) Send(ctx context.Context, data []byte) error {
	w.writeMu.Lock()
	defer w.writeMu.Unlock()

	if w.conn == nil {
		return NewError(OpSend, ConnTypeWS, errors.New("connection closed"))
	}

	var err error
	if deadline, ok := ctx.Deadline(); ok {
		err = w.conn.SetWriteDeadline(deadline)
	} else {
		err = w.conn.SetWriteDeadline(time.Now().Add(WriteTimeout))
	}

	if err != nil {
		return NewError(OpDeadline, ConnTypeWS, err)
	}

	if err := w.conn.WriteMessage(websocket.BinaryMessage, data); err != nil {
		return NewError(OpSend, ConnTypeWS, err)
	}

	return nil
}

// Close gracefully closes the WebSocket connection.
func (w *WS) Close() error {
	var err error

	w.closeOnce.Do(func() {
		w.writeMu.Lock()
		defer w.writeMu.Unlock()

		if w.conn != nil {
			msg := websocket.FormatCloseMessage(websocket.CloseNormalClosure, "")
			_ = w.conn.WriteMessage(websocket.CloseMessage, msg)
			err = w.conn.Close()
		}
	})

	if err != nil {
		return NewError(OpClose, ConnTypeWS, err)
	}

	return nil
}

// readLoop runs in a dedicated goroutine, reading messages zero-alloc into pooled buffers.
func (w *WS) readLoop() {
	defer func() {
		_ = w.Close()
		close(w.closedChan)
		close(w.msgChan)
		close(w.errChan)
	}()

	for {
		msgType, r, err := w.conn.NextReader()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseNormalClosure) {
				select {
				case w.errChan <- NewError(OpRead, ConnTypeWS, err):
				default:
				}
			}

			return
		}

		if msgType != websocket.BinaryMessage {
			_, _ = io.Copy(io.Discard, r)
			continue
		}

		fb := framer.AcquireFrameBuffer(0)
		buf := bytes.NewBuffer(fb.B[:0])
		_, err = io.Copy(buf, r)
		fb.B = buf.Bytes()

		if err != nil {
			framer.ReleaseFrameBuffer(fb)

			select {
			case w.errChan <- NewError(OpRead, ConnTypeWS, err):
			default:
			}

			return
		}

		select {
		case w.msgChan <- fb:
		case <-w.closedChan:
			framer.ReleaseFrameBuffer(fb)
			return
		}
	}
}

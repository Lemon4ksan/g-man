// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package network

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"

	"github.com/lemon4ksan/g-man/pkg/log"
)

// ConnTypeWS is the connection type for WebSocket connections.
const ConnTypeWS = "WS"

var _ Connection = (*WS)(nil)

// wsConn defines the minimum interface required for a base WebSocket connection.
// This allows for isolation and testing of method behavior under various error conditions.
type wsConn interface {
	SetWriteDeadline(t time.Time) error
	WriteMessage(messageType int, data []byte) error
	Close() error
	ReadMessage() (messageType int, p []byte, err error)
}

// WS implements the [Connection] interface using the WebSocket protocol.
//
// It wraps gorilla/websocket to handle connection establishment, message framing,
// and WebSocket close handshake rules. All WebSocket operations are thread-safe and
// respect contexts and write deadlines.
//
// Create new instances of WS using the [NewWS] constructor.
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
//
// The endpoint should be a valid URL. If the endpoint does not specify a scheme,
// it defaults to "wss://". Scheme "http" is normalized to "ws" and "https" to "wss".
//
// If proxyURL is not empty, the WebSocket dialer will route the handshake request
// through the specified HTTP proxy. Handshake headers can be optionally provided
// via the headers argument.
//
// If the context ctx is canceled before or during connection establishment,
// NewWS cancels the dial and returns the context error.
// If connection or handshake fails, it returns an *[Error] with [OpDial].
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

// Name returns the protocol name [ConnTypeWS].
func (w *WS) Name() string { return ConnTypeWS }

// Messages returns a channel that receives incoming binary messages from the WebSocket.
// The channel is closed when the connection is terminated.
func (w *WS) Messages() <-chan Message { return w.msgChan }

// Errors returns a channel that receives non-fatal errors from the WebSocket read loop.
// The channel is closed when the connection is terminated.
func (w *WS) Errors() <-chan error { return w.errChan }

// Closed returns a channel that is closed once the WebSocket connection has terminated
// and all cleanup is complete.
func (w *WS) Closed() <-chan struct{} { return w.closedChan }

// Send transmits the message payload as a binary frame over the WebSocket.
//
// Send blocks until the write completes, the context is canceled, or the write deadline
// is reached. It returns an *[Error] if write deadline configuration or write operation fails.
// This method is safe for concurrent use.
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

// Close gracefully closes the WebSocket connection by sending a CloseNormalClosure frame.
//
// Close is idempotent and thread-safe. Subsequent calls return nil or the original close error.
// Closing the connection triggers cleanup of all channels and background goroutines.
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

	if err != nil {
		return NewError(OpClose, ConnTypeWS, err)
	}

	return nil
}

// readLoop runs in a dedicated goroutine, reading messages from the WebSocket.
func (w *WS) readLoop() {
	defer func() {
		_ = w.Close()
		close(w.closedChan)
		close(w.msgChan)
		close(w.errChan)
	}()

	for {
		msgType, data, err := w.conn.ReadMessage()
		if err != nil {
			// Filter out expected close errors to avoid noisy logs.
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseNormalClosure) {
				select {
				case w.errChan <- NewError(OpRead, ConnTypeWS, err):
				default:
				}
			}

			return
		}

		if msgType == websocket.BinaryMessage {
			select {
			case w.msgChan <- data:
			case <-w.closedChan:
				return
			}
		}
	}
}

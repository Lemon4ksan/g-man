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

	"github.com/lemon4ksan/aoni"
	"github.com/lemon4ksan/aoni/fast"
	"github.com/lemon4ksan/aoni/mod"
	"github.com/lemon4ksan/aoni/option"
	"github.com/lemon4ksan/aoni/realtime/ws"
	"github.com/lemon4ksan/foundation/async/log"

	"github.com/lemon4ksan/g-man/internal/framer"
)

// ConnTypeWS labels WebSocket transport connections.
const ConnTypeWS = "WS"

var _ Connection = (*WS)(nil)

var (
	ErrWSConnTypeMismatch = errors.New("ws: connection does not satisfy wsConn interface")
	ErrWSConnectionClosed = errors.New("ws: connection closed")
)

type wsConn interface {
	SetWriteDeadline(t time.Time) error
	WriteMessage(messageType int, data []byte) error
	Close() error
	ReadMessage() (messageType int, payload []byte, err error)
}

// WS implements Connection over WebSocket protocols.
type WS struct {
	BaseConnection

	conn   wsConn
	logger log.Logger

	msgChan    chan Message
	errChan    chan error
	closedChan chan struct{}

	writeMu   sync.Mutex
	closeOnce sync.Once
}

// NewWS establishes a WebSocket connection to the specified endpoint.
func NewWS(
	ctx context.Context,
	logger log.Logger,
	endpoint, proxyURL string,
	headers http.Header,
) (*WS, error) {
	return NewWSWithClient(ctx, logger, endpoint, proxyURL, headers, nil)
}

// NewWSWithClient establishes a WebSocket connection using a custom aoni.WebSocketDialer client.
func NewWSWithClient(
	ctx context.Context,
	logger log.Logger,
	endpoint, proxyURL string,
	headers http.Header,
	dialerClient aoni.WebSocketDialer,
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

	if proxyURL != "" {
		if _, err := url.Parse(proxyURL); err != nil {
			return nil, NewError(OpProxy, ConnTypeWS, err)
		}
	}

	if dialerClient == nil {
		opts := make([]aoni.ClientOption, 0, 2)
		if proxyURL != "" {
			opts = append(opts, option.WithProxyString(proxyURL))
		}
		dialerClient = NewClient(nil, opts...)
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

	conn, resp, err := ws.DialWebSocket(ctx, dialerClient, u.String(), reqMods...)
	if resp != nil && resp.Body != nil {
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
		return NewWSWithClient(ctx, logger, endpoint, proxyURL, headers, nil)
	}

	return NewWSWithClient(ctx, logger, endpoint, proxyURL, headers, fastClient)
}

// Name returns protocol label "WS".
func (w *WS) Name() string { return ConnTypeWS }

// Messages returns channel receiving incoming binary WebSocket frames.
func (w *WS) Messages() <-chan Message { return w.msgChan }

// Errors returns channel receiving non-fatal read errors.
func (w *WS) Errors() <-chan error { return w.errChan }

// Closed returns channel closed upon connection termination.
func (w *WS) Closed() <-chan struct{} { return w.closedChan }

// Send transmits binary frames over the WebSocket connection.
func (w *WS) Send(ctx context.Context, data []byte) error {
	w.writeMu.Lock()
	defer w.writeMu.Unlock()

	if w.conn == nil {
		return NewError(OpSend, ConnTypeWS, ErrWSConnectionClosed)
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

	if err := w.conn.WriteMessage(ws.FrameBinary, data); err != nil {
		return NewError(OpSend, ConnTypeWS, err)
	}

	return nil
}

// Close gracefully closes the WebSocket session.
func (w *WS) Close() error {
	var err error

	w.closeOnce.Do(func() {
		w.writeMu.Lock()
		defer w.writeMu.Unlock()

		if w.conn != nil {
			_ = w.conn.WriteMessage(ws.FrameClose, nil)
			err = w.conn.Close()
		}
	})

	if err != nil {
		return NewError(OpClose, ConnTypeWS, err)
	}

	return nil
}

func (w *WS) readLoop() {
	defer func() {
		_ = w.Close()
		close(w.closedChan)
		close(w.msgChan)
		close(w.errChan)
	}()

	for {
		msgType, payload, err := w.conn.ReadMessage()
		if err != nil {
			select {
			case w.errChan <- NewError(OpRead, ConnTypeWS, err):
			default:
			}
			return
		}

		if msgType != ws.FrameBinary {
			continue
		}

		fb := framer.AcquireFrameBuffer(len(payload))
		copy(fb.B, payload)

		select {
		case w.msgChan <- fb:
		case <-w.closedChan:
			framer.ReleaseFrameBuffer(fb)
			return
		}
	}
}

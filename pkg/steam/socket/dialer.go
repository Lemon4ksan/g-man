// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package socket

import (
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"net/http"
	"time"

	"github.com/lemon4ksan/aoni/fast"
	"github.com/lemon4ksan/aoni-contrib/socket"
	"github.com/lemon4ksan/aoni-contrib/socket/connector"
	"github.com/lemon4ksan/aoni-contrib/socket/processor"
	log "github.com/lemon4ksan/foundation/async/logkit"

	"github.com/lemon4ksan/g-man/internal/network"
)

type (
	Dialer          = connector.Dialer[CMServer]
	ReconnectPolicy = connector.ReconnectPolicy[CMServer]
	ProcessorConfig = processor.Config
)

// ConnectorConfig configures connection dialing, reconnect policies, and proxy routes.
type ConnectorConfig struct {
	FastClient      *fast.Client
	Dialers         map[string]Dialer
	ReconnectPolicy ReconnectPolicy
	ConnectTimeout  time.Duration
	ProxyURL        string
	Headers         http.Header
	Logger          log.Logger
}

// DefaultReconnectPolicy returns standard auto-reconnection settings.
func DefaultReconnectPolicy() ReconnectPolicy {
	return connector.DefaultReconnectPolicy[CMServer]()
}

// DefaultProcessorConfig returns standard processor worker pool settings.
func DefaultProcessorConfig() ProcessorConfig {
	return processor.DefaultConfig()
}

// DefaultConnectorConfig constructs standard CM server connection options.
func DefaultConnectorConfig() ConnectorConfig {
	return ConnectorConfig{
		Dialers:         DefaultDialers(),
		ReconnectPolicy: DefaultReconnectPolicy(),
		ConnectTimeout:  20 * time.Second,
	}
}

type legacyConnAdapter struct {
	conn network.Connection
}

func (a *legacyConnAdapter) Send(ctx context.Context, data []byte) error {
	return a.conn.Send(ctx, data)
}

func (a *legacyConnAdapter) Receive(ctx context.Context) (*socket.FrameBuffer, error) {
	select {
	case msg, ok := <-a.conn.Messages():
		if !ok {
			return nil, io.EOF
		}

		return msg, nil

	case err := <-a.conn.Errors():
		if err == nil {
			return nil, io.EOF
		}

		return nil, err

	case <-a.conn.Closed():
		return nil, io.EOF

	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (a *legacyConnAdapter) Close() error {
	return a.conn.Close()
}

// DefaultDialers initializes TCP and WebSocket dialers without proxy.
func DefaultDialers() map[string]Dialer {
	return NewDialers("")
}

// NewDialers initializes TCP and WebSocket dialers configured with an optional proxy URL.
func NewDialers(proxyURL string) map[string]Dialer {
	return NewDialersWithLogger(proxyURL, log.Discard)
}

// NewDialersWithLogger initializes TCP and WebSocket dialers configured with an optional proxy URL and custom logger.
func NewDialersWithLogger(proxyURL string, logger log.Logger) map[string]Dialer {
	if logger == nil {
		logger = log.Discard
	}

	wsDialer := func(ctx context.Context, endpoint CMServer, _ socket.Framer, _ socket.Cipher) (connector.Connection, error) {
		wsConn, err := network.NewWS(ctx, logger, endpoint.Endpoint, proxyURL, nil)
		if err != nil {
			return nil, err
		}

		return &legacyConnAdapter{conn: wsConn}, nil
	}

	tcpDialer := func(ctx context.Context, endpoint CMServer, framer socket.Framer, cipher socket.Cipher) (connector.Connection, error) {
		var (
			conn net.Conn
			err  error
		)

		if proxyURL != "" {
			conn, err = network.NewProxyConn(ctx, proxyURL, endpoint.Endpoint)
		} else {
			var d net.Dialer

			conn, err = d.DialContext(ctx, "tcp", endpoint.Endpoint)
		}

		if err != nil {
			return nil, fmt.Errorf("tcp dial: %w", err)
		}

		if framer == nil {
			framer = socket.NewLengthPrefixedFramer(socket.LengthPrefixedConfig{
				ByteOrder: binary.LittleEndian,
				Magic:     []byte("VT01"),
				MaxLength: 10 * 1024 * 1024,
			})
		}

		return connector.NewNetConnWrapper(conn, framer, cipher), nil
	}

	return map[string]Dialer{
		"tcp":        tcpDialer,
		"netfilter":  tcpDialer,
		"websocket":  wsDialer,
		"websockets": wsDialer,
		"ws":         wsDialer,
		"wss":        wsDialer,
	}
}

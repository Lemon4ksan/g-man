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
	"github.com/lemon4ksan/aoni/realtime/socket"
	"github.com/lemon4ksan/aoni/realtime/socket/connector"
	"github.com/lemon4ksan/aoni/realtime/socket/processor"
	"github.com/lemon4ksan/miyako/log"

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

// DefaultDialers initializes TCP and WebSocket dialers.
func DefaultDialers() map[string]Dialer {
	return map[string]Dialer{
		"tcp": func(ctx context.Context, endpoint CMServer, framer socket.Framer, cipher socket.Cipher) (connector.Connection, error) {
			var d net.Dialer

			conn, err := d.DialContext(ctx, "tcp", endpoint.Endpoint)
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
		},
		"websocket": func(ctx context.Context, endpoint CMServer, _ socket.Framer, _ socket.Cipher) (connector.Connection, error) {
			wsConn, err := network.NewWS(ctx, log.Discard, endpoint.Endpoint, "", nil)
			if err != nil {
				return nil, err
			}

			return &legacyConnAdapter{conn: wsConn}, nil
		},
	}
}

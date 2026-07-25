// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package network

import (
	"bufio"
	"context"
	"errors"
	"io"
	"net"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/lemon4ksan/miyako/log"
	"golang.org/x/net/proxy"
)

const (
	// ConnTypeTCP is the connection type for TCP connections.
	ConnTypeTCP = "TCP"

	// WriteTimeout is the default duration to wait for a write operation
	// to complete before timing out.
	WriteTimeout = 5 * time.Second
)

// Compile-time checks to ensure interface satisfaction.
var (
	_ Connection  = (*TCP)(nil)
	_ Encryptable = (*TCP)(nil)
)

// TCP implements the [Connection] interface using stream-oriented TCP sockets.
//
// It handles raw framing over TCP by utilizing a [Framer] implementation, and
// supports message-level encryption and decryption if configured with a [Cipher].
// All TCP operations are thread-safe and respect contexts and timeouts.
//
// Create new instances of TCP using the [NewTCP] constructor.
type TCP struct {
	BaseConnection
	conn   net.Conn
	logger log.Logger
	framer Framer

	msgChan    chan Message
	errChan    chan error
	closedChan chan struct{}

	writeMu sync.Mutex   // Ensures atomic writes.
	keyMu   sync.RWMutex // Protects cipher for concurrent reads/writes.
	cipher  Cipher
}

// NewTCP establishes a TCP connection to the given endpoint and starts a
// background read loop to receive messages.
//
// The endpoint should be a host address or host:port combination. If proxyURL
// is not empty, the connection is routed through the specified SOCKS5 or HTTP
// proxy.
//
// The framer cannot be nil and is used to frame outgoing messages and unframe
// incoming stream packets.
//
// If the context ctx is canceled before or during connection establishment,
// NewTCP cancels the dial and returns the context error.
// If the connection establishment fails, it returns an *[Error] with [OpDial].
func NewTCP(
	ctx context.Context,
	logger log.Logger,
	endpoint, proxyURL string,
	framer Framer,
) (*TCP, error) {
	if framer == nil {
		return nil, NewError(OpFramer, ConnTypeTCP, errors.New("framer cannot be nil"))
	}

	var (
		conn net.Conn
		err  error
	)

	if proxyURL != "" {
		conn, err = newProxyConn(ctx, proxyURL, endpoint)
	} else {
		conn, err = new(net.Dialer).DialContext(ctx, "tcp", endpoint)
	}

	if err != nil {
		return nil, NewError(OpDial, ConnTypeTCP, err)
	}

	t := &TCP{
		BaseConnection: NewBaseConnection(ConnTypeTCP),
		conn:           conn,
		logger:         logger.With(log.String("transport", ConnTypeTCP), log.String("endpoint", endpoint)),
		framer:         framer,
		msgChan:        make(chan Message, 100),
		errChan:        make(chan error, 10),
		closedChan:     make(chan struct{}),
	}

	go t.readLoop()

	return t, nil
}

// Name returns the protocol name [ConnTypeTCP].
func (t *TCP) Name() string { return ConnTypeTCP }

// Messages returns a channel that receives framed messages from the TCP connection.
// The channel is closed when the connection is terminated.
func (t *TCP) Messages() <-chan Message { return t.msgChan }

// Errors returns a channel that receives non-fatal errors from the TCP read/write loop.
// The channel is closed when the connection is terminated.
func (t *TCP) Errors() <-chan error { return t.errChan }

// Closed returns a channel that is closed once the TCP connection has terminated
// and all cleanup is complete.
func (t *TCP) Closed() <-chan struct{} { return t.closedChan }

// SetCipher configures the TCP connection to use the provided [Cipher] for encrypting
// all future outgoing messages and decrypting incoming ones.
// Pass nil to disable encryption on the connection.
//
// It returns true once the cipher is applied. This method is safe for concurrent use.
func (t *TCP) SetCipher(c Cipher) bool {
	t.keyMu.Lock()
	t.cipher = c
	t.keyMu.Unlock()
	t.logger.Debug("Encryption enabled")

	return true
}

// Send encrypts (if a cipher is configured), frames, and writes the message payload
// to the underlying TCP socket.
//
// Send blocks until the write completes, the context is canceled, or the write deadline
// is reached. It returns an *[Error] if encryption, deadline setting, framing, or
// writing fails. This method is safe for concurrent use.
func (t *TCP) Send(ctx context.Context, data []byte) error {
	if err := ctx.Err(); err != nil {
		return NewError(OpSend, ConnTypeTCP, err)
	}

	t.keyMu.RLock()
	cipher := t.cipher
	t.keyMu.RUnlock()

	var err error
	if cipher != nil {
		data, err = cipher.Encrypt(data)
		if err != nil {
			return NewError(OpEncrypt, ConnTypeTCP, err)
		}
	}

	t.writeMu.Lock()
	defer t.writeMu.Unlock()

	deadline, ok := ctx.Deadline()
	if !ok {
		deadline = time.Now().Add(WriteTimeout)
	}

	if err := t.conn.SetWriteDeadline(deadline); err != nil {
		return NewError(OpDeadline, ConnTypeTCP, err)
	}

	if err := t.framer.WriteFrame(t.conn, data); err != nil {
		return NewError(OpFramer, ConnTypeTCP, err)
	}

	return nil
}

// Close gracefully closes the connection, terminating the underlying TCP socket.
//
// It is idempotent and safe to call concurrently. Closing the connection triggers
// cleanup of all channels and background goroutines.
func (t *TCP) Close() error {
	if t.conn == nil {
		return nil
	}

	return t.conn.Close()
}

// readLoop runs in a dedicated goroutine, continuously reading and decoding packets.
func (t *TCP) readLoop() {
	defer func() {
		_ = t.conn.Close()
		close(t.closedChan)
		close(t.msgChan)
		close(t.errChan)
	}()

	sendErr := func(err error) {
		select {
		case t.errChan <- err:
		default:
		}
	}

	reader := bufio.NewReaderSize(t.conn, 64*1024)

	for {
		payload, err := t.framer.ReadFrame(reader)
		if err != nil {
			if !isIgnorableError(err) {
				sendErr(NewError(OpFramer, ConnTypeTCP, err))
			}

			return
		}

		t.keyMu.RLock()
		cipher := t.cipher
		t.keyMu.RUnlock()

		if cipher != nil {
			payload, err = cipher.Decrypt(payload)
			if err != nil {
				sendErr(NewError(OpDecrypt, ConnTypeTCP, err))
				continue
			}
		}

		select {
		case t.msgChan <- payload:
		case <-t.closedChan:
			return
		}
	}
}

// newProxyConn dials the given endpoint through the specified proxy URL.
func newProxyConn(ctx context.Context, proxyURL, endpoint string) (conn net.Conn, err error) {
	u, err := url.Parse(proxyURL)
	if err != nil {
		return nil, NewError(OpProxy, ConnTypeTCP, err)
	}

	dialer, err := proxy.FromURL(u, proxy.Direct)
	if err != nil {
		return nil, NewError(OpProxy, ConnTypeTCP, err)
	}

	if contextDialer, ok := dialer.(proxy.ContextDialer); ok {
		conn, err = contextDialer.DialContext(ctx, "tcp", endpoint)
	} else {
		conn, err = dialer.Dial("tcp", endpoint)
	}

	if err != nil {
		return nil, NewError(OpDial, ConnTypeTCP, err)
	}

	return conn, err
}

// isIgnorableError returns true if the error indicates a standard, expected
// connection termination (such as EOF or a closed connection) which should not
// be reported as a failure to the user.
func isIgnorableError(err error) bool {
	if err == nil {
		return true
	}

	// Robust check for closure/EOF across different platforms/types of conns
	if errors.Is(err, net.ErrClosed) || errors.Is(err, io.EOF) || errors.Is(err, io.ErrClosedPipe) {
		return true
	}

	s := err.Error()

	return strings.Contains(s, "use of closed") || strings.Contains(s, "closed pipe")
}

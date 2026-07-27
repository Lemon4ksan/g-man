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

	"github.com/lemon4ksan/aoni"
	"github.com/lemon4ksan/aoni/mod"
	"github.com/lemon4ksan/miyako/log"
	"golang.org/x/net/proxy"

	"github.com/lemon4ksan/g-man/internal/framer"
)

const (
	// ConnTypeTCP labels standard TCP transport connections.
	ConnTypeTCP = "TCP"

	// WriteTimeout defines default write deadline durations.
	WriteTimeout = 5 * time.Second
)

var (
	_ Connection  = (*TCP)(nil)
	_ Encryptable = (*TCP)(nil)
)

var ErrNilFramer = errors.New("tcp: framer cannot be nil")

// TCP implements Connection over standard TCP sockets with asynchronous background reading and framing.
type TCP struct {
	BaseConnection
	conn   net.Conn
	logger log.Logger
	framer Framer

	msgChan    chan Message
	errChan    chan error
	closedChan chan struct{}

	writeMu sync.Mutex
	keyMu   sync.RWMutex
	cipher  Cipher
}

// NewTCP dials a TCP endpoint (optionally via proxy) and starts background framing loops.
func NewTCP(
	ctx context.Context,
	logger log.Logger,
	endpoint, proxyURL string,
	framer Framer,
) (*TCP, error) {
	if framer == nil {
		return nil, NewError(OpFramer, ConnTypeTCP, ErrNilFramer)
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

// NewTCPWithDialer dials a TCP connection using a custom dialer function.
func NewTCPWithDialer(
	ctx context.Context,
	logger log.Logger,
	endpoint, proxyURL string,
	framer Framer,
	dialFunc func(ctx context.Context, network, addr string) (net.Conn, error),
) (*TCP, error) {
	if framer == nil {
		return nil, NewError(OpFramer, ConnTypeTCP, ErrNilFramer)
	}

	if dialFunc == nil {
		return NewTCP(ctx, logger, endpoint, proxyURL, framer)
	}

	if proxyURL != "" {
		ctx = aoni.WithContextModifier(ctx, mod.WithProxyOverride(proxyURL))
	}

	conn, err := dialFunc(ctx, "tcp", endpoint)
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

// Name returns protocol label "TCP".
func (t *TCP) Name() string { return ConnTypeTCP }

// Messages returns channel receiving framed inbound messages.
func (t *TCP) Messages() <-chan Message { return t.msgChan }

// Errors returns channel receiving non-fatal background reading errors.
func (t *TCP) Errors() <-chan error { return t.errChan }

// Closed returns channel closed when the TCP connection terminates.
func (t *TCP) Closed() <-chan struct{} { return t.closedChan }

// SetCipher configures dynamic payload encryption/decryption for subsequent frames.
func (t *TCP) SetCipher(c Cipher) bool {
	t.keyMu.Lock()
	t.cipher = c
	t.keyMu.Unlock()
	t.logger.Debug("Encryption enabled")

	return true
}

// Send encrypts (if cipher configured), frames, and writes payload data to the TCP socket.
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

// Close gracefully closes the underlying TCP socket.
func (t *TCP) Close() error {
	if t.conn == nil {
		return nil
	}

	return t.conn.Close()
}

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
		rawFB, err := t.framer.ReadFrame(reader)
		if err != nil {
			if !isIgnorableError(err) {
				sendErr(NewError(OpFramer, ConnTypeTCP, err))
			}

			return
		}

		t.keyMu.RLock()
		cipher := t.cipher
		t.keyMu.RUnlock()

		var finalFB *framer.FrameBuffer

		if cipher != nil {
			decryptedFB, err := cipher.Decrypt(rawFB)
			framer.ReleaseFrameBuffer(rawFB)

			if err != nil {
				sendErr(NewError(OpDecrypt, ConnTypeTCP, err))
				continue
			}

			finalFB = decryptedFB
		} else {
			finalFB = rawFB
		}

		select {
		case t.msgChan <- finalFB:
		case <-t.closedChan:
			framer.ReleaseFrameBuffer(finalFB)
			return
		}
	}
}

func newProxyConn(ctx context.Context, proxyURL, endpoint string) (net.Conn, error) {
	u, err := url.Parse(proxyURL)
	if err != nil {
		return nil, NewError(OpProxy, ConnTypeTCP, err)
	}

	dialer, err := proxy.FromURL(u, proxy.Direct)
	if err != nil {
		return nil, NewError(OpProxy, ConnTypeTCP, err)
	}

	var conn net.Conn
	if contextDialer, ok := dialer.(proxy.ContextDialer); ok {
		conn, err = contextDialer.DialContext(ctx, "tcp", endpoint)
	} else {
		conn, err = dialer.Dial("tcp", endpoint)
	}

	if err != nil {
		return nil, NewError(OpDial, ConnTypeTCP, err)
	}

	return conn, nil
}

func isIgnorableError(err error) bool {
	if err == nil {
		return true
	}

	if errors.Is(err, net.ErrClosed) || errors.Is(err, io.EOF) || errors.Is(err, io.ErrClosedPipe) {
		return true
	}

	s := err.Error()

	return strings.Contains(s, "use of closed") || strings.Contains(s, "closed pipe")
}

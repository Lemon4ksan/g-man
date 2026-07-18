// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package network

import (
	"context"
	"errors"
	"io"
	"net"
	"net/url"
	"testing"
	"time"

	"github.com/lemon4ksan/miyako/log"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/net/proxy"
)

type mockFramer struct {
	readFunc  func(r io.Reader) ([]byte, error)
	writeFunc func(w io.Writer, data []byte) error
}

func (m mockFramer) ReadFrame(r io.Reader) ([]byte, error) {
	if m.readFunc != nil {
		return m.readFunc(r)
	}

	return nil, io.EOF
}

func (m mockFramer) WriteFrame(w io.Writer, data []byte) error {
	if m.writeFunc != nil {
		return m.writeFunc(w, data)
	}

	return nil
}

type mockCipher struct {
	encFunc func(data []byte) ([]byte, error)
	decFunc func(data []byte) ([]byte, error)
}

func (m mockCipher) Encrypt(data []byte) ([]byte, error) {
	if m.encFunc != nil {
		return m.encFunc(data)
	}

	return data, nil
}

func (m mockCipher) Decrypt(data []byte) ([]byte, error) {
	if m.decFunc != nil {
		return m.decFunc(data)
	}

	return data, nil
}

type deadlineFailingConn struct {
	net.Conn
}

func (deadlineFailingConn) SetWriteDeadline(time.Time) error {
	return errors.New("deadline failed")
}

func (deadlineFailingConn) Close() error {
	return nil
}

type mockNetConn struct {
	net.Conn
}

func (mockNetConn) SetWriteDeadline(time.Time) error {
	return nil
}

func (mockNetConn) Close() error {
	return nil
}

type mockSimpleDialer struct{}

func (mockSimpleDialer) Dial(network, addr string) (net.Conn, error) {
	return nil, errors.New("mock simple dial error")
}

func init() {
	proxy.RegisterDialerType("mocksock", func(u *url.URL, d proxy.Dialer) (proxy.Dialer, error) {
		return mockSimpleDialer{}, nil
	})
}

func TestBaseConnection(t *testing.T) {
	t.Parallel()

	b1 := NewBaseConnection("test")
	b2 := NewBaseConnection("test")

	assert.True(t, b1.ID() > 0)
	assert.Equal(t, b1.ID()+1, b2.ID())
}

func TestTCP_NewTCP_Fail(t *testing.T) {
	t.Parallel()

	_, err := NewTCP(t.Context(), log.Discard, "127.0.0.1:1", "", mockFramer{})
	assert.Error(t, err)

	_, err = NewTCP(t.Context(), log.Discard, "127.0.0.1:1", "", nil)
	assert.ErrorContains(t, err, "framer cannot be nil")
}

func TestTCP_NewTCP_ProxyErrors(t *testing.T) {
	t.Parallel()

	t.Run("invalid_proxy_url", func(t *testing.T) {
		t.Parallel()
		// unescapeable percentage sign to trigger url.Parse error
		_, err := NewTCP(t.Context(), log.Discard, "127.0.0.1:1", "https://%", mockFramer{})
		assert.Error(t, err)
	})

	t.Run("unsupported_proxy_scheme", func(t *testing.T) {
		t.Parallel()
		// ftp scheme is not supported by proxy.FromURL
		_, err := NewTCP(t.Context(), log.Discard, "127.0.0.1:1", "ftp://localhost", mockFramer{})
		assert.Error(t, err)
	})

	t.Run("custom_proxy_without_context_dialer", func(t *testing.T) {
		t.Parallel()
		_, err := NewTCP(t.Context(), log.Discard, "127.0.0.1:1", "mocksock://localhost", mockFramer{})
		assert.ErrorContains(t, err, "mock simple dial error")
	})
}

func TestTCP_Name(t *testing.T) {
	t.Parallel()

	tcp := &TCP{}
	assert.Equal(t, "TCP", tcp.Name())
	assert.Nil(t, tcp.Closed())
	assert.Nil(t, tcp.Messages())
	assert.Nil(t, tcp.Errors())
}

func TestTCP_Close_NilConn(t *testing.T) {
	t.Parallel()

	tcp := &TCP{conn: nil}
	assert.NoError(t, tcp.Close())
}

func TestTCP_ReadLoop_Coverage(t *testing.T) {
	t.Parallel()

	logger := log.Discard

	t.Run("decryption_branch", func(t *testing.T) {
		t.Parallel()

		s, c := net.Pipe()
		tcp := &TCP{
			conn:           c,
			logger:         logger,
			BaseConnection: NewBaseConnection("TCP"),
			framer: mockFramer{
				readFunc: func(r io.Reader) ([]byte, error) {
					b := make([]byte, 10)
					_, err := r.Read(b)
					return b, err
				},
			},
			msgChan:    make(chan Message, 10),
			errChan:    make(chan error, 10),
			closedChan: make(chan struct{}),
		}

		cipher := mockCipher{
			decFunc: func(data []byte) ([]byte, error) {
				return nil, errors.New("decrypt failed")
			},
		}
		tcp.SetCipher(cipher)

		go tcp.readLoop()

		_, _ = s.Write([]byte("some data!"))

		select {
		case err := <-tcp.Errors():
			assert.ErrorContains(t, err, "tcp: decrypt failed")
		case <-time.After(time.Second):
			t.Fatal("timeout")
		}
	})

	t.Run("framer_error", func(t *testing.T) {
		t.Parallel()

		_, c := net.Pipe()

		tcp := &TCP{
			conn:   c,
			logger: log.Discard,
			framer: mockFramer{
				readFunc: func(r io.Reader) ([]byte, error) {
					return nil, errors.New("invalid frame")
				},
			},
			msgChan:    make(chan Message, 10),
			errChan:    make(chan error, 10),
			closedChan: make(chan struct{}),
		}
		go tcp.readLoop()

		select {
		case err := <-tcp.Errors():
			assert.ErrorContains(t, err, "invalid frame")
		case <-time.After(time.Second):
			t.Fatal("timeout")
		}
	})

	t.Run("read_loop_success_no_cipher", func(t *testing.T) {
		t.Parallel()

		s, c := net.Pipe()

		tcp := &TCP{
			conn:           c,
			logger:         logger,
			BaseConnection: NewBaseConnection("TCP"),
			framer: mockFramer{
				readFunc: func(r io.Reader) ([]byte, error) {
					b := make([]byte, 4)
					_, err := r.Read(b)
					return b, err
				},
			},
			msgChan:    make(chan Message, 10),
			errChan:    make(chan error, 10),
			closedChan: make(chan struct{}),
		}

		go tcp.readLoop()
		defer tcp.Close()

		_, err := s.Write([]byte("test"))
		require.NoError(t, err)

		select {
		case msg := <-tcp.Messages():
			assert.Equal(t, Message("test"), msg)
		case <-time.After(time.Second):
			t.Fatal("timeout")
		}
	})

	t.Run("read_loop_success_with_cipher", func(t *testing.T) {
		t.Parallel()

		s, c := net.Pipe()
		tcp := &TCP{
			conn:           c,
			logger:         logger,
			BaseConnection: NewBaseConnection("TCP"),
			framer: mockFramer{
				readFunc: func(r io.Reader) ([]byte, error) {
					b := make([]byte, 4)
					_, err := r.Read(b)
					return b, err
				},
			},
			msgChan:    make(chan Message, 10),
			errChan:    make(chan error, 10),
			closedChan: make(chan struct{}),
		}

		cipher := mockCipher{
			decFunc: func(data []byte) ([]byte, error) {
				return []byte("decrypted"), nil
			},
		}
		tcp.SetCipher(cipher)

		go tcp.readLoop()
		defer tcp.Close()

		_, err := s.Write([]byte("test"))
		require.NoError(t, err)

		select {
		case msg := <-tcp.Messages():
			assert.Equal(t, Message("decrypted"), msg)
		case <-time.After(time.Second):
			t.Fatal("timeout")
		}
	})

	t.Run("read_loop_ignorable_error", func(t *testing.T) {
		t.Parallel()

		s, c := net.Pipe()

		tcp := &TCP{
			conn:           c,
			logger:         logger,
			BaseConnection: NewBaseConnection("TCP"),
			framer: mockFramer{
				readFunc: func(r io.Reader) ([]byte, error) {
					return nil, io.EOF
				},
			},
			msgChan:    make(chan Message, 10),
			errChan:    make(chan error, 10),
			closedChan: make(chan struct{}),
		}
		go tcp.readLoop()

		defer s.Close()

		select {
		case err := <-tcp.Errors():
			t.Fatalf("unexpected error received: %v", err)
		case <-tcp.Closed():
			// Success! The readLoop returned gracefully without writing to errChan.
		case <-time.After(time.Second):
			t.Fatal("timeout")
		}
	})

	t.Run("read_loop_closed_chan_branch", func(t *testing.T) {
		t.Parallel()

		s, c := net.Pipe()
		tcp := &TCP{
			conn:           c,
			logger:         log.Discard,
			BaseConnection: NewBaseConnection("TCP"),
			framer: mockFramer{
				readFunc: func(r io.Reader) ([]byte, error) {
					b := make([]byte, 10)
					_, err := r.Read(b)
					return b, err
				},
			},
			msgChan:    make(chan Message), // unbuffered so it blocks
			errChan:    make(chan error, 10),
			closedChan: make(chan struct{}),
		}

		go tcp.readLoop()

		// Write data to satisfy ReadFrame and enter the select block
		_, err := s.Write([]byte("some data!"))
		require.NoError(t, err)

		// Wait a brief moment to ensure readLoop is blocked on select
		time.Sleep(50 * time.Millisecond)

		// Send to closedChan to force select exit
		tcp.closedChan <- struct{}{}

		// Verify that closedChan is closed via readLoop's defer
		select {
		case _, ok := <-tcp.closedChan:
			assert.False(t, ok, "closedChan should be closed")
		case <-time.After(time.Second):
			t.Fatal("timeout waiting for readLoop exit")
		}
	})
}

func TestTCP_SetCipher(t *testing.T) {
	t.Parallel()

	tcp := &TCP{logger: log.Discard}
	cipher := mockCipher{}
	ok := tcp.SetCipher(cipher)
	assert.True(t, ok)
	assert.NotNil(t, tcp.cipher)
}

func TestTCP_Send_Deadline(t *testing.T) {
	t.Parallel()

	l, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)

	defer l.Close()

	go func() {
		conn, _ := l.Accept()
		if conn != nil {
			conn.Close()
		}
	}()

	clientConn, err := net.Dial("tcp", l.Addr().String())
	require.NoError(t, err)

	tcp := &TCP{
		conn:           clientConn,
		logger:         log.Discard,
		BaseConnection: NewBaseConnection("TCP"),
		framer:         mockFramer{},
	}
	defer tcp.Close()

	t.Run("immediate_context_cancel", func(t *testing.T) {
		t.Parallel()
		ctx, cancel := context.WithCancel(t.Context())
		cancel() // Cancel before calling Send

		assert.ErrorIs(t, tcp.Send(ctx, []byte("some data")), context.Canceled)
	})

	t.Run("no_context_deadline_branch", func(t *testing.T) {
		t.Parallel()
		assert.NotPanics(t, func() {
			_ = tcp.Send(t.Context(), []byte("data"))
		})
	})

	t.Run("context_with_deadline_success", func(t *testing.T) {
		t.Parallel()

		ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
		defer cancel()

		server, client := net.Pipe()
		defer server.Close()
		defer client.Close()

		tcp := &TCP{
			conn:           client,
			logger:         log.Discard,
			BaseConnection: NewBaseConnection("TCP"),
			framer:         mockFramer{},
		}

		err := tcp.Send(ctx, []byte("data"))
		assert.NoError(t, err)
	})

	t.Run("cipher_encryption_success", func(t *testing.T) {
		t.Parallel()

		server, client := net.Pipe()
		defer server.Close()
		defer client.Close()

		tcp := &TCP{
			conn:           client,
			logger:         log.Discard,
			BaseConnection: NewBaseConnection("TCP"),
			framer:         mockFramer{},
		}

		cipher := mockCipher{
			encFunc: func(data []byte) ([]byte, error) {
				return []byte("encrypted"), nil
			},
		}
		tcp.SetCipher(cipher)

		err := tcp.Send(t.Context(), []byte("plain"))
		assert.NoError(t, err)
	})
}

func TestTCP_Send_Errors(t *testing.T) {
	t.Parallel()

	t.Run("encrypt_failure", func(t *testing.T) {
		t.Parallel()

		tcp := &TCP{
			logger:         log.Discard,
			BaseConnection: NewBaseConnection("TCP"),
			framer:         mockFramer{},
		}
		badCipher := mockCipher{
			encFunc: func(data []byte) ([]byte, error) {
				return nil, errors.New("encrypt fail")
			},
		}
		tcp.SetCipher(badCipher)

		err := tcp.Send(t.Context(), []byte("data"))
		assert.ErrorContains(t, err, "encrypt fail")
	})

	t.Run("deadline_setting_failure", func(t *testing.T) {
		t.Parallel()

		tcp := &TCP{
			conn:           deadlineFailingConn{},
			logger:         log.Discard,
			BaseConnection: NewBaseConnection("TCP"),
			framer:         mockFramer{},
		}

		err := tcp.Send(t.Context(), []byte("data"))
		assert.ErrorContains(t, err, "deadline failed")
	})

	t.Run("framer_write_failure", func(t *testing.T) {
		t.Parallel()

		tcp := &TCP{
			conn:           mockNetConn{},
			logger:         log.Discard,
			BaseConnection: NewBaseConnection("TCP"),
			framer: mockFramer{
				writeFunc: func(w io.Writer, data []byte) error {
					return errors.New("write frame fail")
				},
			},
		}

		err := tcp.Send(t.Context(), []byte("data"))
		assert.ErrorContains(t, err, "write frame fail")
	})
}

func TestIsIgnorableError(t *testing.T) {
	t.Parallel()

	assert.True(t, isIgnorableError(nil))
	assert.True(t, isIgnorableError(io.EOF))
	assert.True(t, isIgnorableError(net.ErrClosed))
	assert.True(t, isIgnorableError(io.ErrClosedPipe))
	assert.True(t, isIgnorableError(errors.New("something: use of closed network connection")))
	assert.True(t, isIgnorableError(errors.New("some closed pipe error")))
	assert.False(t, isIgnorableError(context.DeadlineExceeded))
	assert.False(t, isIgnorableError(errors.New("random error")))
}

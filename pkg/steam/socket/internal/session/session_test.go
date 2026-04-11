// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package session_test

import (
	"context"
	"sync"
	"testing"

	"github.com/lemon4ksan/g-man/pkg/log"
	"github.com/lemon4ksan/g-man/pkg/steam/socket/internal/session"
)

// mockConn имитирует сетевое соединение
type mockConn struct {
	sentData []byte
	closed   bool
	key      []byte
}

func (m *mockConn) Send(ctx context.Context, data []byte) error {
	m.sentData = append([]byte(nil), data...)
	return nil
}

func (m *mockConn) Name() string { return "MOCK" }

func (m *mockConn) Close() error {
	m.closed = true
	return nil
}

func (m *mockConn) ID() int64 { return 1 }

func (m *mockConn) SetEncryptionKey(key []byte) {
	m.key = append([]byte(nil), key...)
}

func TestBase_GettersSetters(t *testing.T) {
	s := session.New(&mockConn{})

	s.SetSteamID(76561197960287930)
	if s.SteamID() != 76561197960287930 {
		t.Errorf("expected SteamID to be set")
	}

	s.SetSessionID(12345)
	if s.SessionID() != 12345 {
		t.Errorf("expected SessionID to be set")
	}

	s.SetAccessToken("access")
	if s.AccessToken() != "access" {
		t.Errorf("expected AccessToken to be set")
	}

	s.SetRefreshToken("refresh")
	if s.RefreshToken() != "refresh" {
		t.Errorf("expected RefreshToken to be set")
	}
}

func TestBase_IsAuthenticated(t *testing.T) {
	tests := []struct {
		name      string
		steamID   uint64
		sessionID int32
		want      bool
	}{
		{"Empty", 0, 0, false},
		{"OnlySession", 0, 123, false},
		{"OnlySteamID", 76561197960287930, 0, false},
		{"BothSet", 76561197960287930, 123, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := session.New(&mockConn{})
			s.SetSteamID(tt.steamID)
			s.SetSessionID(tt.sessionID)
			if got := s.IsAuthenticated(); got != tt.want {
				t.Errorf("IsAuthenticated() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestBase_SendAndClose(t *testing.T) {
	conn := &mockConn{}
	s := session.New(conn)

	payload := []byte("hello")
	if err := s.Send(context.Background(), payload); err != nil {
		t.Fatal(err)
	}

	if string(conn.sentData) != "hello" {
		t.Error("data was not sent to connection")
	}

	if err := s.Close(); err != nil {
		t.Fatal(err)
	}

	if !conn.closed {
		t.Error("connection was not closed")
	}
}

func TestBase_Encryption(t *testing.T) {
	conn := &mockConn{}
	s := session.New(conn)
	key := []byte("secret")

	if !s.SetEncryptionKey(key) {
		t.Error("SetEncryptionKey should return true for mockConn")
	}
	if string(conn.key) != "secret" {
		t.Error("encryption key not passed to connection")
	}
}

func TestLogged_Decorator(t *testing.T) {
	conn := &mockConn{}
	base := session.New(conn)
	logged := session.NewLogged(base, log.Discard)

	logged.SetSteamID(100)
	if logged.SteamID() != 100 {
		t.Error("delegation failed")
	}

	if err := logged.Send(context.Background(), []byte("test")); err != nil {
		t.Fatal(err)
	}

	if string(conn.sentData) != "test" {
		t.Error("Send delegation failed")
	}

	if !logged.SetEncryptionKey([]byte("key")) {
		t.Error("SetEncryptionKey delegation failed")
	}

	_ = logged.Close()
	if !conn.closed {
		t.Error("Close delegation failed")
	}
}

func TestBase_Concurrency(t *testing.T) {
	s := session.New(&mockConn{})
	wg := sync.WaitGroup{}

	const iterations = 1000

	wg.Add(2)
	go func() {
		defer wg.Done()
		for i := 0; i < iterations; i++ {
			s.SetSteamID(uint64(i))
			s.SetAccessToken("token")
		}
	}()

	go func() {
		defer wg.Done()
		for i := 0; i < iterations; i++ {
			_ = s.SteamID()
			_ = s.AccessToken()
			_ = s.IsAuthenticated()
		}
	}()

	wg.Wait()
}

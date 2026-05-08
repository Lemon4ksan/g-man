// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package session_test

import (
	"sync"
	"testing"

	"github.com/lemon4ksan/g-man/pkg/steam/socket/session"
)

func TestBase_GettersSetters(t *testing.T) {
	s := &session.Session{}

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
			s := &session.Session{}
			s.SetSteamID(tt.steamID)
			s.SetSessionID(tt.sessionID)

			if got := s.IsAuthenticated(); got != tt.want {
				t.Errorf("IsAuthenticated() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestBase_Concurrency(t *testing.T) {
	s := &session.Session{}
	wg := sync.WaitGroup{}

	const iterations = 1000

	wg.Add(2)

	go func() {
		defer wg.Done()

		for i := range iterations {
			s.SetSteamID(uint64(i))
			s.SetAccessToken("token")
		}
	}()

	go func() {
		defer wg.Done()

		for range iterations {
			_ = s.SteamID()
			_ = s.AccessToken()
			_ = s.IsAuthenticated()
		}
	}()

	wg.Wait()
}

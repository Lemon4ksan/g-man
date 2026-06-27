// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package session_test

import (
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/lemon4ksan/g-man/pkg/steam/socket/session"
)

func TestBase_GettersSetters(t *testing.T) {
	t.Parallel()

	t.Run("uninitialized_session", func(t *testing.T) {
		t.Parallel()

		s := &session.Session{}

		// Verify default zero values of uninitialized types (including atomic.Value nil-assert fallback)
		assert.Equal(t, uint64(0), s.SteamID())
		assert.Equal(t, int32(0), s.SessionID())
		assert.Empty(t, s.AccessToken())
		assert.Empty(t, s.RefreshToken())
	})

	t.Run("setters_and_getters", func(t *testing.T) {
		t.Parallel()

		s := &session.Session{}

		s.SetSteamID(76561197960287930)
		assert.Equal(t, uint64(76561197960287930), s.SteamID())

		s.SetSessionID(12345)
		assert.Equal(t, int32(12345), s.SessionID())

		s.SetAccessToken("access")
		assert.Equal(t, "access", s.AccessToken())

		s.SetRefreshToken("refresh")
		assert.Equal(t, "refresh", s.RefreshToken())
	})
}

func TestBase_IsAuthenticated(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		steamID   uint64
		sessionID int32
		want      bool
	}{
		{"empty", 0, 0, false},
		{"only_session", 0, 123, false},
		{"only_steam_id", 76561197960287930, 0, false},
		{"both_set", 76561197960287930, 123, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			s := &session.Session{}
			s.SetSteamID(tt.steamID)
			s.SetSessionID(tt.sessionID)

			assert.Equal(t, tt.want, s.IsAuthenticated())
		})
	}
}

func TestBase_Concurrency(t *testing.T) {
	t.Parallel()

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

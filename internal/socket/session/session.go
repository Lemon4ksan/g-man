// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package session maintains thread-safe atomic identity and credential state for socket sessions.
package session

import (
	"sync/atomic"
)

// Session tracks SteamID, SessionID, and OAuth tokens using atomic primitives.
//
// Thread Safety:
//   - Fully safe for concurrent read and write access across all methods.
type Session struct {
	steamID      atomic.Uint64
	sessionID    atomic.Int32
	refreshToken atomic.Value
	accessToken  atomic.Value
}

// SteamID returns the 64-bit Steam ID assigned to the session.
func (s *Session) SteamID() uint64 {
	return s.steamID.Load()
}

// SessionID returns the 32-bit session ID assigned by the Connection Manager.
func (s *Session) SessionID() int32 {
	return s.sessionID.Load()
}

// RefreshToken returns the current OAuth2 refresh token string.
func (s *Session) RefreshToken() string {
	val, _ := s.refreshToken.Load().(string)
	return val
}

// AccessToken returns the current OAuth2 access token string.
func (s *Session) AccessToken() string {
	val, _ := s.accessToken.Load().(string)
	return val
}

// IsAuthenticated reports whether both a valid SessionID and non-zero SteamID exist.
func (s *Session) IsAuthenticated() bool {
	return s.SessionID() != 0 && s.SteamID() != 0
}

// SetSteamID sets the active 64-bit Steam ID.
func (s *Session) SetSteamID(sid uint64) {
	s.steamID.Store(sid)
}

// SetSessionID sets the Connection Manager session ID.
func (s *Session) SetSessionID(sid int32) {
	s.sessionID.Store(sid)
}

// SetRefreshToken sets the OAuth2 refresh token string.
func (s *Session) SetRefreshToken(token string) {
	s.refreshToken.Store(token)
}

// SetAccessToken sets the OAuth2 access token string.
func (s *Session) SetAccessToken(token string) {
	s.accessToken.Store(token)
}

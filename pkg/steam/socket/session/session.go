// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

/*
Package session provides thread-safe storage for Steam session state.

Unlike connection-oriented designs, this session object is long-lived.
It holds identity data (SteamID, SessionID) and security credentials
(Access/Refresh tokens) that survive transport reconnections.

It uses atomic primitives to ensure high-performance access during
asynchronous packet processing.
*/
package session

import (
	"sync/atomic"
)

// Session is the standard thread-safe implementation of a Steam session.
// It relies on atomic operations to prevent data races during high-throughput
// asynchronous packet handling.
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

// SessionID returns the 32-bit session ID assigned by the CM.
func (s *Session) SessionID() int32 {
	return s.sessionID.Load()
}

// RefreshToken returns the current OAuth2 refresh token.
func (s *Session) RefreshToken() string {
	val, _ := s.refreshToken.Load().(string)
	return val
}

// AccessToken returns the current OAuth2 access token.
func (s *Session) AccessToken() string {
	val, _ := s.accessToken.Load().(string)
	return val
}

// IsAuthenticated returns true if the session has been assigned both
// a SessionID by the CM and a valid SteamID.
func (s *Session) IsAuthenticated() bool {
	// Steam considers a client partially authenticated once it has a SessionID,
	// but fully authenticated only when a valid SteamID is assigned.
	return s.SessionID() != 0 && s.SteamID() != 0
}

// SetSteamID updates the session's Steam ID.
func (s *Session) SetSteamID(sid uint64) {
	s.steamID.Store(sid)
}

// SetSessionID updates the session's ID assigned by the CM.
func (s *Session) SetSessionID(sid int32) {
	s.sessionID.Store(sid)
}

// SetRefreshToken updates the OAuth2 refresh token.
func (s *Session) SetRefreshToken(token string) {
	s.refreshToken.Store(token)
}

// SetAccessToken updates the OAuth2 access token.
func (s *Session) SetAccessToken(token string) {
	s.accessToken.Store(token)
}

// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package session manages the state and lifecycle of a Steam Connection Manager (CM) session.
package session

import (
	"context"
	"sync/atomic"

	"github.com/lemon4ksan/g-man/pkg/steam/socket/internal/network"
)

// Base is the standard thread-safe implementation of a Steam session.
// It relies on atomic operations to prevent data races during high-throughput
// asynchronous packet handling.
type Base struct {
	conn network.Connection

	steamID      atomic.Uint64
	sessionID    atomic.Int32
	refreshToken atomic.Value
	accessToken  atomic.Value
}

// New initializes a new session wrapping the provided connection.
func New(conn network.Connection) *Base {
	return &Base{
		conn: conn,
	}
}

// SteamID returns the 64-bit Steam ID assigned to the session.
func (s *Base) SteamID() uint64 {
	return s.steamID.Load()
}

// SessionID returns the 32-bit session ID assigned by the CM.
func (s *Base) SessionID() int32 {
	return s.sessionID.Load()
}

// RefreshToken returns the current OAuth2 refresh token.
func (s *Base) RefreshToken() string {
	val, _ := s.refreshToken.Load().(string)
	return val
}

// AccessToken returns the current OAuth2 access token.
func (s *Base) AccessToken() string {
	val, _ := s.accessToken.Load().(string)
	return val
}

// IsAuthenticated returns true if the session has been assigned both
// a SessionID by the CM and a valid SteamID.
func (s *Base) IsAuthenticated() bool {
	// Steam considers a client partially authenticated once it has a SessionID,
	// but fully authenticated only when a valid SteamID is assigned.
	return s.SessionID() != 0 && s.SteamID() != 0
}

// SetSteamID updates the session's Steam ID.
func (s *Base) SetSteamID(sid uint64) {
	s.steamID.Store(sid)
}

// SetSessionID updates the session's ID assigned by the CM.
func (s *Base) SetSessionID(sid int32) {
	s.sessionID.Store(sid)
}

// SetRefreshToken updates the OAuth2 refresh token.
func (s *Base) SetRefreshToken(token string) {
	s.refreshToken.Store(token)
}

// SetAccessToken updates the OAuth2 access token.
func (s *Base) SetAccessToken(token string) {
	s.accessToken.Store(token)
}

// Send writes the provided payload to the underlying network transport.
func (s *Base) Send(ctx context.Context, data []byte) error {
	return s.conn.Send(ctx, data)
}

// SetEncryptionKey upgrades the underlying connection to use Steam's symmetric encryption.
func (s *Base) SetEncryptionKey(key []byte) bool {
	if enc, ok := s.conn.(network.Encryptable); ok {
		enc.SetEncryptionKey(key)
		return true
	}

	return false
}

// Close terminates the underlying network connection.
func (s *Base) Close() error {
	return s.conn.Close()
}

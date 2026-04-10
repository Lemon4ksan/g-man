// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package session

import (
	"context"
	"sync/atomic"

	"github.com/lemon4ksan/g-man/pkg/log"
	"github.com/lemon4ksan/g-man/pkg/steam/socket/internal/network"
)

// Reader provides read-only access to the Steam session state.
type Reader interface {
	SteamID() uint64
	SessionID() int32
	AccessToken() string
	RefreshToken() string

	// IsAuthenticated returns true if the session has been assigned both
	// a SessionID by the CM and a valid SteamID.
	IsAuthenticated() bool
}

// Writer provides message transmission capabilities.
type Writer interface {
	// Send writes the provided payload to the underlying network transport.
	Send(ctx context.Context, data []byte) error
}

// Mutator provides write access to modify the session's internal state and lifecycle.
type Mutator interface {
	SetSteamID(uint64)
	SetSessionID(int32)
	SetRefreshToken(string)
	SetAccessToken(string)

	// SetEncryptionKey upgrades the underlying connection to use Steam's
	// symmetric encryption if the underlying connection supports it.
	SetEncryptionKey(key []byte) bool

	// Close terminates the underlying network connection.
	Close() error
}

// Session represents the complete lifecycle and state of a connection
// to a Steam Connection Manager (CM).
type Session interface {
	Reader
	Writer
	Mutator
}

// Ensure BaseSession implements the Session interface at compile time.
var _ Session = (*Base)(nil)

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

func (s *Base) SteamID() uint64 {
	return s.steamID.Load()
}

func (s *Base) SessionID() int32 {
	return s.sessionID.Load()
}

func (s *Base) RefreshToken() string {
	val, _ := s.refreshToken.Load().(string)
	return val
}

func (s *Base) AccessToken() string {
	val, _ := s.accessToken.Load().(string)
	return val
}

func (s *Base) IsAuthenticated() bool {
	// Steam considers a client partially authenticated once it has a SessionID,
	// but fully authenticated only when a valid SteamID is assigned.
	return s.SessionID() != 0 && s.SteamID() != 0
}

func (s *Base) SetSteamID(sid uint64) {
	s.steamID.Store(sid)
}

func (s *Base) SetSessionID(sid int32) {
	s.sessionID.Store(sid)
}

func (s *Base) SetRefreshToken(token string) {
	s.refreshToken.Store(token)
}

func (s *Base) SetAccessToken(token string) {
	s.accessToken.Store(token)
}

func (s *Base) Send(ctx context.Context, data []byte) error {
	return s.conn.Send(ctx, data)
}

func (s *Base) SetEncryptionKey(key []byte) bool {
	if enc, ok := s.conn.(network.Encryptable); ok {
		enc.SetEncryptionKey(key)
		return true
	}
	return false
}

func (s *Base) Close() error {
	return s.conn.Close()
}

// Logged is a Decorator that wraps a Session to provide automatic
// logging of network and lifecycle events, without modifying the core logic.
type Logged struct {
	Session
	logger log.Logger
}

// NewLogged wraps an existing session with logging capabilities.
func NewLogged(s Session, l log.Logger) *Logged {
	return &Logged{
		Session: s,
		logger:  l,
	}
}

// Send intercepts the Send call to add debug logging.
func (l *Logged) Send(ctx context.Context, data []byte) error {
	l.logger.Debug("Writing to socket",
		log.Int("size_bytes", len(data)),
		log.Uint64("steam_id", l.SteamID()),
	)

	err := l.Session.Send(ctx, data)
	if err != nil {
		l.logger.Error("Failed to write to socket",
			log.Err(err),
			log.Int("size_bytes", len(data)),
		)
	}
	return err
}

// SetEncryptionKey intercepts the encryption setup to log the event.
func (l *Logged) SetEncryptionKey(key []byte) bool {
	l.logger.Debug("Applying channel encryption key", log.Int("key_len", len(key)))
	if l.Session.SetEncryptionKey(key) {
		l.logger.Info("Channel encryption established successfully")
		return true
	}
	l.logger.Warn("Channel encryption skipped: connection not encryptable")
	return false
}

// Close intercepts the Close call to add debug logging.
func (l *Logged) Close() error {
	l.logger.Debug("Closing session connection",
		log.Uint64("steam_id", l.SteamID()),
		log.Int32("session_id", l.SessionID()),
	)
	return l.Session.Close()
}

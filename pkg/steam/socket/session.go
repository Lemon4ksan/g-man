// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package socket

import (
	"context"
	"sync/atomic"

	"github.com/lemon4ksan/g-man/pkg/log"
	"github.com/lemon4ksan/g-man/pkg/steam/network"
)

// SessionReader provides read-only access to the Steam session state.
type SessionReader interface {
	SteamID() uint64
	SessionID() int32
	AccessToken() string

	// IsAuthenticated returns true if the session has been assigned both
	// a SessionID by the CM and a valid SteamID.
	IsAuthenticated() bool
}

// SessionWriter provides message transmission capabilities.
type SessionWriter interface {
	// Send writes the provided payload to the underlying network transport.
	Send(ctx context.Context, data []byte) error
}

// SessionMutator provides write access to modify the session's internal state and lifecycle.
type SessionMutator interface {
	SetSteamID(uint64)
	SetSessionID(int32)
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
	SessionReader
	SessionWriter
	SessionMutator
}

// Ensure BaseSession implements the Session interface at compile time.
var _ Session = (*BaseSession)(nil)

// BaseSession is the standard thread-safe implementation of a Steam session.
// It relies on atomic operations to prevent data races during high-throughput
// asynchronous packet handling.
type BaseSession struct {
	conn network.Connection

	steamID     atomic.Uint64
	sessionID   atomic.Int32
	accessToken atomic.Pointer[string]
}

// NewBaseSession initializes a new session wrapping the provided connection.
func NewBaseSession(conn network.Connection) *BaseSession {
	return &BaseSession{
		conn: conn,
	}
	// Note: No need to pre-allocate an empty string for accessToken.
	// The AccessToken() getter handles nil pointers safely.
}

func (s *BaseSession) SteamID() uint64 {
	return s.steamID.Load()
}

func (s *BaseSession) SessionID() int32 {
	return s.sessionID.Load()
}

func (s *BaseSession) AccessToken() string {
	ptr := s.accessToken.Load()
	if ptr == nil {
		return ""
	}
	return *ptr
}

func (s *BaseSession) IsAuthenticated() bool {
	// Steam considers a client partially authenticated once it has a SessionID,
	// but fully authenticated only when a valid SteamID is assigned.
	return s.SessionID() != 0 && s.SteamID() != 0
}

func (s *BaseSession) SetSteamID(sid uint64) {
	s.steamID.Store(sid)
}

func (s *BaseSession) SetSessionID(sid int32) {
	s.sessionID.Store(sid)
}

func (s *BaseSession) SetAccessToken(token string) {
	s.accessToken.Store(&token)
}

func (s *BaseSession) Send(ctx context.Context, data []byte) error {
	return s.conn.Send(ctx, data)
}

func (s *BaseSession) SetEncryptionKey(key []byte) bool {
	if enc, ok := s.conn.(network.Encryptable); ok {
		enc.SetEncryptionKey(key)
		return true
	}
	return false
}

func (s *BaseSession) Close() error {
	return s.conn.Close()
}

// LoggedSession is a Decorator that wraps a Session to provide automatic
// logging of network and lifecycle events, without modifying the core logic.
type LoggedSession struct {
	Session // Embedded interface automatically forwards all unimplemented methods
	logger  log.Logger
}

// NewLoggedSession wraps an existing session with logging capabilities.
func NewLoggedSession(s Session, l log.Logger) *LoggedSession {
	return &LoggedSession{
		Session: s,
		logger:  l,
	}
}

// Send intercepts the Send call to add debug logging.
func (l *LoggedSession) Send(ctx context.Context, data []byte) error {
	if l.logger.IsDebugEnabled() {
		l.logger.Debug("Writing to socket",
			log.Int("size_bytes", len(data)),
			log.Uint64("steam_id", l.Session.SteamID()),
		)
	}

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
func (l *LoggedSession) SetEncryptionKey(key []byte) bool {
	l.logger.Debug("Applying channel encryption key", log.Int("key_len", len(key)))
	if l.Session.SetEncryptionKey(key) {
		l.logger.Info("Channel encryption established successfully")
		return true
	}
	l.logger.Warn("Channel encryption skipped: connection not encryptable")
	return false
}

// Close intercepts the Close call to add debug logging.
func (l *LoggedSession) Close() error {
	l.logger.Debug("Closing session connection",
		log.Uint64("steam_id", l.Session.SteamID()),
		log.Int32("session_id", l.Session.SessionID()),
	)
	return l.Session.Close()
}

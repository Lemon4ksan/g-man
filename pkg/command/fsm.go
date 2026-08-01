// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package command

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"
)

var (
	// ErrStateNotFound is returned when a session state has no registered handler.
	ErrStateNotFound = errors.New("fsm: state handler not found")
	// ErrSessionExpired is returned when a session has exceeded its TTL.
	ErrSessionExpired = errors.New("fsm: conversation session expired")
)

// Session represents an active user conversation state and payload.
type Session struct {
	UserID    string
	State     string
	Data      map[string]any
	UpdatedAt time.Time
}

// Get retrieves a key from session data.
func (s *Session) Get(key string) (any, bool) {
	if s.Data == nil {
		return nil, false
	}

	val, ok := s.Data[key]

	return val, ok
}

// Set sets a key in session data.
func (s *Session) Set(key string, val any) {
	if s.Data == nil {
		s.Data = make(map[string]any)
	}

	s.Data[key] = val
}

// StateHandler processes input for a specific conversation state.
// Returning nextState = "" completes/clears the conversation.
type StateHandler func(ctx context.Context, input string, session *Session) (nextState, response string, err error)

// ConversationManager manages multi-step user states (FSM / Dialogs).
type ConversationManager struct {
	mu          sync.RWMutex
	sessions    map[string]*Session
	handlers    map[string]StateHandler
	ttl         time.Duration
	cancelToken string
}

// NewConversationManager constructs a ConversationManager with optional TTL for inactivity.
func NewConversationManager(ttl time.Duration) *ConversationManager {
	if ttl <= 0 {
		ttl = 15 * time.Minute
	}

	return &ConversationManager{
		sessions:    make(map[string]*Session),
		handlers:    make(map[string]StateHandler),
		ttl:         ttl,
		cancelToken: "!cancel",
	}
}

// SetCancelToken configures a custom cancellation command keyword (default "!cancel").
func (cm *ConversationManager) SetCancelToken(token string) {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	cm.cancelToken = token
}

// RegisterState registers a handler for a specific conversation state.
func (cm *ConversationManager) RegisterState(state string, handler StateHandler) {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	cm.handlers[state] = handler
}

// SetState explicitly sets or updates a user's active state.
func (cm *ConversationManager) SetState(userID, state string, initialData map[string]any) {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	if initialData == nil {
		initialData = make(map[string]any)
	}

	cm.sessions[userID] = &Session{
		UserID:    userID,
		State:     state,
		Data:      initialData,
		UpdatedAt: time.Now(),
	}
}

// GetState retrieves a user's current active session if valid and not expired.
func (cm *ConversationManager) GetState(userID string) (*Session, bool) {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	sess, exists := cm.sessions[userID]
	if !exists {
		return nil, false
	}

	if time.Since(sess.UpdatedAt) > cm.ttl {
		delete(cm.sessions, userID)
		return nil, false
	}

	return sess, true
}

// ClearState terminates an active user session.
func (cm *ConversationManager) ClearState(userID string) {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	delete(cm.sessions, userID)
}

// HandleInput attempts to process incoming user text through an active FSM session.
// Returns handled = true if the input was handled by the FSM (or cancelled).
func (cm *ConversationManager) HandleInput(
	ctx context.Context,
	userID, input string,
) (handled bool, response string, err error) {
	cm.mu.Lock()

	sess, exists := cm.sessions[userID]
	if !exists {
		cm.mu.Unlock()
		return false, "", nil
	}

	if time.Since(sess.UpdatedAt) > cm.ttl {
		delete(cm.sessions, userID)
		cm.mu.Unlock()
		return false, "", nil
	}

	cancelTok := cm.cancelToken
	handler, hasHandler := cm.handlers[sess.State]
	cm.mu.Unlock()

	trimmed := strings.TrimSpace(input)
	if trimmed == cancelTok || trimmed == "/cancel" {
		cm.ClearState(userID)
		return true, "Conversation cancelled.", nil
	}

	if !hasHandler {
		cm.ClearState(userID)
		return true, "", ErrStateNotFound
	}

	sess.UpdatedAt = time.Now()

	nextState, resp, err := handler(ctx, input, sess)
	if err != nil {
		return true, resp, err
	}

	if nextState == "" {
		cm.ClearState(userID)
	} else {
		cm.mu.Lock()
		sess.State = nextState
		sess.UpdatedAt = time.Now()
		cm.mu.Unlock()
	}

	return true, resp, nil
}

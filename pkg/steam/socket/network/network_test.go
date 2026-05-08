// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package network

import "sync"

type MockHandler struct {
	mu           sync.Mutex
	messages     [][]byte
	errors       []error
	closedCalled bool
	msgChan      chan []byte
	errChan      chan error
}

func NewMockHandler() *MockHandler {
	return &MockHandler{
		msgChan: make(chan []byte, 10),
		errChan: make(chan error, 10),
	}
}

func (m *MockHandler) OnNetMessage(msg NetMessage) {
	m.mu.Lock()
	m.messages = append(m.messages, msg)
	m.mu.Unlock()

	m.msgChan <- msg
}

func (m *MockHandler) OnNetError(err error) {
	m.mu.Lock()
	m.errors = append(m.errors, err)
	m.mu.Unlock()

	m.errChan <- err
}

func (m *MockHandler) OnNetClose() {
	m.mu.Lock()
	m.closedCalled = true
	m.mu.Unlock()
}

func (m *MockHandler) ErrChan() <-chan error { return m.errChan }

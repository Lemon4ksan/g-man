package network

import "sync"

type MockHandler struct {
	mu           sync.Mutex
	messages     [][]byte
	errors       []error
	closedCalled bool
	msgChan      chan []byte
}

func NewMockHandler() *MockHandler {
	return &MockHandler{
		msgChan: make(chan []byte, 10),
	}
}

func (m *MockHandler) OnNetMessage(msg NetMessage) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.messages = append(m.messages, msg)
	m.msgChan <- msg
}

func (m *MockHandler) OnNetError(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.errors = append(m.errors, err)
}

func (m *MockHandler) OnNetClose() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.closedCalled = true
}

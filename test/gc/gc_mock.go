// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package gc

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/lemon4ksan/g-man/pkg/jobs"
	"github.com/lemon4ksan/g-man/pkg/steam/module"
	"github.com/lemon4ksan/g-man/pkg/steam/socket/protocol"
	"google.golang.org/protobuf/proto"
)

type Mock struct {
	mu           sync.RWMutex
	sendCalls    map[uint32]proto.Message
	sendRawCalls map[uint32][]byte

	pendingCalls map[uint32]jobs.Callback[*protocol.GCPacket]
	autoReplies  map[uint32]func(payload []byte) ([]byte, error)
}

func New() *Mock {
	return &Mock{
		sendCalls:    make(map[uint32]proto.Message),
		sendRawCalls: make(map[uint32][]byte),
		pendingCalls: make(map[uint32]jobs.Callback[*protocol.GCPacket]),
		autoReplies:  make(map[uint32]func(payload []byte) ([]byte, error)),
	}
}

func (m *Mock) Name() string {
	return "gc"
}

func (m *Mock) Init(init module.InitContext) error {
	return nil
}

func (m *Mock) Start(ctx context.Context) error {
	return nil
}

func (m *Mock) Close() error {
	return nil
}

func (m *Mock) Send(ctx context.Context, appID uint32, msgType uint32, msg proto.Message) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sendCalls[msgType] = msg
	return nil
}

func (m *Mock) SendRaw(ctx context.Context, appID uint32, msgType uint32, payload []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sendRawCalls[msgType] = payload
	return nil
}

func (m *Mock) Call(ctx context.Context, appID uint32, msgType uint32, msg proto.Message, cb jobs.Callback[*protocol.GCPacket]) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.sendCalls[msgType] = msg
	m.pendingCalls[msgType] = cb

	if handler, ok := m.autoReplies[msgType]; ok {
		go func() {
			b, _ := proto.Marshal(msg)
			respPayload, err := handler(b)
			cb(&protocol.GCPacket{MsgType: msgType, Payload: respPayload}, err)
		}()
	}

	return nil
}

func (m *Mock) CallRaw(ctx context.Context, appID uint32, msgType uint32, payload []byte, cb jobs.Callback[*protocol.GCPacket]) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.sendRawCalls[msgType] = payload
	m.pendingCalls[msgType] = cb

	if handler, ok := m.autoReplies[msgType]; ok {
		go func() {
			respPayload, err := handler(payload)
			cb(&protocol.GCPacket{MsgType: msgType, Payload: respPayload}, err)
		}()
	}

	return nil
}

func (m *Mock) OnCallRaw(msgType uint32, handler func(payload []byte) ([]byte, error)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.autoReplies[msgType] = handler
}

func (m *Mock) AssertSent(t *testing.T, msgType uint32) {
	t.Helper()
	m.mu.RLock()
	defer m.mu.RUnlock()
	if _, ok := m.sendCalls[msgType]; !ok {
		t.Errorf("expected Protobuf message %d to be sent to GC", msgType)
	}
}

func (m *Mock) AssertSentRaw(t *testing.T, msgType uint32) {
	t.Helper()
	m.mu.RLock()
	defer m.mu.RUnlock()
	if _, ok := m.sendRawCalls[msgType]; !ok {
		t.Errorf("expected raw message %d to be sent to GC", msgType)
	}
}

func (m *Mock) GetLastRawCall(msgType uint32) []byte {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.sendRawCalls[msgType]
}

func (m *Mock) ReplyToLastCall(msgType uint32, payload []byte, err error) error {
	m.mu.Lock()
	cb, ok := m.pendingCalls[msgType]
	m.mu.Unlock()

	if !ok || cb == nil {
		return errors.New("no pending call for this msgType")
	}

	go cb(&protocol.GCPacket{
		MsgType: msgType,
		Payload: payload,
	}, err)

	return nil
}

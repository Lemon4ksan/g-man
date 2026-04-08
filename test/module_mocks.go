// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package test

import (
	"encoding/json"
	"sync"
	"testing"

	"github.com/lemon4ksan/g-man/pkg/log"
	"github.com/lemon4ksan/g-man/pkg/modules"
	"github.com/lemon4ksan/g-man/pkg/rest"
	"github.com/lemon4ksan/g-man/pkg/steam/bus"
	"github.com/lemon4ksan/g-man/pkg/steam/community"
	"github.com/lemon4ksan/g-man/pkg/steam/protocol"
	"github.com/lemon4ksan/g-man/pkg/steam/service"
	"github.com/lemon4ksan/g-man/pkg/steam/socket"
	"github.com/lemon4ksan/g-man/pkg/steam/steamid"
	tr "github.com/lemon4ksan/g-man/pkg/steam/transport"
	"github.com/lemon4ksan/g-man/pkg/storage"
	"google.golang.org/protobuf/proto"
)

type MockInitContext struct {
	mu              sync.RWMutex
	eventBus        *bus.Bus
	logger          log.Logger
	mockService     *MockRequester
	packetHandlers  map[protocol.EMsg]socket.Handler
	serviceHandlers map[string]socket.Handler
	modules         map[string]modules.Module
	storage         storage.Provider
}

func NewMockInitContext() *MockInitContext {
	return &MockInitContext{
		eventBus:        bus.NewBus(),
		logger:          log.Discard,
		mockService:     NewMockRequester(),
		packetHandlers:  make(map[protocol.EMsg]socket.Handler),
		serviceHandlers: make(map[string]socket.Handler),
		modules:         make(map[string]modules.Module),
	}
}

func (m *MockInitContext) Bus() *bus.Bus               { return m.eventBus }
func (m *MockInitContext) Logger() log.Logger          { return m.logger }
func (m *MockInitContext) Service() service.Doer       { return m.mockService }
func (m *MockInitContext) Rest() rest.Requester        { return m.mockService }
func (m *MockInitContext) Storage() storage.Provider   { return m.storage }
func (m *MockInitContext) MockService() *MockRequester { return m.mockService }

func (m *MockInitContext) SetService(s *MockRequester) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.mockService = s
}

func (m *MockInitContext) SetStorage(s storage.Provider) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.storage = s
}

func (m *MockInitContext) RegisterPacketHandler(e protocol.EMsg, h socket.Handler) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.packetHandlers[e] = h
}

func (m *MockInitContext) UnregisterPacketHandler(e protocol.EMsg) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.packetHandlers, e)
}

func (m *MockInitContext) RegisterServiceHandler(method string, h socket.Handler) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.serviceHandlers[method] = h
}

func (m *MockInitContext) UnregisterServiceHandler(method string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.serviceHandlers, method)
}

func (m *MockInitContext) Module(name string) modules.Module {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.modules[name]
}

func (m *MockInitContext) GetPacketHandler(method protocol.EMsg) (socket.Handler, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	h, ok := m.packetHandlers[method]
	return h, ok
}

func (m *MockInitContext) GetServiceHandler(method string) (socket.Handler, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	h, ok := m.serviceHandlers[method]
	return h, ok
}

func (m *MockInitContext) AssertPacketHandlerRegistered(t *testing.T, e protocol.EMsg) {
	t.Helper()
	m.mu.RLock()
	defer m.mu.RUnlock()
	if _, ok := m.packetHandlers[e]; !ok {
		t.Errorf("expected packet handler for %v to be registered", e)
	}
}

func (m *MockInitContext) AssertPacketHandlerUnregistered(t *testing.T, e protocol.EMsg) {
	t.Helper()
	m.mu.RLock()
	defer m.mu.RUnlock()
	if _, ok := m.packetHandlers[e]; ok {
		t.Errorf("expected packet handler for %v to be unregistered", e)
	}
}

func (m *MockInitContext) EmitPacket(t *testing.T, e protocol.EMsg, msg proto.Message) {
	t.Helper()
	m.mu.RLock()
	handler, ok := m.packetHandlers[e]
	m.mu.RUnlock()

	if !ok {
		t.Fatalf("no handler registered for packet %v", e)
	}

	payload, err := proto.Marshal(msg)
	if err != nil {
		t.Fatalf("failed to marshal packet %v: %v", e, err)
	}

	handler(&protocol.Packet{
		EMsg:    e,
		Payload: payload,
	})
}

func (m *MockInitContext) SetModule(name string, mod modules.Module) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.modules[name] = mod
}

func (m *MockInitContext) MockServiceAccessor() *MockRequester {
	return m.mockService
}

type MockAuthContext struct {
	mockCommunity *MockCommunityRequester
	steamID       steamid.ID
}

func NewMockAuthContext(steamID steamid.ID) *MockAuthContext {
	return &MockAuthContext{
		mockCommunity: NewMockCommunityRequester(),
		steamID:       steamID,
	}
}

func (m *MockAuthContext) Community() community.Requester         { return m.mockCommunity }
func (m *MockAuthContext) MockCommunity() *MockCommunityRequester { return m.mockCommunity }
func (m *MockAuthContext) SteamID() steamid.ID                    { return m.steamID }

func ProtoResponse(msg proto.Message) (*tr.Response, error) {
	b, err := proto.Marshal(msg)
	if err != nil {
		return nil, err
	}
	return tr.NewResponse(b, tr.SocketMetadata{Result: protocol.EResult_OK}), nil
}

func JSONResponse(msg any) (*tr.Response, error) {
	b, err := json.Marshal(msg)
	if err != nil {
		return nil, err
	}
	return tr.NewResponse(b, tr.HTTPMetadata{StatusCode: 200}), nil
}

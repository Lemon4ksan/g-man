// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package module

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"google.golang.org/protobuf/proto"

	"github.com/lemon4ksan/aoni"
	"github.com/lemon4ksan/g-man/pkg/log"
	"github.com/lemon4ksan/g-man/pkg/steam/community"
	"github.com/lemon4ksan/g-man/pkg/steam/id"
	"github.com/lemon4ksan/g-man/pkg/steam/module"
	"github.com/lemon4ksan/g-man/pkg/steam/protocol"
	"github.com/lemon4ksan/g-man/pkg/steam/protocol/enums"
	"github.com/lemon4ksan/g-man/pkg/steam/service"
	"github.com/lemon4ksan/g-man/pkg/steam/socket"
	tr "github.com/lemon4ksan/g-man/pkg/steam/transport"
	"github.com/lemon4ksan/g-man/pkg/storage"
	cm "github.com/lemon4ksan/g-man/test/community"
	"github.com/lemon4ksan/g-man/test/requester"
	"github.com/lemon4ksan/miyako/bus"
)

type requesterDoer struct {
	r aoni.Requester
}

func (d *requesterDoer) Do(req *http.Request) (*http.Response, error) {
	return d.r.Request(req.Context(), req.Method, req.URL.String(), func(r *http.Request) {
		r.Header = req.Header
		r.Body = req.Body
	})
}

type InitContext struct {
	mu              sync.RWMutex
	eventBus        *bus.Bus
	logger          log.Logger
	mockService     *requester.Mock
	packetHandlers  map[enums.EMsg]socket.Handler
	serviceHandlers map[string]socket.Handler
	modules         map[string]module.Module
	storage         storage.Provider
}

func NewInitContext() *InitContext {
	return &InitContext{
		eventBus:        bus.New(),
		logger:          log.Discard,
		mockService:     requester.New(),
		packetHandlers:  make(map[enums.EMsg]socket.Handler),
		serviceHandlers: make(map[string]socket.Handler),
		modules:         make(map[string]module.Module),
	}
}

func (m *InitContext) Bus() *bus.Bus         { return m.eventBus }
func (m *InitContext) Logger() log.Logger    { return m.logger }
func (m *InitContext) Service() service.Doer { return m.mockService }

func (m *InitContext) Rest() aoni.Requester {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if m.mockService == nil {
		return nil
	}

	return aoni.NewClient(&requesterDoer{r: m.mockService})
}

func (m *InitContext) Storage() storage.Provider    { return m.storage }
func (m *InitContext) MockService() *requester.Mock { return m.mockService }

func (m *InitContext) SetService(s *requester.Mock) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.mockService = s
}

func (m *InitContext) SetStorage(s storage.Provider) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.storage = s
}

func (m *InitContext) RegisterPacketHandler(e enums.EMsg, h socket.Handler) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.packetHandlers[e] = h
}

func (m *InitContext) UnregisterPacketHandler(e enums.EMsg) {
	m.mu.Lock()
	defer m.mu.Unlock()

	delete(m.packetHandlers, e)
}

func (m *InitContext) RegisterServiceHandler(method string, h socket.Handler) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.serviceHandlers[method] = h
}

func (m *InitContext) UnregisterServiceHandler(method string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	delete(m.serviceHandlers, method)
}

func (m *InitContext) Module(name string) module.Module {
	m.mu.RLock()
	defer m.mu.RUnlock()

	return m.modules[name]
}

func (m *InitContext) GetPacketHandler(method enums.EMsg) (socket.Handler, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	h, ok := m.packetHandlers[method]

	return h, ok
}

func (m *InitContext) GetServiceHandler(method string) (socket.Handler, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	h, ok := m.serviceHandlers[method]

	return h, ok
}

func (m *InitContext) AssertPacketHandlerRegistered(t *testing.T, e enums.EMsg) {
	t.Helper()

	m.mu.RLock()
	defer m.mu.RUnlock()

	if _, ok := m.packetHandlers[e]; !ok {
		t.Errorf("expected packet handler for %v to be registered", e)
	}
}

func (m *InitContext) AssertPacketHandlerUnregistered(t *testing.T, e enums.EMsg) {
	t.Helper()

	m.mu.RLock()
	defer m.mu.RUnlock()

	if _, ok := m.packetHandlers[e]; ok {
		t.Errorf("expected packet handler for %v to be unregistered", e)
	}
}

// AssertServiceHandlerRegistered verifies that a handler for the specific method exists.
func (m *InitContext) AssertServiceHandlerRegistered(t *testing.T, serviceMethod string) {
	t.Helper()
	m.mu.RLock()
	defer m.mu.RUnlock()

	_, ok := m.serviceHandlers[serviceMethod]
	assert.True(t, ok, "Expected service handler %q to be registered, but it was not", serviceMethod)
}

// AssertServiceHandlerUnregistered verifies that a handler for the specific method does NOT exist.
func (m *InitContext) AssertServiceHandlerUnregistered(t *testing.T, serviceMethod string) {
	t.Helper()
	m.mu.RLock()
	defer m.mu.RUnlock()

	_, ok := m.serviceHandlers[serviceMethod]
	assert.False(t, ok, "Expected service handler %q to be unregistered, but it is still present", serviceMethod)
}

func (m *InitContext) EmitPacket(t *testing.T, e enums.EMsg, msg proto.Message) {
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

func (m *InitContext) SetModule(name string, mod module.Module) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.modules[name] = mod
}

func (m *InitContext) MockServiceAccessor() *requester.Mock {
	return m.mockService
}

type AuthContext struct {
	mockCommunity *cm.Mock
	steamID       id.ID
}

func NewAuthContext(steamID id.ID) *AuthContext {
	return &AuthContext{
		mockCommunity: cm.New(),
		steamID:       steamID,
	}
}

func (m *AuthContext) Community() community.Requester { return m.mockCommunity }
func (m *AuthContext) MockCommunity() *cm.Mock        { return m.mockCommunity }
func (m *AuthContext) SteamID() id.ID                 { return m.steamID }

func ProtoResponse(msg proto.Message) (*tr.Response, error) {
	b, err := proto.Marshal(msg)
	if err != nil {
		return nil, err
	}

	return tr.NewResponse(io.NopCloser(bytes.NewReader(b)), tr.SocketMetadata{Result: enums.EResult_OK}), nil
}

func JSONResponse(msg any) (*tr.Response, error) {
	b, err := json.Marshal(msg)
	if err != nil {
		return nil, err
	}

	return tr.NewResponse(io.NopCloser(bytes.NewReader(b)), tr.HTTPMetadata{StatusCode: 200}), nil
}

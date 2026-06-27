// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// test/mock/module.go
package mock

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"sync"
	"testing"

	"github.com/lemon4ksan/aoni"
	"github.com/lemon4ksan/miyako/bus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"google.golang.org/protobuf/proto"

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
	"github.com/lemon4ksan/g-man/pkg/storage/memory"
)

// Module is a Testify mock for module.Module.
type Module struct {
	mock.Mock
}

func (m *Module) Name() string {
	args := m.Called()
	return args.String(0)
}

func (m *Module) Init(ctx module.InitContext) error {
	args := m.Called(ctx)
	return args.Error(0)
}

func (m *Module) Start(ctx context.Context) error {
	args := m.Called(ctx)
	return args.Error(0)
}

// AuthModule is a Testify mock for module.Auth.
type AuthModule struct {
	Module
}

func (m *AuthModule) StartAuthed(ctx context.Context, authCtx module.AuthContext) error {
	args := m.Called(ctx, authCtx)
	return args.Error(0)
}

type requesterDoer struct {
	r aoni.Requester
}

func (d *requesterDoer) Do(req *http.Request) (*http.Response, error) {
	return d.r.Request(req.Context(), req.Method, req.URL.String(), func(r *http.Request) {
		r.Header = req.Header
		r.Body = req.Body
	})
}

// InitContext is a lightweight, fully-featured manual mock for module.InitContext.
// It provides convenient helpers for emitting packets and asserting registrations in tests.
type InitContext struct {
	mu              sync.RWMutex
	eventBus        *bus.Bus
	logger          log.Logger
	packetHandlers  map[enums.EMsg]socket.Handler
	serviceHandlers map[string]socket.Handler
	modules         map[string]module.Module
	storage         storage.Provider
	service         *ServiceMock
	rest            aoni.Requester
}

// NewInitContext creates a new InitContext with safe defaults (e.g. memory storage).
func NewInitContext() *InitContext {
	return &InitContext{
		eventBus:        bus.New(),
		logger:          log.Discard,
		packetHandlers:  make(map[enums.EMsg]socket.Handler),
		serviceHandlers: make(map[string]socket.Handler),
		modules:         make(map[string]module.Module),
		service:         NewServiceMock(),
		storage:         memory.New(),
	}
}

func (m *InitContext) MockService() *ServiceMock {
	return m.service
}

func (m *InitContext) Bus() *bus.Bus         { return m.eventBus }
func (m *InitContext) Logger() log.Logger    { return m.logger }
func (m *InitContext) Service() service.Doer { return m.service }

func (m *InitContext) Storage() storage.Provider {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.storage
}

func (m *InitContext) Rest() aoni.Requester {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.rest
}

func (m *InitContext) SetService(s *ServiceMock) {
	m.mu.Lock()
	m.service = s
	m.mu.Unlock()
}

func (m *InitContext) SetRest(r aoni.Requester) {
	m.mu.Lock()
	m.rest = r
	m.mu.Unlock()
}

func (m *InitContext) SetStorage(s storage.Provider) {
	m.mu.Lock()
	m.storage = s
	m.mu.Unlock()
}

func (m *InitContext) RegisterPacketHandler(e enums.EMsg, h socket.Handler) {
	m.mu.Lock()
	m.packetHandlers[e] = h
	m.mu.Unlock()
}

func (m *InitContext) UnregisterPacketHandler(e enums.EMsg) {
	m.mu.Lock()
	delete(m.packetHandlers, e)
	m.mu.Unlock()
}

func (m *InitContext) RegisterServiceHandler(method string, h socket.Handler) {
	m.mu.Lock()
	m.serviceHandlers[method] = h
	m.mu.Unlock()
}

func (m *InitContext) UnregisterServiceHandler(method string) {
	m.mu.Lock()
	delete(m.serviceHandlers, method)
	m.mu.Unlock()
}

func (m *InitContext) Module(name string) module.Module {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.modules[name]
}

func (m *InitContext) SetModule(name string, mod module.Module) {
	m.mu.Lock()
	m.modules[name] = mod
	m.mu.Unlock()
}

func (m *InitContext) AssertPacketHandlerRegistered(t *testing.T, e enums.EMsg) {
	t.Helper()
	m.mu.RLock()
	defer m.mu.RUnlock()
	_, ok := m.packetHandlers[e]
	assert.True(t, ok, "Expected packet handler for %v to be registered", e)
}

func (m *InitContext) AssertPacketHandlerUnregistered(t *testing.T, e enums.EMsg) {
	t.Helper()
	m.mu.RLock()
	defer m.mu.RUnlock()
	_, ok := m.packetHandlers[e]
	assert.False(t, ok, "Expected packet handler for %v to be unregistered", e)
}

func (m *InitContext) AssertServiceHandlerRegistered(t *testing.T, method string) {
	t.Helper()
	m.mu.RLock()
	defer m.mu.RUnlock()
	_, ok := m.serviceHandlers[method]
	assert.True(t, ok, "Expected service handler %q to be registered", method)
}

func (m *InitContext) AssertServiceHandlerUnregistered(t *testing.T, method string) {
	t.Helper()
	m.mu.RLock()
	defer m.mu.RUnlock()
	_, ok := m.serviceHandlers[method]
	assert.False(t, ok, "Expected service handler %q to be unregistered", method)
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

// GetPacketHandler returns the registered packet handler for the given EMsg, if one exists.
func (m *InitContext) GetPacketHandler(e enums.EMsg) (socket.Handler, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	h, ok := m.packetHandlers[e]
	return h, ok
}

// GetServiceHandler returns the registered service handler for the given method, if one exists.
func (m *InitContext) GetServiceHandler(method string) (socket.Handler, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	h, ok := m.serviceHandlers[method]
	return h, ok
}

// AuthContext is a lightweight manual mock for module.AuthContext.
type AuthContext struct {
	MockCommunity *HTTPStub
	MockSteamID       id.ID
}

// NewAuthContext creates a new AuthContext with a clean HTTPStub.
func NewAuthContext(steamID id.ID) *AuthContext {
	return &AuthContext{
		MockCommunity: NewHTTPStub(),
		MockSteamID:       steamID,
	}
}

func (m *AuthContext) Community() community.Requester { return m.MockCommunity }
func (m *AuthContext) SteamID() id.ID                 { return m.MockSteamID }

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

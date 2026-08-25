// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package mock

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/lemon4ksan/aoni"
	"github.com/lemon4ksan/aoni/mod"
	"github.com/lemon4ksan/aoni/request"
	"github.com/lemon4ksan/foundation/async/event"
	"github.com/lemon4ksan/foundation/async/log"
	"github.com/lemon4ksan/foundation/codec/json"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"google.golang.org/protobuf/proto"

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

type AuthModule struct {
	Module
}

func (m *AuthModule) StartAuthed(ctx context.Context, authCtx module.AuthContext) error {
	args := m.Called(ctx, authCtx)
	return args.Error(0)
}

type requesterDoer struct {
	r request.Requester
}

func (d *requesterDoer) Do(req *http.Request) (*http.Response, error) {
	return d.r.Request(req.Context(), req.Method, req.URL.String(), mod.Custom(func(r aoni.Request) {
		for k, v := range req.Header {
			r.SetHeader(k, strings.Join(v, ","))
		}

		r.SetBodyStream(req.Body, req.ContentLength)
	}))
}

type InitContext struct {
	mu              sync.RWMutex
	eventBus        *event.Bus
	logger          log.Logger
	packetHandlers  map[enums.EMsg]socket.Handler
	serviceHandlers map[string]socket.Handler
	modules         map[string]module.Module
	storage         storage.Provider
	service         *ServiceMock
	rest            request.Requester
}

func NewInitContext() *InitContext {
	return &InitContext{
		eventBus:        event.New(),
		logger:          log.Discard,
		packetHandlers:  make(map[enums.EMsg]socket.Handler),
		serviceHandlers: make(map[string]socket.Handler),
		modules:         make(map[string]module.Module),
		service:         NewServiceMock(),
		storage:         memory.New(),
	}
}

func (m *InitContext) MockService() *ServiceMock { return m.service }
func (m *InitContext) Bus() *event.Bus           { return m.eventBus }
func (m *InitContext) Logger() log.Logger        { return m.logger }
func (m *InitContext) Service() service.Doer     { return m.service }

func (m *InitContext) Storage() storage.Provider {
	m.mu.RLock()
	defer m.mu.RUnlock()

	return m.storage
}

func (m *InitContext) Rest() request.Requester {
	m.mu.RLock()
	defer m.mu.RUnlock()

	return m.rest
}

func (m *InitContext) SetService(s *ServiceMock) {
	m.mu.Lock()
	m.service = s
	m.mu.Unlock()
}

func (m *InitContext) SetRest(r request.Requester) {
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

func (m *InitContext) EmitRawPacket(t *testing.T, e enums.EMsg, payload []byte) {
	t.Helper()
	m.mu.RLock()
	handler, ok := m.packetHandlers[e]
	m.mu.RUnlock()

	if !ok {
		t.Fatalf("no handler registered for packet %v", e)
	}

	handler(&protocol.Packet{
		EMsg:    e,
		Payload: payload,
	})
}

func (m *InitContext) Subscribe(eventID any, handler func(raw []byte)) (unsubscribe func()) {
	switch v := eventID.(type) {
	case string:
		m.RegisterServiceHandler(v, func(p *protocol.Packet) {
			handler(p.Payload)
		})

		return func() {
			m.UnregisterServiceHandler(v)
		}

	case enums.EMsg:
		m.RegisterPacketHandler(v, func(p *protocol.Packet) {
			handler(p.Payload)
		})

		return func() {
			m.UnregisterPacketHandler(v)
		}

	case int:
		emsg := enums.EMsg(v)
		m.RegisterPacketHandler(emsg, func(p *protocol.Packet) {
			handler(p.Payload)
		})

		return func() {
			m.UnregisterPacketHandler(emsg)
		}

	case uint32:
		emsg := enums.EMsg(v)
		m.RegisterPacketHandler(emsg, func(p *protocol.Packet) {
			handler(p.Payload)
		})

		return func() {
			m.UnregisterPacketHandler(emsg)
		}

	default:
		return func() {}
	}
}

func (m *InitContext) Invoke(ctx context.Context, op any, payload []byte) ([]byte, error) {
	if m.service != nil {
		var req *tr.Request
		switch v := op.(type) {
		case string:
			cleanOp := strings.TrimSuffix(v, "#1")
			parts := strings.Split(cleanOp, ".")
			if len(parts) == 2 {
				req = tr.NewRequest(&service.UnifiedTarget{
					Interface: parts[0],
					Method:    parts[1],
					Version:   1,
					IsService: true,
				}, bytes.NewReader(payload))
			} else {
				req = tr.NewRequest(&service.UnifiedTarget{
					Interface: v,
					Method:    "",
					Version:   1,
					IsService: true,
				}, bytes.NewReader(payload))
			}
		case enums.EMsg:
			req = tr.NewRequest(v, bytes.NewReader(payload))
		case int:
			req = tr.NewRequest(enums.EMsg(v), bytes.NewReader(payload))
		case uint32:
			req = tr.NewRequest(enums.EMsg(v), bytes.NewReader(payload))
		}

		if req != nil {
			resp, err := m.service.Do(ctx, req)
			if err != nil {
				return nil, err
			}
			defer resp.Body.Close()

			return io.ReadAll(resp.Body)
		}
	}

	return nil, nil
}

func (m *InitContext) Notify(ctx context.Context, op any, payload []byte) error {
	if m.service != nil {
		var req *tr.Request
		switch v := op.(type) {
		case string:
			cleanOp := strings.TrimSuffix(v, "#1")
			parts := strings.Split(cleanOp, ".")
			if len(parts) == 2 {
				req = tr.NewRequest(&service.UnifiedTarget{
					Interface: parts[0],
					Method:    parts[1],
					Version:   1,
					IsService: true,
				}, bytes.NewReader(payload))
			} else {
				req = tr.NewRequest(&service.UnifiedTarget{
					Interface: v,
					Method:    "",
					Version:   1,
					IsService: true,
				}, bytes.NewReader(payload))
			}
		case enums.EMsg:
			req = tr.NewRequest(v, bytes.NewReader(payload))
		case int:
			req = tr.NewRequest(enums.EMsg(v), bytes.NewReader(payload))
		case uint32:
			req = tr.NewRequest(enums.EMsg(v), bytes.NewReader(payload))
		}

		if req != nil {
			_, err := m.service.Do(ctx, req)
			return err
		}
	}

	return nil
}

func (m *InitContext) GetPacketHandler(e enums.EMsg) (socket.Handler, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	h, ok := m.packetHandlers[e]

	return h, ok
}

func (m *InitContext) GetServiceHandler(method string) (socket.Handler, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	h, ok := m.serviceHandlers[method]

	return h, ok
}

type AuthContext struct {
	MockCommunity *HTTPStub
	MockSteamID   id.ID
}

func NewAuthContext(steamID id.ID) *AuthContext {
	return &AuthContext{
		MockCommunity: NewHTTPStub(),
		MockSteamID:   steamID,
	}
}

func (m *AuthContext) Community() community.Requester { return m.MockCommunity }
func (m *AuthContext) SteamID() id.ID                 { return m.MockSteamID }

func (m *AuthContext) String() string {
	return "mock.AuthContext{...}"
}

func (m *AuthContext) GoString() string {
	return m.String()
}

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

// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package test

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/lemon4ksan/g-man/pkg/steam/protocol"
	"github.com/lemon4ksan/g-man/pkg/steam/service"
	tr "github.com/lemon4ksan/g-man/pkg/steam/transport"
	"google.golang.org/protobuf/proto"
)

type MockRequester struct {
	mu          sync.Mutex
	Calls       []*tr.Request
	OnDo        func(req *tr.Request) (*tr.Response, error)
	OnSessionID func(string) string

	ResponseErr  error
	ResponseErrs map[string]error

	protoResponses map[string]proto.Message
	jsonResponses  map[string]any
	rawResponses   map[string][]byte
}

func NewMockRequester() *MockRequester {
	return &MockRequester{
		ResponseErrs:   make(map[string]error),
		protoResponses: make(map[string]proto.Message),
		jsonResponses:  make(map[string]any),
		rawResponses:   make(map[string][]byte),
	}
}

func (m *MockRequester) Do(ctx context.Context, req *tr.Request) (*tr.Response, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.Calls = append(m.Calls, req)

	if m.OnDo != nil {
		resp, err := m.OnDo(req)
		if resp != nil || err != nil {
			return resp, err
		}
	}

	if m.ResponseErr != nil {
		return nil, m.ResponseErr
	}

	methodName := m.identifyTarget(req.Target())

	if err, ok := m.ResponseErrs[methodName]; ok && err != nil {
		return nil, err
	}

	if data, ok := m.jsonResponses[methodName]; ok {
		body, _ := json.Marshal(data)
		return tr.NewResponse(body, tr.HTTPMetadata{StatusCode: 200}), nil
	}

	if msg, ok := m.protoResponses[methodName]; ok {
		body, _ := proto.Marshal(msg)
		return tr.NewResponse(body, tr.SocketMetadata{Result: protocol.EResult_OK}), nil
	}

	if body, ok := m.rawResponses[methodName]; ok {
		return tr.NewResponse(body, tr.HTTPMetadata{StatusCode: 200}), nil
	}

	return tr.NewResponse(nil, tr.SocketMetadata{Result: protocol.EResult_OK}), nil
}

func (m *MockRequester) SetJSONResponse(iface, method string, resp any) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.jsonResponses[fmt.Sprintf("%s/%s", iface, method)] = resp
}

func (m *MockRequester) SetProtoResponse(iface, method string, resp proto.Message) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.protoResponses[fmt.Sprintf("%s.%s", iface, method)] = resp
}

func (m *MockRequester) SetLegacyResponse(message protocol.EMsg, resp proto.Message) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.protoResponses[message.String()] = resp
}

func (m *MockRequester) SetRawResponse(key string, body []byte) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.rawResponses[key] = body
}

func (m *MockRequester) GetLastRequest() *tr.Request {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.Calls) == 0 {
		return nil
	}
	return m.Calls[len(m.Calls)-1]
}

func (m *MockRequester) GetLastCall(out proto.Message) *tr.Request {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.Calls) == 0 {
		return nil
	}

	req := m.Calls[len(m.Calls)-1]

	if out != nil && req.Body() != nil {
		_ = proto.Unmarshal(req.Body(), out)
	}
	return req
}

func (m *MockRequester) SessionID(targetURI string) string {
	if m.OnSessionID != nil {
		return m.OnSessionID(targetURI)
	}
	return "mock_session_id"
}

func (m *MockRequester) ClearCalls() {
	m.mu.Lock()
	defer m.mu.Unlock()
	clear(m.Calls)
}

func (m *MockRequester) CallsCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.Calls)
}


func (m *MockRequester) identifyTarget(target any) string {
	switch t := target.(type) {
	case service.UnifiedTarget:
		return fmt.Sprintf("%s.%s", t.Interface, t.Method)
	case service.WebAPITarget:
		return fmt.Sprintf("%s/%s", t.Interface, t.Method)
	case service.LegacyTarget:
		return t.String()
	case fmt.Stringer:
		return t.String()
	default:
		return fmt.Sprintf("%v", t)
	}
}

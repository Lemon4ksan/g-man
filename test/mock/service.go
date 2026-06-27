// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package mock

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"

	"github.com/lemon4ksan/aoni"
	"github.com/lemon4ksan/g-man/pkg/steam/protocol/enums"
	"github.com/lemon4ksan/g-man/pkg/steam/service"
	tr "github.com/lemon4ksan/g-man/pkg/steam/transport"
	"google.golang.org/protobuf/proto"
)

type restCall struct {
	Method string
	Path   string
	Body   []byte
	Query  any
}

type restResponse struct {
	Status int
	Body   []byte
	Header http.Header
}

type ServiceMock struct {
	mu    sync.Mutex
	Calls []*tr.Request

	restCalls     []restCall
	restResponses map[string]restResponse

	OnDo        func(req *tr.Request) (*tr.Response, error)
	OnRest      func(method, path string, body any) (*http.Response, error)
	OnSessionID func(string) string

	OnGetOrRegisterAPIKey func(ctx context.Context, domain string) (string, error)

	ResponseErr  error
	ResponseErrs map[string]error

	protoResponses map[string]proto.Message
	jsonResponses  map[string]any
	rawResponses   map[string][]byte

	BaseResponseFunc func() aoni.BaseResponse
}

func NewServiceMock() *ServiceMock {
	return &ServiceMock{
		ResponseErrs:   make(map[string]error),
		protoResponses: make(map[string]proto.Message),
		jsonResponses:  make(map[string]any),
		rawResponses:   make(map[string][]byte),
	}
}

func (m *ServiceMock) Do(ctx context.Context, req *tr.Request) (*tr.Response, error) {
	m.mu.Lock()
	m.Calls = append(m.Calls, req)
	m.mu.Unlock()

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

	m.mu.Lock()
	defer m.mu.Unlock()

	if err, ok := m.ResponseErrs[methodName]; ok && err != nil {
		return nil, err
	}

	if data, ok := m.jsonResponses[methodName]; ok {
		body, _ := json.Marshal(data)
		return tr.NewResponse(io.NopCloser(bytes.NewReader(body)), tr.HTTPMetadata{StatusCode: 200}), nil
	}

	if msg, ok := m.protoResponses[methodName]; ok {
		body, _ := proto.Marshal(msg)
		return tr.NewResponse(io.NopCloser(bytes.NewReader(body)), tr.SocketMetadata{Result: enums.EResult_OK}), nil
	}

	if body, ok := m.rawResponses[methodName]; ok {
		return tr.NewResponse(io.NopCloser(bytes.NewReader(body)), tr.HTTPMetadata{StatusCode: 200}), nil
	}

	return tr.NewResponse(io.NopCloser(bytes.NewReader(nil)), tr.SocketMetadata{Result: enums.EResult_OK}), nil
}

func (m *ServiceMock) Request(
	ctx context.Context,
	method, path string,
	mods ...aoni.RequestModifier,
) (*http.Response, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	var bodyBytes []byte
	dummyReq, _ := http.NewRequestWithContext(ctx, method, path, nil)
	for _, mod := range mods {
		mod(dummyReq)
	}
	if dummyReq.Body != nil {
		bodyBytes, _ = io.ReadAll(dummyReq.Body)
		dummyReq.Body.Close()
	}

	m.restCalls = append(m.restCalls, restCall{method, path, bodyBytes, nil})

	if m.OnRest != nil {
		if len(bodyBytes) > 0 {
			return m.OnRest(method, path, bodyBytes)
		}
		return m.OnRest(method, path, nil)
	}

	key := fmt.Sprintf("%s:%s", method, path)
	if err, ok := m.ResponseErrs[key]; ok {
		return nil, err
	}

	respData, ok := m.restResponses[key]
	if !ok {
		respData = restResponse{Status: http.StatusOK, Body: []byte("{}")}
	}

	dummyReq2, _ := http.NewRequest(method, path, bytes.NewReader(bodyBytes))
	for _, mod := range mods {
		mod(dummyReq2)
	}

	return &http.Response{
		StatusCode: respData.Status,
		Body:       io.NopCloser(bytes.NewReader(respData.Body)),
		Header:     respData.Header,
		Request:    dummyReq2,
	}, nil
}

// GetOrRegisterAPIKey checks for the presence of a WebAPI key or registers a new one.
func (m *ServiceMock) GetOrRegisterAPIKey(ctx context.Context, domain string) (string, error) {
	if m.OnGetOrRegisterAPIKey != nil {
		return m.OnGetOrRegisterAPIKey(ctx, domain)
	}
	return "", nil
}

func (m *ServiceMock) SessionID(targetURI string) string {
	if m.OnSessionID != nil {
		return m.OnSessionID(targetURI)
	}
	return "mock_session_id"
}

func (m *ServiceMock) SetErrorResponse(iface, method string, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.ResponseErrs[fmt.Sprintf("%s/%s", iface, method)] = err
}

func (m *ServiceMock) SetJSONResponse(iface, method string, resp any) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.jsonResponses[fmt.Sprintf("%s/%s", iface, method)] = resp
}

func (m *ServiceMock) SetProtoResponse(iface, method string, resp proto.Message) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.protoResponses[fmt.Sprintf("%s.%s", iface, method)] = resp
}

func (m *ServiceMock) SetLegacyResponse(message enums.EMsg, resp proto.Message) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.protoResponses[message.String()] = resp
}

func (m *ServiceMock) SetRawResponse(key string, body []byte) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.rawResponses[key] = body
}

func (m *ServiceMock) GetLastRequest() *tr.Request {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.Calls) == 0 {
		return nil
	}
	return m.Calls[len(m.Calls)-1]
}

func (m *ServiceMock) CallsCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.Calls)
}

func (m *ServiceMock) ClearCalls() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Calls = nil
}

func (m *ServiceMock) GetLastCall(out proto.Message) *tr.Request {
	m.mu.Lock()
	defer m.mu.Unlock()

	if len(m.Calls) == 0 {
		return nil
	}

	req := m.Calls[len(m.Calls)-1]

	if out != nil && req.Body != nil {
		bodyBytes, _ := io.ReadAll(req.Body)
		_ = proto.Unmarshal(bodyBytes, out)

		req.Body = bytes.NewReader(bodyBytes)
	}

	return req
}

func (m *ServiceMock) identifyTarget(target any) string {
	switch t := target.(type) {
	case *service.UnifiedTarget:
		return fmt.Sprintf("%s.%s", t.Interface, t.Method)
	case *service.WebAPITarget:
		return fmt.Sprintf("%s/%s", t.Interface, t.Method)
	case *service.LegacyTarget:
		return t.String()
	default:
		return fmt.Sprintf("%v", target)
	}
}

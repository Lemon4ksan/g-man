// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package mock

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"

	"github.com/lemon4ksan/foundation/codec/json"
	"github.com/lemon4ksan/aoni"
	"google.golang.org/protobuf/proto"

	"github.com/lemon4ksan/g-man/pkg/steam/protocol"
	"github.com/lemon4ksan/g-man/pkg/steam/protocol/enums"
	"github.com/lemon4ksan/g-man/pkg/steam/service"
	tr "github.com/lemon4ksan/g-man/pkg/steam/transport"
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

	OnDo                  func(req *tr.Request) (*tr.Response, error)
	OnRest                func(method, path string, body any) (*http.Response, error)
	OnSessionID           func(string) string
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
	stdReq := aoni.NewStdRequest(dummyReq)

	for _, m := range mods {
		m.Apply(stdReq)
	}

	if dummyReq.Body != nil {
		bodyBytes, _ = io.ReadAll(dummyReq.Body)
		dummyReq.Body.Close()
	}

	m.restCalls = append(m.restCalls, restCall{method, path, bodyBytes, nil})

	if m.OnRest != nil {
		var (
			resp *http.Response
			err  error
		)

		if len(bodyBytes) > 0 {
			resp, err = m.OnRest(method, path, bodyBytes)
		} else {
			resp, err = m.OnRest(method, path, nil)
		}

		if resp != nil && resp.Request == nil {
			resp.Request = dummyReq
		}

		return resp, err
	}

	if err := m.findError(method, path); err != nil {
		return nil, err
	}

	var respBody []byte
	var respStatus = http.StatusOK
	var respHeader http.Header

	if body, ok := m.findJSONResponse(method, path); ok {
		respBody = body
	} else {
		key := fmt.Sprintf("%s:%s", method, path)
		if respData, ok := m.restResponses[key]; ok {
			respBody = respData.Body
			respStatus = respData.Status
			respHeader = respData.Header
		} else {
			respBody = []byte("{}")
		}
	}

	dummyReq2, _ := http.NewRequestWithContext(ctx, method, path, bytes.NewReader(bodyBytes))
	stdReq2 := aoni.NewStdRequest(dummyReq2)

	for _, m := range mods {
		m.Apply(stdReq2)
	}

	return &http.Response{
		StatusCode: respStatus,
		Body:       io.NopCloser(bytes.NewReader(respBody)),
		Header:     respHeader,
		Request:    dummyReq2,
	}, nil
}

func (m *ServiceMock) findError(method, path string) error {
	pathOnly := strings.Split(path, "?")[0]
	cleanPath := strings.Trim(pathOnly, "/")

	key := fmt.Sprintf("%s:%s", method, path)
	if err, ok := m.ResponseErrs[key]; ok && err != nil {
		return err
	}
	if err, ok := m.ResponseErrs[path]; ok && err != nil {
		return err
	}
	if err, ok := m.ResponseErrs[cleanPath]; ok && err != nil {
		return err
	}

	for k, err := range m.ResponseErrs {
		if err == nil {
			continue
		}
		if strings.Contains(cleanPath, k) || strings.Contains(k, cleanPath) {
			return err
		}
	}

	parts := strings.Split(cleanPath, "/")
	if len(parts) > 0 {
		mName := parts[0]
		for k, err := range m.ResponseErrs {
			if (k == mName || strings.HasSuffix(k, "/"+mName)) && err != nil {
				return err
			}
		}
	}

	return nil
}

func (m *ServiceMock) findJSONResponse(method, path string) ([]byte, bool) {
	pathOnly := strings.Split(path, "?")[0]
	cleanPath := strings.Trim(pathOnly, "/")

	key := fmt.Sprintf("%s:%s", method, path)
	if respData, ok := m.restResponses[key]; ok {
		return respData.Body, true
	}
	if data, ok := m.jsonResponses[path]; ok {
		body, _ := json.Marshal(data)
		return body, true
	}
	if data, ok := m.jsonResponses[cleanPath]; ok {
		body, _ := json.Marshal(data)
		return body, true
	}

	for k, data := range m.jsonResponses {
		if strings.Contains(cleanPath, k) || strings.Contains(k, cleanPath) {
			body, _ := json.Marshal(data)
			return body, true
		}
	}

	parts := strings.Split(cleanPath, "/")
	if len(parts) > 0 {
		mName := parts[0]
		for k, data := range m.jsonResponses {
			if k == mName || strings.HasSuffix(k, "/"+mName) {
				body, _ := json.Marshal(data)
				return body, true
			}
		}
	}

	return nil, false
}

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
		_ = protocol.UnmarshalProto(bodyBytes, out)
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

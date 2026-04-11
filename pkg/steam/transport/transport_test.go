// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package transport

import (
	"io"
	"net/http"
	"net/url"
	"testing"

	"github.com/lemon4ksan/g-man/pkg/steam/protocol/enums"
	"github.com/lemon4ksan/g-man/pkg/steam/socket"
)

type mockTarget struct {
	name string
}

func (m mockTarget) String() string { return m.name }

type mockHTTPTarget struct {
	mockTarget
	method string
	path   string
}

func (m mockHTTPTarget) HTTPMethod() string { return m.method }
func (m mockHTTPTarget) HTTPPath() string   { return m.path }

type mockSocketTarget struct {
	mockTarget
	eMsg enums.EMsg
	name string
}

func (m mockSocketTarget) EMsg(isAuth bool) enums.EMsg { return m.eMsg }
func (m mockSocketTarget) ObjectName() string          { return m.name }

type mockHTTPDoer struct {
	doFunc func(req *http.Request) (*http.Response, error)
}

func (m *mockHTTPDoer) Do(req *http.Request) (*http.Response, error) {
	return m.doFunc(req)
}

type mockSession struct {
	socket.Session
	isAuth bool
}

func (m *mockSession) IsAuthenticated() bool { return m.isAuth }
func (m *mockSession) SteamID() uint64       { return 12345 }
func (m *mockSession) SessionID() int32      { return 67890 }

type mockEHeader struct {
	result    enums.EResult
	sourceJob uint64
}

func (m mockEHeader) GetEResult() enums.EResult     { return m.result }
func (m mockEHeader) GetSourceJob() uint64          { return m.sourceJob }
func (m mockEHeader) GetTargetJob() uint64          { return 0 }
func (m mockEHeader) SerializeTo(w io.Writer) error { return nil }

func TestRequest_Builder(t *testing.T) {
	target := mockTarget{name: "test_target"}
	body := []byte("hello steam")

	req := NewRequest(target, body).
		WithParam("key1", "val1").
		WithParams(url.Values{"key2": {"val2"}}).
		WithHeader("X-Token", "123")

	if req.Target().String() != "test_target" {
		t.Errorf("Expected target to be 'test_target', got '%s'", req.Target().String())
	}
	if string(req.Body()) != "hello steam" {
		t.Errorf("Expected body 'hello steam', got '%s'", string(req.Body()))
	}
	if req.Params().Get("key1") != "val1" || req.Params().Get("key2") != "val2" {
		t.Errorf("Expected params to be correctly set")
	}
	if req.Header().Get("X-Token") != "123" {
		t.Errorf("Expected header X-Token to be '123'")
	}
}

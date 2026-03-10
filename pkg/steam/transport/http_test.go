// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package transport

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"testing"

	"github.com/lemon4ksan/g-man/pkg/steam/protocol"
)

func TestHTTPTransport_ParseEResult(t *testing.T) {
	trans := &HTTPTransport{}

	tests := []struct {
		name     string
		header   string
		expected protocol.EResult
	}{
		{"With Header OK", "1", protocol.EResult_OK},
		{"With Header Fail", "2", protocol.EResult_Fail},
		{"Without Header", "", protocol.EResult_OK},
		{"Invalid Header", "invalid", protocol.EResult_OK},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp := &http.Response{Header: make(http.Header)}
			if tt.header != "" {
				resp.Header.Set("x-eresult", tt.header)
			}
			result := trans.parseEResult(resp)
			if result != tt.expected {
				t.Errorf("Expected EResult %v, got %v", tt.expected, result)
			}
		})
	}
}

func TestHTTPTransport_Do(t *testing.T) {
	doer := &mockHTTPDoer{
		doFunc: func(req *http.Request) (*http.Response, error) {
			if req.Header.Get("Accept") == "" {
				t.Errorf("Expected Accept header to be set by RequestModifier")
			}
			if req.Header.Get("User-Agent") == "" {
				t.Errorf("Expected User-Agent to be set")
			}

			resp := &http.Response{
				StatusCode: 200,
				Header:     make(http.Header),
				Body:       io.NopCloser(bytes.NewReader([]byte(`{"response":{}}`))),
			}
			resp.Header.Set("x-eresult", "1")
			return resp, nil
		},
	}

	trans := NewHTTPTransport(doer, "https://api.steampowered.com")
	defer trans.Close()

	target := mockHTTPTarget{
		method: "GET",
		path:   "/IPlayerService/GetOwnedGames/v1/",
	}

	req := NewRequest(context.Background(), target, []byte("proto_data"))
	resp, err := trans.Do(req)

	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if resp.StatusCode != 200 {
		t.Errorf("Expected StatusCode 200, got %d", resp.StatusCode)
	}
	if resp.Result != protocol.EResult_OK {
		t.Errorf("Expected Result OK, got %v", resp.Result)
	}
	if string(resp.Body) != `{"response":{}}` {
		t.Errorf("Unexpected body: %s", string(resp.Body))
	}
}

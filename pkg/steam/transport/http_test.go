// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package transport

import (
	"bytes"
	"io"
	"net/http"
	"testing"

	"github.com/lemon4ksan/g-man/pkg/steam/protocol/enums"
)

func TestHTTPTransport_ParseEResult(t *testing.T) {
	tr := &HTTPTransport{}

	tests := []struct {
		name     string
		header   string
		expected enums.EResult
	}{
		{"With Header OK", "1", enums.EResult_OK},
		{"With Header Fail", "2", enums.EResult_Fail},
		{"Without Header", "", enums.EResult_OK},
		{"Invalid Header", "invalid", enums.EResult_OK},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp := &http.Response{Header: make(http.Header)}
			if tt.header != "" {
				resp.Header.Set("x-eresult", tt.header)
			}

			result := tr.parseEResult(resp)
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
				StatusCode: http.StatusOK,
				Header:     make(http.Header),
				Body:       io.NopCloser(bytes.NewReader([]byte(`{"response":{}}`))),
			}
			resp.Header.Set("x-eresult", "1")

			return resp, nil
		},
	}

	tr := NewHTTPTransport(doer, "https://api.steampowered.com")

	target := mockHTTPTarget{
		method: "GET",
		path:   "/IPlayerService/GetOwnedGames/v1/",
	}

	req := NewRequest(target, []byte("proto_data"))

	resp, err := tr.Do(t.Context(), req)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	meta, _ := resp.HTTP()
	if meta.StatusCode != http.StatusOK {
		t.Errorf("Expected StatusCode 200, got %d", meta.StatusCode)
	}

	if meta.Result != enums.EResult_OK {
		t.Errorf("Expected Result OK, got %v", meta.Result)
	}

	if string(resp.Body) != `{"response":{}}` {
		t.Errorf("Unexpected body: %s", string(resp.Body))
	}
}

// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package openid

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
)

type mockTransport struct {
	responses map[string]*http.Response
	calls     []string
}

func (m *mockTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	url := req.URL.String()
	path := req.URL.Host + req.URL.Path

	m.calls = append(m.calls, fmt.Sprintf("%s %s", req.Method, url))

	if res, ok := m.responses[url]; ok {
		res.Request = req
		return res, nil
	}

	if res, ok := m.responses[path]; ok {
		res.Request = req
		return res, nil
	}

	return nil, fmt.Errorf("no mock response for %s", url)
}

func TestLogin(t *testing.T) {
	targetSite := "https://skin-site.com/login"
	steamLoginURL := "https://steamcommunity.com/openid/login"
	finalSiteURL := "https://skin-site.com/auth?confirmed=1"
	evilSiteURL := "https://evil-site.com/"

	tests := []struct {
		name          string
		setupMock     func() *mockTransport
		wantErr       error
		expectedCalls []string
	}{
		{
			name: "Success_FullFlow",
			setupMock: func() *mockTransport {
				m := &mockTransport{responses: make(map[string]*http.Response)}

				m.responses[targetSite] = &http.Response{
					StatusCode: http.StatusFound,
					Header:     http.Header{"Location": {steamLoginURL}},
					Body:       io.NopCloser(strings.NewReader("")),
				}

				formHTML := `<html><body><form id="openidForm" action="/openid/login" method="POST">
					<input type="hidden" name="openid.mode" value="checkid_setup">
					</form></body></html>`

				resp1 := stringResponse(200, formHTML)
				defer resp1.Body.Close()

				m.responses[steamLoginURL] = resp1

				m.responses["steamcommunity.com/openid/login"] = &http.Response{
					StatusCode: http.StatusFound,
					Header:     http.Header{"Location": {finalSiteURL}},
					Body:       io.NopCloser(strings.NewReader("")),
				}

				resp2 := stringResponse(200, "Success")
				defer resp2.Body.Close()

				m.responses[finalSiteURL] = resp2

				return m
			},
			wantErr: nil,
		},
		{
			name: "AlreadyAuthenticated_NoRedirect",
			setupMock: func() *mockTransport {
				m := &mockTransport{responses: make(map[string]*http.Response)}

				resp := stringResponse(200, "Welcome back")
				defer resp.Body.Close()

				m.responses[targetSite] = resp

				return m
			},
			wantErr: nil,
		},
		{
			name: "Fail_NotSignedInToSteam",
			setupMock: func() *mockTransport {
				m := &mockTransport{responses: make(map[string]*http.Response)}
				m.responses[targetSite] = &http.Response{
					StatusCode: http.StatusFound,
					Header:     http.Header{"Location": {steamLoginURL}},
					Body:       http.NoBody,
				}

				resp := stringResponse(200, `<form id="loginForm"></form>`)
				defer resp.Body.Close()

				m.responses[steamLoginURL] = resp

				return m
			},
			wantErr: ErrNotSignedIn,
		},
		{
			name: "Fail_WrongHost",
			setupMock: func() *mockTransport {
				m := &mockTransport{responses: make(map[string]*http.Response)}
				m.responses[targetSite] = &http.Response{
					StatusCode: http.StatusFound,
					Header:     http.Header{"Location": {evilSiteURL}},
					Body:       io.NopCloser(strings.NewReader("")),
				}

				resp := stringResponse(200, "Evil content")
				defer resp.Body.Close()

				m.responses[evilSiteURL] = resp

				return m
			},
			wantErr: ErrWrongHost,
		},
		{
			name: "Fail_NoFormOnPage",
			setupMock: func() *mockTransport {
				m := &mockTransport{responses: make(map[string]*http.Response)}
				m.responses[targetSite] = &http.Response{
					StatusCode: http.StatusFound,
					Header:     http.Header{"Location": {steamLoginURL}},
					Body:       http.NoBody,
				}

				resp := stringResponse(200, `<html><body>No form here</body></html>`)
				defer resp.Body.Close()

				m.responses[steamLoginURL] = resp

				return m
			},
			wantErr: ErrNoForm,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := tt.setupMock()

			oldTransport := http.DefaultTransport
			http.DefaultTransport = mock

			defer func() { http.DefaultTransport = oldTransport }()

			_, err := Login(context.Background(), targetSite, nil)

			if tt.wantErr != nil {
				if err == nil || !errors.Is(err, tt.wantErr) && !strings.Contains(err.Error(), tt.wantErr.Error()) {
					t.Errorf("Login() error = %v, wantErr %v", err, tt.wantErr)
				}
			} else if err != nil {
				t.Errorf("Login() unexpected error: %v", err)
			}
		})
	}
}

func stringResponse(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

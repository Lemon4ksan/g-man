// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package openid_test

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/lemon4ksan/aoni"
	"github.com/stretchr/testify/assert"

	"github.com/lemon4ksan/g-man/pkg/steam/community/openid"
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

type errorReader struct{}

func (errorReader) Read(p []byte) (n int, err error) {
	return 0, errors.New("read error")
}

func TestLogin(t *testing.T) {
	t.Parallel()

	targetSite := "https://skin-site.com/login"
	steamLoginURL := "https://steamcommunity.com/openid/login"
	finalSiteURL := "https://skin-site.com/auth?confirmed=1"
	evilSiteURL := "https://evil-site.com/"

	tests := []struct {
		name      string
		setupMock func() *mockTransport
		wantErr   error
	}{
		{
			name: "success_full_flow",
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
				m.responses[steamLoginURL] = resp1

				m.responses["steamcommunity.com/openid/login"] = &http.Response{
					StatusCode: http.StatusFound,
					Header:     http.Header{"Location": {finalSiteURL}},
					Body:       io.NopCloser(strings.NewReader("")),
				}

				resp2 := stringResponse(200, "Success")
				m.responses[finalSiteURL] = resp2

				return m
			},
			wantErr: nil,
		},
		{
			name: "already_authenticated_no_redirect",
			setupMock: func() *mockTransport {
				m := &mockTransport{responses: make(map[string]*http.Response)}

				resp := stringResponse(200, "Welcome back")
				m.responses[targetSite] = resp

				return m
			},
			wantErr: nil,
		},
		{
			name: "fail_not_signed_in_to_steam",
			setupMock: func() *mockTransport {
				m := &mockTransport{responses: make(map[string]*http.Response)}
				m.responses[targetSite] = &http.Response{
					StatusCode: http.StatusFound,
					Header:     http.Header{"Location": {steamLoginURL}},
					Body:       http.NoBody,
				}

				resp := stringResponse(200, `<form id="loginForm"></form>`)
				m.responses[steamLoginURL] = resp

				return m
			},
			wantErr: openid.ErrNotSignedIn,
		},
		{
			name: "fail_wrong_host",
			setupMock: func() *mockTransport {
				m := &mockTransport{responses: make(map[string]*http.Response)}
				m.responses[targetSite] = &http.Response{
					StatusCode: http.StatusFound,
					Header:     http.Header{"Location": {evilSiteURL}},
					Body:       io.NopCloser(strings.NewReader("")),
				}

				resp := stringResponse(200, "Evil content")
				m.responses[evilSiteURL] = resp

				return m
			},
			wantErr: openid.ErrWrongHost,
		},
		{
			name: "fail_no_form_on_page",
			setupMock: func() *mockTransport {
				m := &mockTransport{responses: make(map[string]*http.Response)}
				m.responses[targetSite] = &http.Response{
					StatusCode: http.StatusFound,
					Header:     http.Header{"Location": {steamLoginURL}},
					Body:       http.NoBody,
				}

				resp := stringResponse(200, `<html><body>No form here</body></html>`)
				m.responses[steamLoginURL] = resp

				return m
			},
			wantErr: openid.ErrNoForm,
		},
		{
			name: "fail_initial_request_error",
			setupMock: func() *mockTransport {
				return &mockTransport{responses: make(map[string]*http.Response)}
			},
			wantErr: errors.New("initial request failed"),
		},
		{
			name: "fail_html_parsing_error",
			setupMock: func() *mockTransport {
				m := &mockTransport{responses: make(map[string]*http.Response)}
				m.responses[targetSite] = &http.Response{
					StatusCode: http.StatusFound,
					Header:     http.Header{"Location": {steamLoginURL}},
					Body:       http.NoBody,
				}

				m.responses[steamLoginURL] = &http.Response{
					StatusCode: 200,
					Body:       io.NopCloser(&errorReader{}),
				}

				return m
			},
			wantErr: errors.New("failed to parse HTML"),
		},
		{
			name: "success_edge_cases_inputs_and_action",
			setupMock: func() *mockTransport {
				m := &mockTransport{responses: make(map[string]*http.Response)}
				m.responses[targetSite] = &http.Response{
					StatusCode: http.StatusFound,
					Header:     http.Header{"Location": {steamLoginURL}},
					Body:       http.NoBody,
				}

				formHTML := `<html><body><form id="openidForm">
					<input type="hidden" value="nameless">
					<input type="hidden" name="" value="emptyname">
					<input type="hidden" name="action" value="custom_action">
					</form></body></html>`
				m.responses[steamLoginURL] = stringResponse(200, formHTML)

				m.responses["steamcommunity.com/openid/login"] = &http.Response{
					StatusCode: http.StatusFound,
					Header:     http.Header{"Location": {finalSiteURL}},
					Body:       http.NoBody,
				}
				m.responses[finalSiteURL] = stringResponse(200, "Success")

				return m
			},
			wantErr: nil,
		},
		{
			name: "success_invalid_action_url_fallback",
			setupMock: func() *mockTransport {
				m := &mockTransport{responses: make(map[string]*http.Response)}
				m.responses[targetSite] = &http.Response{
					StatusCode: http.StatusFound,
					Header:     http.Header{"Location": {steamLoginURL}},
					Body:       http.NoBody,
				}

				formHTML := `<html><body><form id="openidForm" action="http://[::1]:namedport">
					<input type="hidden" name="openid.mode" value="checkid_setup">
					</form></body></html>`
				m.responses[steamLoginURL] = stringResponse(200, formHTML)

				m.responses["steamcommunity.com/openid/login"] = &http.Response{
					StatusCode: http.StatusFound,
					Header:     http.Header{"Location": {finalSiteURL}},
					Body:       http.NoBody,
				}
				m.responses[finalSiteURL] = stringResponse(200, "Success")

				return m
			},
			wantErr: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := tt.setupMock()

			oldClient := aoni.DefaultClient
			aoni.DefaultClient = aoni.NewClient(&http.Client{
				Transport:     mock,
				CheckRedirect: aoni.DefaultRedirectPolicy(10),
			})

			defer func() { aoni.DefaultClient = oldClient }()

			_, err := openid.Login(t.Context(), targetSite, nil)

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

func TestLogin_InvalidTargetURL(t *testing.T) {
	t.Parallel()

	_, err := openid.Login(t.Context(), "http://[::1]:namedport", nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid target URL")
}

func stringResponse(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

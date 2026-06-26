// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// test/mock/net.go
package mock

import (
	"context"
	"net/http"

	"github.com/lemon4ksan/aoni"
	"github.com/lemon4ksan/g-man/pkg/steam/auth"
	"github.com/lemon4ksan/g-man/pkg/steam/socket"
	"github.com/stretchr/testify/mock"
)

type Authenticator struct {
	mock.Mock
}

func (m *Authenticator) LogOn(ctx context.Context, details *auth.LogOnDetails, server socket.CMServer) error {
	args := m.Called(ctx, details, server)
	return args.Error(0)
}

type Community struct {
	mock.Mock
}

func (m *Community) SessionID(baseURL string) string {
	args := m.Called(baseURL)
	return args.String(0)
}

func (m *Community) Request(ctx context.Context, method, path string, mods ...aoni.RequestModifier) (*http.Response, error) {
	args := m.Called(ctx, method, path, mods)
	resp, _ := args.Get(0).(*http.Response)
	return resp, args.Error(1)
}

func (m *Community) GetOrRegisterAPIKey(ctx context.Context, domain string) (string, error) {
	args := m.Called(ctx, domain)
	return args.String(0), args.Error(1)
}

type HTTPDoer struct {
	mock.Mock
}

func (m *HTTPDoer) Do(req *http.Request) (*http.Response, error) {
	args := m.Called(req)
	resp, _ := args.Get(0).(*http.Response)
	return resp, args.Error(1)
}

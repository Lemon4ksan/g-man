// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package mock

import (
	"context"
	"net/http"

	pb "github.com/lemon4ksan/g-man/pkg/protobuf/steam"
	"github.com/lemon4ksan/g-man/pkg/steam/socket"

	"github.com/stretchr/testify/mock"
)

type Session struct {
	socket.Session
	mock.Mock
}

func (m *Session) SteamID() uint64 {
	args := m.Called()
	return args.Get(0).(uint64)
}

func (m *Session) AccessToken() string {
	args := m.Called()
	return args.String(0)
}

func (m *Session) RefreshToken() string {
	args := m.Called()
	return args.String(0)
}

func (m *Session) SetAccessToken(token string) {
	m.Called(token)
}

type WebSession struct {
	mock.Mock
}

func (m *WebSession) HTTP() *http.Client {
	args := m.Called()
	return args.Get(0).(*http.Client)
}

func (m *WebSession) SessionID(baseURL string) string {
	args := m.Called(baseURL)
	return args.String(0)
}

func (m *WebSession) Verify(ctx context.Context) (bool, error) {
	args := m.Called(ctx)
	return args.Bool(0), args.Error(1)
}

func (m *WebSession) Authenticate(
	ctx context.Context,
	platformType pb.EAuthTokenPlatformType,
	refreshToken, accessToken string,
) error {
	args := m.Called(ctx, platformType, refreshToken, accessToken)
	return args.Error(0)
}

func (m *WebSession) IsAuthenticated() bool {
	args := m.Called()
	return args.Bool(0)
}

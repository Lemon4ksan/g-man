// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// test/mock/client.go
package mock

import (
	"net/http"
	"testing"

	"github.com/lemon4ksan/aoni"
	"github.com/lemon4ksan/miyako/log"
	"github.com/lemon4ksan/g-man/pkg/steam/client"
	"github.com/lemon4ksan/g-man/pkg/steam/client/router"
	"github.com/lemon4ksan/g-man/pkg/steam/client/session"
	"github.com/lemon4ksan/g-man/pkg/steam/community"
	"github.com/lemon4ksan/g-man/pkg/steam/id"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

type TestMocks struct {
	Auth *Authenticator
	Web  *WebSession
	Comm *Community
	Sock *Socket
	Http *HTTPDoer
}

func SetupTestClient(t *testing.T) (*client.Client, *TestMocks) {
	m := &TestMocks{
		Auth: new(Authenticator),
		Web:  new(WebSession),
		Comm: new(Community),
		Sock: new(Socket),
		Http: new(HTTPDoer),
	}

	sess := session.New(m.Sock, session.Config{
		HTTP:          m.Http,
		Authenticator: m.Auth,
		WebFactory: func(steamID id.ID, logger log.Logger, baseDoer aoni.HTTPDoer) session.WebSessionProvider {
			return m.Web
		},
		CommunityFactory: func(httpClient *http.Client, sess community.SessionProvider, logger log.Logger) community.Requester {
			return m.Comm
		},
	})

	opts := []client.Option{
		client.WithREST(aoni.NewClient(m.Http)),
		client.WithSocket(m.Sock),
		client.WithSession(sess),
		client.WithRouter(router.New(sess, m.Sock)),
		client.WithAuthenticator(m.Auth),
		client.WithWebFactory(func(steamID id.ID, logger log.Logger, baseDoer aoni.HTTPDoer) session.WebSessionProvider {
			return m.Web
		}),
		client.WithCommunityFactory(
			func(httpClient *http.Client, sess community.SessionProvider, logger log.Logger) community.Requester {
				return m.Comm
			},
		),
	}

	c, err := client.New(client.Config{}, opts...)
	require.NoError(t, err)

	m.Sock.On("IsConnected").Return(false).Maybe()
	m.Sock.On("Close").Return(nil).Maybe()
	m.Sock.On("UpdateLogger", mock.Anything).Return().Maybe()
	m.Sock.On("UpdateServers", mock.Anything).Return().Maybe()
	m.Web.On("Verify", mock.Anything).Return(true, nil).Maybe()
	m.Web.On("HTTP").Return(&http.Client{}).Maybe()
	m.Comm.On("GetOrRegisterAPIKey", mock.Anything, mock.Anything).Return("key_123", nil).Maybe()

	return c, m
}

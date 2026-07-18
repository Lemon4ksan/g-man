// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package steam_test

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"testing"

	"github.com/lemon4ksan/aoni"
	"github.com/lemon4ksan/miyako/log"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"

	"github.com/lemon4ksan/g-man/pkg/steam"
	"github.com/lemon4ksan/g-man/pkg/steam/auth"
	"github.com/lemon4ksan/g-man/pkg/steam/client/session"
	"github.com/lemon4ksan/g-man/pkg/steam/community"
	"github.com/lemon4ksan/g-man/pkg/steam/id"
	"github.com/lemon4ksan/g-man/pkg/steam/protocol/enums"
	steammock "github.com/lemon4ksan/g-man/test/mock"
)

func TestGetModule_VariousClientsAndModules_ReturnsExpectedResult(t *testing.T) {
	t.Parallel()

	t.Run("client_is_nil", func(t *testing.T) {
		t.Parallel()

		res := steam.GetModule[*steammock.AuthModule](nil)
		assert.Nil(t, res)
	})

	t.Run("module_found", func(t *testing.T) {
		t.Parallel()

		mod := &steammock.AuthModule{}
		mod.On("Name").Return("auth")

		c, _ := steam.NewClient(steam.Config{DisableSocket: true}, steam.WithModule(mod))
		res := steam.GetModule[*steammock.AuthModule](c)
		assert.Equal(t, mod, res)
		c.Close()
	})

	t.Run("module_not_found", func(t *testing.T) {
		t.Parallel()

		mod := &steammock.Module{}
		mod.On("Name").Return("simple")

		c, _ := steam.NewClient(steam.Config{DisableSocket: true}, steam.WithModule(mod))
		res := steam.GetModule[*steammock.AuthModule](c)
		assert.Nil(t, res)
		c.Close()
	})
}

type readyClientMocks struct {
	sock          *steammock.Socket
	authenticator *steammock.Authenticator
	webMock       *steammock.WebSession
	commMock      *steammock.Community
	httpMock      *steammock.HTTPDoer
	opts          []steam.Option
	details       *auth.LogOnDetails
}

func setupReadyClientMocks(t *testing.T) *readyClientMocks {
	t.Helper()

	sock := new(steammock.Socket).OnDefault()
	authenticator := new(steammock.Authenticator)
	webMock := new(steammock.WebSession)
	commMock := new(steammock.Community)
	httpMock := new(steammock.HTTPDoer)

	opts := []steam.Option{
		steam.WithSocket(sock),
		steam.WithAuthenticator(authenticator),
		steam.WithREST(aoni.NewClient(httpMock)),
		steam.WithWebFactory(func(steamID id.ID, logger log.Logger, baseDoer aoni.HTTPDoer) session.WebSessionProvider {
			return webMock
		}),
		steam.WithCommunityFactory(
			func(httpClient *http.Client, sess community.SessionProvider, logger log.Logger) community.Requester {
				return commMock
			},
		),
	}

	details := &auth.LogOnDetails{AccountName: "acc", SteamID: 123}

	return &readyClientMocks{
		sock:          sock,
		authenticator: authenticator,
		webMock:       webMock,
		commMock:      commMock,
		httpMock:      httpMock,
		opts:          opts,
		details:       details,
	}
}

func TestNewReadyClient_VariousScenarios_HandlesExpectedly(t *testing.T) {
	t.Parallel()

	t.Run("success", func(t *testing.T) {
		t.Parallel()

		m := setupReadyClientMocks(t)

		m.authenticator.On("LogOn", mock.Anything, m.details, mock.Anything).Return(nil).Once()
		m.webMock.On("Verify", mock.Anything).Return(true, nil)
		m.webMock.On("HTTP").Return(&http.Client{}).Maybe()
		m.commMock.On("GetOrRegisterAPIKey", mock.Anything, mock.Anything).Return("key_123", nil).Once()

		m.httpMock.On("Do", mock.MatchedBy(func(r *http.Request) bool {
			return r.URL.Path == "/ISteamDirectory/GetCMListForConnect/v1" ||
				r.URL.Path == "/ISteamDirectory/GetCMListForConnect/v1/" ||
				r.URL.Path == "/ISteamDirectory/GetCMList/v1" ||
				r.URL.Path == "/ISteamDirectory/GetCMList/v1/"
		})).Return(&http.Response{
			StatusCode: 200,
			Body: io.NopCloser(
				bytes.NewBufferString(
					`{"response":{"serverlist":[{"endpoint": "cm1.steampowered.com:27017"}],"success":true}}`,
				),
			),
		}, nil).Once()

		m.sock.On("SendProto", mock.Anything, enums.EMsg_ClientChangeStatus, mock.Anything, mock.Anything).
			Return(nil).
			Once()

		c, err := steam.NewReadyClient(t.Context(), steam.Config{}, m.details, m.opts...)
		assert.NoError(t, err)
		assert.NotNil(t, c)

		err = c.Close()
		assert.NoError(t, err)
	})

	t.Run("directory_failure", func(t *testing.T) {
		t.Parallel()

		m := setupReadyClientMocks(t)

		m.httpMock.On("Do", mock.Anything).Return(nil, errors.New("http err")).Once()

		c, err := steam.NewReadyClient(t.Context(), steam.Config{}, m.details, m.opts...)
		assert.ErrorContains(t, err, "http err")
		assert.Nil(t, c)
	})

	t.Run("login_failure", func(t *testing.T) {
		t.Parallel()

		m := setupReadyClientMocks(t)

		m.authenticator.On("LogOn", mock.Anything, m.details, mock.Anything).Return(errors.New("login rejected")).Once()

		m.httpMock.On("Do", mock.MatchedBy(func(r *http.Request) bool {
			return r.URL.Path == "/ISteamDirectory/GetCMListForConnect/v1" ||
				r.URL.Path == "/ISteamDirectory/GetCMListForConnect/v1/" ||
				r.URL.Path == "/ISteamDirectory/GetCMList/v1" ||
				r.URL.Path == "/ISteamDirectory/GetCMList/v1/"
		})).Return(&http.Response{
			StatusCode: 200,
			Body: io.NopCloser(
				bytes.NewBufferString(
					`{"response":{"serverlist":[{"endpoint": "cm1.steampowered.com:27017"}],"success":true}}`,
				),
			),
		}, nil).Once()

		c, err := steam.NewReadyClient(t.Context(), steam.Config{}, m.details, m.opts...)
		assert.ErrorContains(t, err, "login rejected")
		assert.Nil(t, c)
	})
}

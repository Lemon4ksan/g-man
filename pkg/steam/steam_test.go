// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package steam_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"testing"

	"github.com/lemon4ksan/aoni"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"

	"github.com/lemon4ksan/g-man/pkg/log"
	"github.com/lemon4ksan/g-man/pkg/steam"
	"github.com/lemon4ksan/g-man/pkg/steam/auth"
	"github.com/lemon4ksan/g-man/pkg/steam/client/session"
	"github.com/lemon4ksan/g-man/pkg/steam/id"
	"github.com/lemon4ksan/g-man/pkg/steam/protocol/enums"
	steammock "github.com/lemon4ksan/g-man/test/mock"
)

func TestGetModule(t *testing.T) {
	t.Run("c is nil", func(t *testing.T) {
		res := steam.GetModule[*steammock.AuthModule](nil)
		assert.Nil(t, res)
	})

	t.Run("module found", func(t *testing.T) {
		mod := &steammock.AuthModule{}
		mod.On("Name").Return("auth")

		c, _ := steam.NewClient(steam.Config{DisableSocket: true}, steam.WithModule(mod))
		res := steam.GetModule[*steammock.AuthModule](c)
		assert.Equal(t, mod, res)
		c.Close()
	})

	t.Run("module not found", func(t *testing.T) {
		mod := &steammock.Module{}
		mod.On("Name").Return("simple")

		c, _ := steam.NewClient(steam.Config{DisableSocket: true}, steam.WithModule(mod))
		res := steam.GetModule[*steammock.AuthModule](c)
		assert.Nil(t, res)
		c.Close()
	})
}

func TestNewReady_Success(t *testing.T) {
	ctx := context.Background()

	// Используем плавную инициализацию сокета по умолчанию
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
			func(httpClient *http.Client, sessionID func(string) string, logger log.Logger) session.CommunityProvider {
				return commMock
			},
		),
	}

	details := &auth.LogOnDetails{AccountName: "acc", SteamID: 123}

	authenticator.On("LogOn", mock.Anything, details, mock.Anything).Return(nil).Once()
	webMock.On("Verify", mock.Anything).Return(true, nil)
	webMock.On("HTTP").Return(&http.Client{}).Maybe()
	commMock.On("GetOrRegisterAPIKey", mock.Anything, mock.Anything).Return("key_123", nil).Once()

	httpMock.On("Do", mock.MatchedBy(func(r *http.Request) bool {
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

	sock.On("SendProto", mock.Anything, enums.EMsg_ClientChangeStatus, mock.Anything, mock.Anything).
		Return(nil).
		Once()

	c, err := steam.NewReadyClient(ctx, steam.Config{}, details, opts...)
	assert.NoError(t, err)
	assert.NotNil(t, c)

	err = c.Close()
	assert.NoError(t, err)
}

func TestNewReady_DirectoryFailure(t *testing.T) {
	ctx := context.Background()

	sock := new(steammock.Socket).OnDefault()
	http := new(steammock.HTTPDoer)

	opts := []steam.Option{
		steam.WithSocket(sock),
		steam.WithREST(aoni.NewClient(http)),
	}

	details := &auth.LogOnDetails{AccountName: "acc", SteamID: 123}

	http.On("Do", mock.Anything).Return(nil, errors.New("http err")).Once()

	c, err := steam.NewReadyClient(ctx, steam.Config{}, details, opts...)
	assert.ErrorContains(t, err, "http err")
	assert.Nil(t, c)
}

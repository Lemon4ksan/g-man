// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package steam

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"google.golang.org/protobuf/proto"

	pb "github.com/lemon4ksan/g-man/pkg/protobuf/steam"
	"github.com/lemon4ksan/g-man/pkg/steam/auth"
	"github.com/lemon4ksan/g-man/pkg/steam/module"
	"github.com/lemon4ksan/g-man/pkg/steam/socket"
)

func TestSessionManager_LogOn(t *testing.T) {
	c, m := setupTestClient(t)
	ctx := context.Background()

	server := socket.CMServer{Endpoint: "cm1.steam.com", Type: "tcp"}
	details := &auth.LogOnDetails{SteamID: 12345}

	m.auth.On("LogOn", mock.Anything, details, server).Return(nil).Once()
	m.web.On("Verify", mock.Anything).Return(true, nil)
	m.comm.On("GetOrRegisterAPIKey", mock.Anything, "g-man-bot.dev").Return("key_123", nil)

	err := c.session.LogOn(ctx, server, details)
	assert.NoError(t, err)

	assert.Eventually(t, func() bool {
		return c.session.unified.APIKey() == "key_123"
	}, time.Second, 10*time.Millisecond)
}

func TestSessionManager_Refresh(t *testing.T) {
	c, m := setupTestClient(t)
	ctx := context.Background()

	m.web.On("Verify", mock.Anything).Return(false, nil).Once()

	msess := new(mockSession)
	msess.On("RefreshToken").Return("my_refresh_token")
	msess.On("SteamID").Return(uint64(12345))
	msess.On("SetAccessToken", "new_at").Return()
	msess.On("AccessToken").Return("new_at")

	m.sock.On("Session").Return(msess)

	tokenPb, _ := proto.Marshal(&pb.CAuthentication_AccessToken_GenerateForApp_Response{
		AccessToken: proto.String("new_at"),
	})
	m.http.On("Do", mock.MatchedBy(func(r *http.Request) bool {
		return r.URL.Path == "/IAuthenticationService/GenerateAccessTokenForApp/v1"
	})).Return(&http.Response{
		StatusCode: 200,
		Header:     http.Header{"x-eresult": {"1"}},
		Body:       io.NopCloser(bytes.NewBuffer(tokenPb)),
	}, nil).Once()

	m.web.On("Authenticate",
		mock.Anything,
		mock.AnythingOfType("steam.EAuthTokenPlatformType"),
		"my_refresh_token",
		"new_at",
	).Return(nil).Once()

	err := c.session.Refresh(ctx)

	assert.NoError(t, err)
	m.http.AssertExpectations(t)
	m.web.AssertExpectations(t)
}

func TestSessionManager_LogOn_Errors(t *testing.T) {
	c, m := setupTestClient(t)
	ctx := context.Background()
	server := socket.CMServer{}
	details := &auth.LogOnDetails{SteamID: 1}

	t.Run("Auth Fails", func(t *testing.T) {
		m.auth.On("LogOn", mock.Anything, details, server).Return(errors.New("auth nope")).Once()
		err := c.session.LogOn(ctx, server, details)
		assert.ErrorContains(t, err, "auth nope")
	})

	t.Run("Refresh Fails", func(t *testing.T) {
		m.auth.On("LogOn", mock.Anything, details, server).Return(nil).Once()
		c.session.closed.Store(true) // trigger module.ErrClosed in Refresh
		err := c.session.LogOn(ctx, server, details)
		assert.ErrorContains(t, err, module.ErrClosed.Error())
		c.session.closed.Store(false)
	})

	t.Run("API Key Fails", func(t *testing.T) {
		m.auth.On("LogOn", mock.Anything, details, server).Return(nil).Once()
		m.web.On("Verify", mock.Anything).Return(true, nil).Once()
		m.comm.On("GetOrRegisterAPIKey", mock.Anything, mock.Anything).Return("", errors.New("no api key")).Once()

		err := c.session.LogOn(ctx, server, details)
		assert.ErrorContains(t, err, "no api key")
	})
}

func TestSessionManager_Refresh_Errors(t *testing.T) {
	c, m := setupTestClient(t)
	ctx := context.Background()

	t.Run("Already Closed", func(t *testing.T) {
		c.session.closed.Store(true)
		assert.ErrorIs(t, c.session.Refresh(ctx), module.ErrClosed)
		c.session.closed.Store(false)
	})

	t.Run("Web Session Valid", func(t *testing.T) {
		m.web.On("Verify", ctx).Return(true, nil).Once()
		assert.NoError(t, c.session.Refresh(ctx))
	})

	t.Run("No Socket Session", func(t *testing.T) {
		m.web.On("Verify", ctx).Return(false, nil).Once()
		m.sock.On("Session").Return(nil).Once()

		err := c.session.Refresh(ctx)
		assert.ErrorContains(t, err, "socket is not connected")
	})

	t.Run("Generate Token Fails", func(t *testing.T) {
		m.web.On("Verify", ctx).Return(false, nil).Once()

		msess := new(mockSession)
		msess.On("RefreshToken").Return("rt")
		msess.On("SteamID").Return(uint64(1))
		m.sock.On("Session").Return(msess).Once()

		m.http.On("Do", mock.Anything).Return(nil, errors.New("http err")).Once()

		err := c.session.Refresh(ctx)
		assert.ErrorContains(t, err, "failed to generate access token")
	})

	t.Run("Web Auth Fails", func(t *testing.T) {
		m.web.On("Verify", ctx).Return(false, nil).Once()

		msess := new(mockSession)
		msess.On("RefreshToken").Return("rt")
		msess.On("SteamID").Return(uint64(1))
		msess.On("AccessToken").Return("old")
		msess.On("SetAccessToken", "new_at").Return()
		m.sock.On("Session").Return(msess).Once()

		tokenPb, _ := proto.Marshal(
			&pb.CAuthentication_AccessToken_GenerateForApp_Response{AccessToken: proto.String("new_at")},
		)
		m.http.On("Do", mock.Anything).Return(&http.Response{
			StatusCode: 200, Header: http.Header{"x-eresult": {"1"}},
			Body: io.NopCloser(bytes.NewBuffer(tokenPb)),
		}, nil).Once()

		m.web.On("Authenticate", ctx, mock.Anything, "rt", "old").Return(errors.New("web nope")).Once()

		err := c.session.Refresh(ctx)
		assert.ErrorContains(t, err, "web auth failed")
	})
}

func TestSessionManager_LoopAndClose(t *testing.T) {
	c, _ := setupTestClient(t)

	// Test Close and Disconnect
	err := c.session.Close()
	assert.NoError(t, err)
	assert.True(t, c.session.closed.Load())

	// Test Loop context cancellation
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // instantly cancel

	err = c.session.StartRefreshLoop(ctx)
	assert.NoError(t, err) // Gracefully shuts down, calls Disconnect which returns nil
}

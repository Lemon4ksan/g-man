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

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"

	"github.com/lemon4ksan/g-man/pkg/steam/api"
	"github.com/lemon4ksan/g-man/pkg/steam/service"
	tr "github.com/lemon4ksan/g-man/pkg/steam/transport"
)

func TestServiceRouter_Do_TransportSelection(t *testing.T) {
	ctx := context.Background()

	sock := new(mockSocket)
	httpDoer := new(mockHTTPDoer)

	unified := service.New(tr.NewHTTPTransport(httpDoer, service.WebAPIBase))
	socketAPI := service.New(tr.NewSocketTransport(sock))

	refresher := new(mockSessionRefresher)
	refresher.On("Clients").Return(unified, socketAPI)

	router := NewServiceRouter(refresher, sock)

	t.Run("Fallback to HTTP when socket disconnected", func(t *testing.T) {
		sock.On("IsConnected").Return(false).Once()
		httpDoer.On("Do", mock.Anything).Return(&http.Response{
			StatusCode: 200,
			Body:       io.NopCloser(bytes.NewBufferString(`{}`)),
		}, nil).Once()

		req := tr.NewRequest(&mockSocketTarget{path: "p"}, nil)
		_, err := router.Do(ctx, req)

		assert.NoError(t, err)
		httpDoer.AssertExpectations(t)
		sock.AssertExpectations(t)
	})

	t.Run("Silent Refresh on Session Expired", func(t *testing.T) {
		sock.On("IsConnected").Return(false).Once() // Fallback to HTTP
		sock.On("IsConnected").Return(true).Once()

		// 1st Attempt: Fails with Expired
		httpDoer.On("Do", mock.MatchedBy(func(r *http.Request) bool {
			return r.URL.Path == "/mock_path"
		})).Return(nil, api.ErrSessionExpired).Once()

		// Router catches error and performs Refresh
		refresher.On("Refresh", ctx).Return(nil).Once()

		// 2nd Attempt: Succeeds
		httpDoer.On("Do", mock.MatchedBy(func(r *http.Request) bool {
			return r.URL.Path == "/mock_path"
		})).Return(&http.Response{
			StatusCode: 200,
			Body:       io.NopCloser(bytes.NewBufferString(`{"ok":true}`)),
		}, nil).Once()

		target := &mockTarget{path: "mock_path"}
		_, err := router.Do(ctx, tr.NewRequest(target, nil))

		assert.NoError(t, err)
		httpDoer.AssertExpectations(t)
		refresher.AssertExpectations(t)
	})
}

func TestServiceRouter_Do_Errors(t *testing.T) {
	ctx := context.Background()

	sock := new(mockSocket)
	httpDoer := new(mockHTTPDoer)

	unified := service.New(tr.NewHTTPTransport(httpDoer, service.WebAPIBase))
	socketAPI := service.New(tr.NewSocketTransport(sock))

	refresher := new(mockSessionRefresher)
	refresher.On("Clients").Return(unified, socketAPI)

	router := NewServiceRouter(refresher, sock)
	target := &mockTarget{path: "mock_path"}

	t.Run("Perform Generic Error", func(t *testing.T) {
		sock.On("IsConnected").Return(false).Once()
		httpDoer.On("Do", mock.Anything).Return(nil, errors.New("network error")).Once()

		_, err := router.Do(ctx, tr.NewRequest(target, nil))
		assert.ErrorContains(t, err, "network error")
	})

	t.Run("Refresh Failure", func(t *testing.T) {
		sock.On("IsConnected").Return(false).Once()
		httpDoer.On("Do", mock.Anything).Return(nil, api.ErrSessionExpired).Once()

		refresher.On("Refresh", ctx).Return(errors.New("refresh failed")).Once()

		_, err := router.Do(ctx, tr.NewRequest(target, nil))
		assert.ErrorContains(t, err, "auto-refresh failed: refresh failed")
	})
}

// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package router

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"

	"github.com/lemon4ksan/g-man/pkg/steam/service"
	tr "github.com/lemon4ksan/g-man/pkg/steam/transport"
)

type testStateProvider struct {
	connected bool
}

func (sp *testStateProvider) IsConnected() bool {
	return sp.connected
}

type testSessionRefresher struct {
	mock.Mock
}

func (m *testSessionRefresher) Refresh(ctx context.Context) error {
	args := m.Called(ctx)
	return args.Error(0)
}

func (m *testSessionRefresher) Unified() *service.Client {
	args := m.Called()
	client, _ := args.Get(0).(*service.Client)
	return client
}

func (m *testSessionRefresher) Socket() *service.Client {
	args := m.Called()
	client, _ := args.Get(0).(*service.Client)
	return client
}

type testTransport struct {
	mock.Mock
}

func (t *testTransport) Do(ctx context.Context, req *tr.Request) (*tr.Response, error) {
	args := t.Called(ctx, req)
	resp, _ := args.Get(0).(*tr.Response)
	return resp, args.Error(1)
}

type testTarget struct {
	tr.Target
}

type testSocketTarget struct {
	tr.SocketTarget
}

// newMockedRouter is a helper to construct a Router with pre-configured mock components.
func newMockedRouter(
	t *testing.T,
	connected bool,
) (*ServiceRouter, *testSessionRefresher, *testStateProvider, *testTransport, *testTransport) {
	t.Helper()

	state := &testStateProvider{connected: connected}
	unifiedTrans := &testTransport{}
	socketTrans := &testTransport{}

	refresher := &testSessionRefresher{}

	router := New(refresher, state)

	return router, refresher, state, unifiedTrans, socketTrans
}

func TestNew_ValidInputs_InitializesCorrectly(t *testing.T) {
	t.Parallel()

	refresher := &testSessionRefresher{}
	state := &testStateProvider{}

	router := New(refresher, state)
	assert.NotNil(t, router)
	assert.NotNil(t, router.matcher)
	assert.Equal(t, refresher, router.refresher)
	assert.Equal(t, state, router.state)
}

func TestServiceRouter_SetRouteMatcher_CustomMatcher_AppliesCorrectly(t *testing.T) {
	t.Parallel()

	router := New(&testSessionRefresher{}, &testStateProvider{})

	router.SetRouteMatcher(nil)
	assert.NotNil(t, router.matcher)

	customCalled := false
	router.SetRouteMatcher(func(req *tr.Request) TransportType {
		customCalled = true
		return TransportSocket
	})

	req := tr.NewRequest(&testTarget{}, nil)
	assert.Equal(t, TransportSocket, router.matcher(req))
	assert.True(t, customCalled)
}

func TestServiceRouter_DefaultRouteMatcher_VariousStates_SelectsCorrectTransport(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		connected bool
		target    tr.Target
		want      TransportType
	}{
		{
			name:      "socket_target_connected",
			connected: true,
			target:    &testSocketTarget{},
			want:      TransportSocket,
		},
		{
			name:      "socket_target_disconnected",
			connected: false,
			target:    &testSocketTarget{},
			want:      TransportWebAPI,
		},
		{
			name:      "web_target_connected",
			connected: true,
			target:    &testTarget{},
			want:      TransportWebAPI,
		},
		{
			name:      "web_target_disconnected",
			connected: false,
			target:    &testTarget{},
			want:      TransportWebAPI,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			state := &testStateProvider{connected: tt.connected}
			router := New(&testSessionRefresher{}, state)
			req := tr.NewRequest(tt.target, nil)

			assert.Equal(t, tt.want, router.DefaultRouteMatcher(req))
		})
	}
}

func TestServiceRouter_Do_TransportSelection_VariousStates_RoutesCorrectly(t *testing.T) {
	t.Parallel()

	t.Run("fallback_to_http_on_disconnected", func(t *testing.T) {
		t.Parallel()

		router, refresher, _, unifiedTrans, _ := newMockedRouter(t, false)
		unified := service.New(unifiedTrans)
		refresher.On("Unified").Return(unified)

		req := tr.NewRequest(&testSocketTarget{}, nil)
		unifiedTrans.On("Do", mock.Anything, req).Return((*tr.Response)(nil), nil).Once()

		_, err := router.Do(t.Context(), req)
		assert.NoError(t, err)
		unifiedTrans.AssertExpectations(t)
	})

	t.Run("route_to_socket_on_connected", func(t *testing.T) {
		t.Parallel()

		router, refresher, _, _, socketTrans := newMockedRouter(t, true)
		socketAPI := service.New(socketTrans)
		refresher.On("Socket").Return(socketAPI)

		req := tr.NewRequest(&testSocketTarget{}, nil)
		socketTrans.On("Do", mock.Anything, req).Return((*tr.Response)(nil), nil).Once()

		_, err := router.Do(t.Context(), req)
		assert.NoError(t, err)
		socketTrans.AssertExpectations(t)
	})
}

func TestServiceRouter_Do_SilentRefresh_WithSessionExpiration_RefreshesAndRetries(t *testing.T) {
	t.Parallel()

	t.Run("success_on_second_attempt", func(t *testing.T) {
		t.Parallel()

		router, refresher, _, unifiedTrans, _ := newMockedRouter(t, false)
		unified := service.New(unifiedTrans)
		refresher.On("Unified").Return(unified)

		req := tr.NewRequest(&testTarget{}, nil)

		unifiedTrans.On("Do", mock.Anything, req).Return((*tr.Response)(nil), service.ErrSessionExpired).Once()
		refresher.On("Refresh", mock.Anything).Return(nil).Once()
		unifiedTrans.On("Do", mock.Anything, req).Return((*tr.Response)(nil), nil).Once()

		_, err := router.Do(t.Context(), req)
		assert.NoError(t, err)
		unifiedTrans.AssertExpectations(t)
		refresher.AssertExpectations(t)
	})

	t.Run("refresh_fails", func(t *testing.T) {
		t.Parallel()

		router, refresher, _, unifiedTrans, _ := newMockedRouter(t, false)
		unified := service.New(unifiedTrans)
		refresher.On("Unified").Return(unified)

		req := tr.NewRequest(&testTarget{}, nil)

		unifiedTrans.On("Do", mock.Anything, req).Return((*tr.Response)(nil), service.ErrSessionExpired).Once()
		refresher.On("Refresh", mock.Anything).Return(errors.New("refresh failed")).Once()

		_, err := router.Do(t.Context(), req)
		assert.ErrorContains(t, err, "router: auto-refresh failed: refresh failed")
		unifiedTrans.AssertExpectations(t)
		refresher.AssertExpectations(t)
	})
}

func TestServiceRouter_Do_TransportError_ReturnsError(t *testing.T) {
	t.Parallel()

	router, refresher, _, unifiedTrans, _ := newMockedRouter(t, false)
	unified := service.New(unifiedTrans)
	refresher.On("Unified").Return(unified)

	req := tr.NewRequest(&testTarget{}, nil)
	unifiedTrans.On("Do", mock.Anything, req).Return((*tr.Response)(nil), errors.New("network error")).Once()

	_, err := router.Do(t.Context(), req)
	assert.ErrorContains(t, err, "network error")
	unifiedTrans.AssertExpectations(t)
}

func TestServiceRouter_Do_NilClient_ReturnsNoActiveClientError(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		connected bool
		setupMock func(refresher *testSessionRefresher)
		target    tr.Target
	}{
		{
			name:      "socket_client_is_nil",
			connected: true,
			setupMock: func(r *testSessionRefresher) {
				r.On("Socket").Return((*service.Client)(nil)).Once()
			},
			target: &testSocketTarget{},
		},
		{
			name:      "web_api_client_is_nil",
			connected: false,
			setupMock: func(r *testSessionRefresher) {
				r.On("Unified").Return((*service.Client)(nil)).Once()
			},
			target: &testTarget{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			router, refresher, _, _, _ := newMockedRouter(t, tt.connected)
			if tt.setupMock != nil {
				tt.setupMock(refresher)
			}

			req := tr.NewRequest(tt.target, nil)

			_, err := router.Do(t.Context(), req)
			assert.ErrorIs(t, err, ErrNoActiveClient)
		})
	}
}

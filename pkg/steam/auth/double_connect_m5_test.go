// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package auth

import (
	"context"
	"testing"
	"time"

	"github.com/lemon4ksan/foundation/async/event"
	log "github.com/lemon4ksan/foundation/async/logkit"
	"github.com/stretchr/testify/mock"

	"github.com/lemon4ksan/g-man/pkg/steam/socket"
)

func (m *MockSocketProvider) IsConnected() bool {
	for _, call := range m.ExpectedCalls {
		if call.Method == "IsConnected" {
			return m.Called().Bool(0)
		}
	}

	return false
}

func (m *MockSocketProvider) IsConnecting() bool {
	for _, call := range m.ExpectedCalls {
		if call.Method == "IsConnecting" {
			return m.Called().Bool(0)
		}
	}

	return false
}

func (m *MockSocketProvider) WaitForConnection(ctx context.Context) error {
	for _, call := range m.ExpectedCalls {
		if call.Method == "WaitForConnection" {
			return m.Called(ctx).Error(0)
		}
	}

	return nil
}

func (m *MockSocketProvider) CurrentServer() (socket.CMServer, bool) {
	for _, call := range m.ExpectedCalls {
		if call.Method == "CurrentServer" {
			args := m.Called()
			return args.Get(0).(socket.CMServer), args.Bool(1)
		}
	}

	return socket.CMServer{}, false
}

func TestAuthenticator_LogOn_TransportAlreadyConnected_SkipsDial(t *testing.T) {
	t.Parallel()

	mockSock := NewMockSocket()
	sess := &mockSession{}
	mockSock.On("Session").Return(sess).Maybe()
	mockSock.On("IsConnected").Return(true)
	mockSock.On("SendProto", mock.Anything, mock.Anything, mock.Anything).Return(nil).Maybe()

	bus := event.New()
	webAPI := new(MockWebAuthenticator)
	store := &nopStore{}

	a := NewAuthenticator(mockSock, webAPI, bus, WithStorage(store), WithLogger(log.Discard))

	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()

	details := &LogOnDetails{
		AccountName:  "testuser",
		RefreshToken: "refresh_token_test",
	}

	server := socket.CMServer{Type: "websockets", Endpoint: "127.0.0.1:443"}

	_ = a.LogOn(ctx, details, server)

	// Invariant: Connect must NOT be called if socket transport is already connected
	mockSock.AssertNotCalled(t, "Connect", mock.Anything)
}

func TestAuthenticator_LogOnAnonymous_TransportAlreadyConnected_SkipsDial(t *testing.T) {
	t.Parallel()

	mockSock := NewMockSocket()
	sess := &mockSession{}
	mockSock.On("Session").Return(sess).Maybe()
	mockSock.On("IsConnected").Return(true)
	mockSock.On("SendProto", mock.Anything, mock.Anything, mock.Anything).Return(nil).Maybe()

	bus := event.New()
	webAPI := new(MockWebAuthenticator)
	store := &nopStore{}

	a := NewAuthenticator(mockSock, webAPI, bus, WithStorage(store), WithLogger(log.Discard))

	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()

	server := socket.CMServer{Type: "websockets", Endpoint: "127.0.0.1:443"}

	_ = a.LogOnAnonymous(ctx, server)

	// Invariant: Connect must NOT be called if socket transport is already connected
	mockSock.AssertNotCalled(t, "Connect", mock.Anything)
}

func TestAuthenticator_LogOn_AwaitsConnectingTransport(t *testing.T) {
	t.Parallel()

	mockSock := NewMockSocket()
	sess := &mockSession{}
	mockSock.On("Session").Return(sess).Maybe()
	mockSock.On("IsConnecting").Return(true).Once()
	mockSock.On("WaitForConnection", mock.Anything).Return(nil).Once()
	mockSock.On("IsConnected").Return(true)
	mockSock.On("SendProto", mock.Anything, mock.Anything, mock.Anything).Return(nil).Maybe()

	bus := event.New()
	webAPI := new(MockWebAuthenticator)
	store := &nopStore{}

	a := NewAuthenticator(mockSock, webAPI, bus, WithStorage(store), WithLogger(log.Discard))

	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()

	details := &LogOnDetails{
		AccountName:  "testuser",
		RefreshToken: "refresh_token_test",
	}

	server := socket.CMServer{Type: "websockets", Endpoint: "127.0.0.1:443"}

	_ = a.LogOn(ctx, details, server)

	mockSock.AssertCalled(t, "WaitForConnection", mock.Anything)
	mockSock.AssertNotCalled(t, "Connect", mock.Anything)
}

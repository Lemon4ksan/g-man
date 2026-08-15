// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package auth

import (
	"context"
	"testing"

	"github.com/lemon4ksan/g-man/pkg/steam/socket"
	"github.com/lemon4ksan/g-man/pkg/storage/memory"
	"github.com/stretchr/testify/require"
)

type mockLogOnRunner struct {
	lastDetails *LogOnDetails
	lastServer  socket.CMServer
	errToReturn error
}

func (m *mockLogOnRunner) LogOn(ctx context.Context, details *LogOnDetails, server socket.CMServer) error {
	m.lastDetails = details
	m.lastServer = server
	return m.errToReturn
}

func TestFlow_Execute_WithCredentialsAndSharedSecret(t *testing.T) {
	ctx := context.Background()
	mockRunner := &mockLogOnRunner{}

	// Valid 20-byte base64 secret
	secret := "AAAAAAAAAAAAAAAAAAAAAAAAAAA="

	flow := NewFlow(
		mockRunner,
		WithCredentials("test_user", "test_pass"),
		WithSharedSecret(secret),
		WithServerFinder(func(ctx context.Context) (socket.CMServer, error) {
			return socket.CMServer{Endpoint: "127.0.0.1:27017"}, nil
		}),
	)

	details, err := flow.Execute(ctx)
	require.NoError(t, err)
	require.NotNil(t, details)
	require.Equal(t, "test_user", details.AccountName)
	require.Equal(t, "test_pass", details.Password)
	require.Len(t, details.AuthCode, 5, "2FA TOTP code should be exactly 5 characters")
	require.Equal(t, details.AuthCode, details.TwoFactorCode)
	require.Equal(t, "127.0.0.1:27017", mockRunner.lastServer.Endpoint)
}

func TestFlow_Resume_WithRefreshToken(t *testing.T) {
	ctx := context.Background()
	mockRunner := &mockLogOnRunner{}

	flow := NewFlow(
		mockRunner,
		WithServerFinder(func(ctx context.Context) (socket.CMServer, error) {
			return socket.CMServer{Endpoint: "cm.steampowered.com:27017"}, nil
		}),
	)

	details, err := flow.Resume(ctx, "ey.mock_jwt_token.sig")
	require.NoError(t, err)
	require.NotNil(t, details)
	require.Equal(t, "ey.mock_jwt_token.sig", details.RefreshToken)
}

func TestFlow_Storage_PersistsAndLoads(t *testing.T) {
	ctx := context.Background()
	memStore := memory.New()
	kv := memStore.KV("auth")

	// Pre-populate saved token
	_ = kv.Set(ctx, "refresh_token:saved_user", []byte("saved_jwt_token_123"))

	mockRunner := &mockLogOnRunner{}

	flow := NewFlow(
		mockRunner,
		WithCredentials("saved_user", "password"),
		WithFlowStorage(kv),
		WithServerFinder(func(ctx context.Context) (socket.CMServer, error) {
			return socket.CMServer{Endpoint: "127.0.0.1:27017"}, nil
		}),
	)

	details, err := flow.Execute(ctx)
	require.NoError(t, err)
	require.Equal(t, "saved_jwt_token_123", details.RefreshToken)
}

func TestFlow_MissingServerFinder_ReturnsError(t *testing.T) {
	ctx := context.Background()
	mockRunner := &mockLogOnRunner{}

	flow := NewFlow(mockRunner, WithCredentials("user", "pass"))
	_, err := flow.Execute(ctx)
	require.ErrorIs(t, err, ErrServerFinderRequired)
}

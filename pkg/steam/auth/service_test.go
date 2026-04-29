// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package auth

import (
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"

	pb "github.com/lemon4ksan/g-man/pkg/protobuf/steam"
	"github.com/lemon4ksan/g-man/test/requester"
)

const (
	TestTimestamp = 1234567890
	TestSteamID   = 123
)

func setupAuthService(t *testing.T, conf *DeviceConfig) (*AuthenticationService, *requester.Mock) {
	t.Helper()

	mock := requester.New()
	svc := NewAuthenticationService(mock, conf)

	return svc, mock
}

func mockRSAResponse(t *testing.T, mock *requester.Mock) *rsa.PrivateKey {
	t.Helper()

	privKey, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err, "failed to generate RSA key")

	modHex := fmt.Sprintf("%x", privKey.N)
	expHex := fmt.Sprintf("%x", privKey.E)

	mock.SetProtoResponse(
		"Authentication",
		"GetPasswordRSAPublicKey",
		&pb.CAuthentication_GetPasswordRSAPublicKey_Response{
			PublickeyMod: proto.String(modHex),
			PublickeyExp: proto.String(expHex),
			Timestamp:    proto.Uint64(TestTimestamp),
		},
	)

	return privKey
}

func TestNewAuthenticationService(t *testing.T) {
	t.Run("Default Config", func(t *testing.T) {
		svc, _ := setupAuthService(t, nil)
		assert.Equal(t, DefaultDeviceConfig(), svc.DeviceConf())
	})

	t.Run("Custom Config", func(t *testing.T) {
		custom := &DeviceConfig{DeviceFriendlyName: "G-man Bot"}
		svc, _ := setupAuthService(t, custom)
		assert.Equal(t, "G-man Bot", svc.DeviceConf().DeviceFriendlyName)
	})
}

func TestAuthenticationService_EncryptPassword(t *testing.T) {
	svc, mock := setupAuthService(t, nil)
	privKey := mockRSAResponse(t, mock)

	password := "secret_123"

	encBase64, ts, err := svc.EncryptPassword(t.Context(), "user", password)
	require.NoError(t, err)
	assert.Equal(t, uint64(TestTimestamp), ts)

	cipherText, err := base64.StdEncoding.DecodeString(encBase64)
	require.NoError(t, err)

	plainText, err := rsa.DecryptPKCS1v15(rand.Reader, privKey, cipherText)
	require.NoError(t, err, "failed to decrypt password")
	assert.Equal(t, password, string(plainText))
}

func TestAuthenticationService_EncryptPassword_Errors(t *testing.T) {
	svc, mock := setupAuthService(t, nil)
	method := "Authentication.GetPasswordRSAPublicKey"

	tests := []struct {
		name    string
		setup   func()
		wantErr string
	}{
		{
			name: "API Down",
			setup: func() {
				mock.ResponseErrs[method] = errors.New("api down")
			},
			wantErr: "fetch rsa key: api down",
		},
		{
			name: "Empty Parameters",
			setup: func() {
				mock.ResponseErrs[method] = nil
				mock.SetProtoResponse(
					"Authentication",
					"GetPasswordRSAPublicKey",
					&pb.CAuthentication_GetPasswordRSAPublicKey_Response{},
				)
			},
			wantErr: "steam returned empty rsa parameters",
		},
		{
			name: "Invalid Hex",
			setup: func() {
				mock.SetProtoResponse(
					"Authentication",
					"GetPasswordRSAPublicKey",
					&pb.CAuthentication_GetPasswordRSAPublicKey_Response{
						PublickeyMod: proto.String("NOT_HEX"),
						PublickeyExp: proto.String("010001"),
					},
				)
			},
			wantErr: "invalid rsa modulus hex string",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.setup()

			_, _, err := svc.EncryptPassword(t.Context(), "user", "pwd")
			assert.EqualError(t, err, tt.wantErr)
		})
	}
}

func TestAuthenticationService_BeginAuthSessionViaCredentials(t *testing.T) {
	svc, mock := setupAuthService(t, nil)
	mockRSAResponse(t, mock)

	mock.SetProtoResponse(
		"Authentication",
		"BeginAuthSessionViaCredentials",
		&pb.CAuthentication_BeginAuthSessionViaCredentials_Response{
			ClientId: proto.Uint64(999),
		},
	)

	resp, err := svc.BeginAuthSessionViaCredentials(t.Context(), "user", "pass", "GUARD_CODE")
	require.NoError(t, err)
	assert.Equal(t, uint64(999), resp.GetClientId())

	sent := &pb.CAuthentication_BeginAuthSessionViaCredentials_Request{}
	mock.GetLastCall(sent)

	assert.Equal(t, "user", sent.GetAccountName())
	assert.Equal(t, "GUARD_CODE", sent.GetGuardData())
	assert.NotEmpty(t, sent.GetEncryptedPassword())
}

func TestAuthenticationService_PollAuthSessionStatus(t *testing.T) {
	svc, mock := setupAuthService(t, nil)

	mock.SetProtoResponse("Authentication", "PollAuthSessionStatus", &pb.CAuthentication_PollAuthSessionStatus_Response{
		RefreshToken: proto.String("new_token"),
	})

	resp, err := svc.PollAuthSessionStatus(t.Context(), 123, []byte{1, 2})
	require.NoError(t, err)
	assert.Equal(t, "new_token", resp.GetRefreshToken())

	sent := &pb.CAuthentication_PollAuthSessionStatus_Request{}
	mock.GetLastCall(sent)

	assert.Equal(t, uint64(123), sent.GetClientId())
}

func TestAuthenticationService_UpdateAuthSessionWithSteamGuardCode(t *testing.T) {
	svc, mock := setupAuthService(t, nil)

	err := svc.UpdateAuthSessionWithSteamGuardCode(
		t.Context(),
		111,
		TestSteamID,
		"ABCDE",
		pb.EAuthSessionGuardType_k_EAuthSessionGuardType_DeviceCode,
	)
	require.NoError(t, err)

	sent := &pb.CAuthentication_UpdateAuthSessionWithSteamGuardCode_Request{}
	mock.GetLastCall(sent)

	assert.Equal(t, "ABCDE", sent.GetCode())
	assert.Equal(t, uint64(TestSteamID), sent.GetSteamid())
}

func TestAuthenticationService_GenerateAccessTokenForApp(t *testing.T) {
	svc, mock := setupAuthService(t, nil)

	mock.SetProtoResponse(
		"Authentication",
		"GenerateAccessTokenForApp",
		&pb.CAuthentication_AccessToken_GenerateForApp_Response{
			AccessToken: proto.String("access_token"),
		},
	)

	resp, err := svc.GenerateAccessTokenForApp(t.Context(), "refresh_token", TestSteamID)
	require.NoError(t, err)
	assert.Equal(t, "access_token", resp.GetAccessToken())
}

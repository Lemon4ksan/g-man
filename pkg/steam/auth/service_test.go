// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package auth_test

import (
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"

	pb "github.com/lemon4ksan/g-man/pkg/protobuf/steam"
	"github.com/lemon4ksan/g-man/pkg/steam/auth"
	"github.com/lemon4ksan/g-man/test/mock"
)

const (
	TestTimestamp = 1234567890
	TestSteamID   = 123
)

func setupAuthService(t *testing.T, conf *auth.DeviceConfig) (*auth.AuthenticationService, *mock.ServiceMock) {
	t.Helper()

	mock := mock.NewServiceMock()
	svc := auth.NewAuthenticationService(mock, conf)

	return svc, mock
}

func mockRSAResponse(t *testing.T, mock *mock.ServiceMock) *rsa.PrivateKey {
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
	t.Parallel()

	t.Run("default_config", func(t *testing.T) {
		t.Parallel()

		svc, _ := setupAuthService(t, nil)
		assert.Equal(t, auth.DefaultDeviceConfig(), svc.DeviceConf())
	})

	t.Run("custom_config", func(t *testing.T) {
		t.Parallel()

		custom := &auth.DeviceConfig{DeviceFriendlyName: "G-man Bot"}
		svc, _ := setupAuthService(t, custom)
		assert.Equal(t, "G-man Bot", svc.DeviceConf().DeviceFriendlyName)
	})
}

func TestAuthenticationService_EncryptPassword(t *testing.T) {
	t.Parallel()

	t.Run("success", func(t *testing.T) {
		t.Parallel()

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
	})

	t.Run("error_oversized", func(t *testing.T) {
		t.Parallel()

		svc, mock := setupAuthService(t, nil)
		mockRSAResponse(t, mock)

		longPassword := strings.Repeat("a", 1000)
		_, _, err := svc.EncryptPassword(t.Context(), "user", longPassword)
		assert.ErrorContains(t, err, "encrypt password payload")
	})

	t.Run("errors_table", func(t *testing.T) {
		t.Parallel()

		method := "Authentication.GetPasswordRSAPublicKey"

		tests := []struct {
			name    string
			setup   func(mock *mock.ServiceMock)
			wantErr string
		}{
			{
				name: "api_down",
				setup: func(mock *mock.ServiceMock) {
					mock.ResponseErrs[method] = errors.New("api down")
				},
				wantErr: "fetch rsa key: api down",
			},
			{
				name: "empty_parameters",
				setup: func(mock *mock.ServiceMock) {
					mock.SetProtoResponse(
						"Authentication",
						"GetPasswordRSAPublicKey",
						&pb.CAuthentication_GetPasswordRSAPublicKey_Response{},
					)
				},
				wantErr: "steam returned empty rsa parameters",
			},
			{
				name: "invalid_modulus_hex",
				setup: func(mock *mock.ServiceMock) {
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
			{
				name: "invalid_exponent_hex",
				setup: func(mock *mock.ServiceMock) {
					mock.SetProtoResponse(
						"Authentication",
						"GetPasswordRSAPublicKey",
						&pb.CAuthentication_GetPasswordRSAPublicKey_Response{
							PublickeyMod: proto.String("010203"),
							PublickeyExp: proto.String("NOT_HEX"),
						},
					)
				},
				wantErr: "invalid rsa exponent hex string",
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				t.Parallel()

				svc, mock := setupAuthService(t, nil)
				tt.setup(mock)

				_, _, err := svc.EncryptPassword(t.Context(), "user", "pwd")
				assert.EqualError(t, err, tt.wantErr)
			})
		}
	})
}

func TestAuthenticationService_BeginAuthSessionViaCredentials(t *testing.T) {
	t.Parallel()

	t.Run("success", func(t *testing.T) {
		t.Parallel()

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
	})

	t.Run("error_encrypt", func(t *testing.T) {
		t.Parallel()

		svc, mock := setupAuthService(t, nil)
		mock.ResponseErrs["Authentication.GetPasswordRSAPublicKey"] = errors.New("rsa fail")

		_, err := svc.BeginAuthSessionViaCredentials(t.Context(), "user", "pass", "GUARD_CODE")
		assert.ErrorContains(t, err, "fetch rsa key: rsa fail")
	})

	t.Run("error_api", func(t *testing.T) {
		t.Parallel()

		svc, mock := setupAuthService(t, nil)
		mockRSAResponse(t, mock)
		mock.ResponseErrs["Authentication.BeginAuthSessionViaCredentials"] = errors.New("api fail")

		_, err := svc.BeginAuthSessionViaCredentials(t.Context(), "user", "pass", "GUARD_CODE")
		assert.ErrorContains(t, err, "api fail")
	})
}

func TestAuthenticationService_PollAuthSessionStatus(t *testing.T) {
	t.Parallel()

	t.Run("success", func(t *testing.T) {
		t.Parallel()

		svc, mock := setupAuthService(t, nil)

		mock.SetProtoResponse(
			"Authentication",
			"PollAuthSessionStatus",
			&pb.CAuthentication_PollAuthSessionStatus_Response{
				RefreshToken: proto.String("new_token"),
			},
		)

		resp, err := svc.PollAuthSessionStatus(t.Context(), 123, []byte{1, 2})
		require.NoError(t, err)
		assert.Equal(t, "new_token", resp.GetRefreshToken())

		sent := &pb.CAuthentication_PollAuthSessionStatus_Request{}
		mock.GetLastCall(sent)

		assert.Equal(t, uint64(123), sent.GetClientId())
	})

	t.Run("error_api", func(t *testing.T) {
		t.Parallel()

		svc, mock := setupAuthService(t, nil)
		mock.ResponseErrs["Authentication.PollAuthSessionStatus"] = errors.New("poll fail")

		_, err := svc.PollAuthSessionStatus(t.Context(), 123, []byte{1, 2})
		assert.ErrorContains(t, err, "poll fail")
	})
}

func TestAuthenticationService_UpdateAuthSessionWithSteamGuardCode(t *testing.T) {
	t.Parallel()

	t.Run("success", func(t *testing.T) {
		t.Parallel()

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
	})

	t.Run("error_api", func(t *testing.T) {
		t.Parallel()

		svc, mock := setupAuthService(t, nil)
		mock.ResponseErrs["Authentication.UpdateAuthSessionWithSteamGuardCode"] = errors.New("update fail")

		err := svc.UpdateAuthSessionWithSteamGuardCode(
			t.Context(),
			111,
			TestSteamID,
			"ABCDE",
			pb.EAuthSessionGuardType_k_EAuthSessionGuardType_DeviceCode,
		)
		assert.ErrorContains(t, err, "update fail")
	})
}

func TestAuthenticationService_GenerateAccessTokenForApp(t *testing.T) {
	t.Parallel()

	t.Run("success", func(t *testing.T) {
		t.Parallel()

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
	})

	t.Run("error_api", func(t *testing.T) {
		t.Parallel()

		svc, mock := setupAuthService(t, nil)
		mock.ResponseErrs["Authentication.GenerateAccessTokenForApp"] = errors.New("generate fail")

		_, err := svc.GenerateAccessTokenForApp(t.Context(), "refresh_token", TestSteamID)
		assert.ErrorContains(t, err, "generate fail")
	})
}

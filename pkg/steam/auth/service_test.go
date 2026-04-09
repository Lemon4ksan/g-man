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
	"reflect"
	"testing"

	pb "github.com/lemon4ksan/g-man/pkg/protobuf/steam"
	"github.com/lemon4ksan/g-man/test"
	"google.golang.org/protobuf/proto"
)

const TestTimestamp = 1234567890
const TestSteamID = 123

func setupAuthService(t *testing.T, conf *DeviceConfig) (*AuthenticationService, *test.MockRequester) {
	t.Helper()
	mock := test.NewMockRequester()
	svc := NewAuthenticationService(mock, conf)
	return svc, mock
}

func mockRSAResponse(t *testing.T, mock *test.MockRequester) *rsa.PrivateKey {
	t.Helper()
	privKey, err := rsa.GenerateKey(rand.Reader, 1024)
	if err != nil {
		t.Fatalf("failed to generate RSA key: %v", err)
	}

	modHex := fmt.Sprintf("%x", privKey.N)
	expHex := fmt.Sprintf("%x", privKey.E)

	mock.SetProtoResponse("Authentication", "GetPasswordRSAPublicKey", &pb.CAuthentication_GetPasswordRSAPublicKey_Response{
		PublickeyMod: proto.String(modHex),
		PublickeyExp: proto.String(expHex),
		Timestamp:    proto.Uint64(TestTimestamp),
	})
	return privKey
}

func TestNewAuthenticationService(t *testing.T) {
	t.Run("Default Config", func(t *testing.T) {
		svc, _ := setupAuthService(t, nil)
		if !reflect.DeepEqual(svc.DeviceConf(), DefaultDeviceConfig()) {
			t.Error("expected default device config")
		}
	})

	t.Run("Custom Config", func(t *testing.T) {
		custom := &DeviceConfig{DeviceFriendlyName: "G-man Bot"}
		svc, _ := setupAuthService(t, custom)
		if svc.DeviceConf().DeviceFriendlyName != "G-man Bot" {
			t.Error("expected custom device config")
		}
	})
}

func TestAuthenticationService_EncryptPassword(t *testing.T) {
	svc, mock := setupAuthService(t, nil)
	privKey := mockRSAResponse(t, mock)

	password := "secret_123"
	encBase64, ts, err := svc.EncryptPassword(t.Context(), "user", password)
	if err != nil {
		t.Fatalf("EncryptPassword failed: %v", err)
	}

	if ts != TestTimestamp {
		t.Errorf("expected ts %d, got %d", TestTimestamp, ts)
	}

	cipherText, _ := base64.StdEncoding.DecodeString(encBase64)
	plainText, err := rsa.DecryptPKCS1v15(rand.Reader, privKey, cipherText)
	if err != nil {
		t.Fatalf("failed to decrypt: %v", err)
	}

	if string(plainText) != password {
		t.Errorf("password mismatch: want %q, got %q", password, string(plainText))
	}
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
				mock.SetProtoResponse("Authentication", "GetPasswordRSAPublicKey", &pb.CAuthentication_GetPasswordRSAPublicKey_Response{})
			},
			wantErr: "steam returned empty rsa parameters",
		},
		{
			name: "Invalid Hex",
			setup: func() {
				mock.SetProtoResponse("Authentication", "GetPasswordRSAPublicKey", &pb.CAuthentication_GetPasswordRSAPublicKey_Response{
					PublickeyMod: proto.String("NOT_HEX"),
					PublickeyExp: proto.String("010001"),
				})
			},
			wantErr: "invalid rsa modulus hex string",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.setup()
			_, _, err := svc.EncryptPassword(t.Context(), "user", "pwd")
			if err == nil || (tt.wantErr != "" && !reflect.DeepEqual(err.Error(), tt.wantErr)) {
				t.Errorf("expected error %q, got %v", tt.wantErr, err)
			}
		})
	}
}

func TestAuthenticationService_BeginAuthSessionViaCredentials(t *testing.T) {
	svc, mock := setupAuthService(t, nil)
	mockRSAResponse(t, mock)

	mock.SetProtoResponse("Authentication", "BeginAuthSessionViaCredentials", &pb.CAuthentication_BeginAuthSessionViaCredentials_Response{
		ClientId: proto.Uint64(999),
	})

	resp, err := svc.BeginAuthSessionViaCredentials(t.Context(), "user", "pass", "GUARD_CODE")
	if err != nil {
		t.Fatalf("failed: %v", err)
	}

	if resp.GetClientId() != 999 {
		t.Errorf("expected client id 999, got %d", resp.GetClientId())
	}

	sent := &pb.CAuthentication_BeginAuthSessionViaCredentials_Request{}
	mock.GetLastCall(sent)

	if sent.GetAccountName() != "user" || sent.GetGuardData() != "GUARD_CODE" {
		t.Errorf("wrong request params: %+v", sent)
	}
	if sent.GetEncryptedPassword() == "" {
		t.Error("password was not encrypted")
	}
}

func TestAuthenticationService_PollAuthSessionStatus(t *testing.T) {
	svc, mock := setupAuthService(t, nil)

	mock.SetProtoResponse("Authentication", "PollAuthSessionStatus", &pb.CAuthentication_PollAuthSessionStatus_Response{
		RefreshToken: proto.String("new_token"),
	})

	resp, err := svc.PollAuthSessionStatus(t.Context(), 123, []byte{1, 2})
	if err != nil {
		t.Fatalf("failed: %v", err)
	}

	if resp.GetRefreshToken() != "new_token" {
		t.Error("token mismatch")
	}

	sent := &pb.CAuthentication_PollAuthSessionStatus_Request{}
	mock.GetLastCall(sent)
	if sent.GetClientId() != 123 {
		t.Errorf("expected client id 123, got %d", sent.GetClientId())
	}
}

func TestAuthenticationService_UpdateAuthSessionWithSteamGuardCode(t *testing.T) {
	svc, mock := setupAuthService(t, nil)

	err := svc.UpdateAuthSessionWithSteamGuardCode(t.Context(), 111, TestSteamID, "ABCDE", pb.EAuthSessionGuardType_k_EAuthSessionGuardType_DeviceCode)
	if err != nil {
		t.Fatalf("failed: %v", err)
	}

	sent := &pb.CAuthentication_UpdateAuthSessionWithSteamGuardCode_Request{}
	mock.GetLastCall(sent)

	if sent.GetCode() != "ABCDE" || sent.GetSteamid() != TestSteamID {
		t.Errorf("invalid request: %+v", sent)
	}
}

func TestAuthenticationService_GenerateAccessTokenForApp(t *testing.T) {
	svc, mock := setupAuthService(t, nil)

	mock.SetProtoResponse("Authentication", "GenerateAccessTokenForApp", &pb.CAuthentication_AccessToken_GenerateForApp_Response{
		AccessToken: proto.String("access_token"),
	})

	resp, err := svc.GenerateAccessTokenForApp(t.Context(), "refresh_token", TestSteamID)
	if err != nil {
		t.Fatalf("failed: %v", err)
	}

	if resp.GetAccessToken() != "access_token" {
		t.Error("access token mismatch")
	}
}

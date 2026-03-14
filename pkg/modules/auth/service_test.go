// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package auth

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"errors"
	"fmt"
	"reflect"
	"sync"
	"testing"

	"github.com/lemon4ksan/g-man/pkg/steam/api"
	"github.com/lemon4ksan/g-man/pkg/steam/protocol"
	pb "github.com/lemon4ksan/g-man/pkg/steam/protocol/protobuf"
	"github.com/lemon4ksan/g-man/pkg/steam/transport"
	"google.golang.org/protobuf/proto"
)

type mockUnifiedRequester struct {
	mu           sync.Mutex
	calls        []string
	lastReqMsg   proto.Message
	responses    map[string]proto.Message
	responseErrs map[string]error
}

func newMockUnifiedRequester() *mockUnifiedRequester {
	return &mockUnifiedRequester{
		responses:    make(map[string]proto.Message),
		responseErrs: make(map[string]error),
	}
}

func (m *mockUnifiedRequester) CallUnified(ctx context.Context, httpMethod, iface, method string, version int, reqMsg, respMsg any, mods ...api.RequestModifier) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.calls = append(m.calls, method)
	if msg, ok := reqMsg.(proto.Message); ok {
		m.lastReqMsg = msg
	}

	if err, ok := m.responseErrs[method]; ok && err != nil {
		return err
	}

	if respMsg != nil {
		if resp, ok := m.responses[method]; ok {
			outBytes, _ := proto.Marshal(resp)
			_ = proto.Unmarshal(outBytes, respMsg.(proto.Message))
		}
	}

	return nil
}

func (m *mockUnifiedRequester) Do(req *transport.Request) (*transport.Response, error) {
	panic("Not implemented")
}

func TestNewAuthenticationService(t *testing.T) {
	mockReq := newMockUnifiedRequester()

	svcDefault := NewAuthenticationService(mockReq, nil)
	defConf := DefaultDeviceConfig()
	if !reflect.DeepEqual(svcDefault.DeviceConf(), defConf) {
		t.Errorf("expected default device config")
	}

	customConf := &DeviceConfig{
		DeviceFriendlyName: "Custom Device",
		PlatformType:       pb.EAuthTokenPlatformType_k_EAuthTokenPlatformType_MobileApp,
		OSType:             protocol.EOSType_Android9,
		GamingDeviceType:   500,
	}
	svcCustom := NewAuthenticationService(mockReq, customConf)
	if !reflect.DeepEqual(svcCustom.DeviceConf(), *customConf) {
		t.Errorf("expected custom device config")
	}
}

func TestAuthenticationService_EncryptPassword(t *testing.T) {
	mockReq := newMockUnifiedRequester()
	svc := NewAuthenticationService(mockReq, nil)
	ctx := context.Background()

	privKey, err := rsa.GenerateKey(rand.Reader, 1024)
	if err != nil {
		t.Fatalf("failed to generate test RSA key: %v", err)
	}

	modHex := fmt.Sprintf("%x", privKey.N)
	expHex := fmt.Sprintf("%x", privKey.E)

	mockReq.responses["GetPasswordRSAPublicKey"] = &pb.CAuthentication_GetPasswordRSAPublicKey_Response{
		PublickeyMod: proto.String(modHex),
		PublickeyExp: proto.String(expHex),
		Timestamp:    proto.Uint64(1234567890),
	}

	password := "my_super_secret_password_123!"

	encBase64, timestamp, err := svc.EncryptPassword(ctx, "testuser", password)
	if err != nil {
		t.Fatalf("EncryptPassword failed: %v", err)
	}

	if timestamp != 1234567890 {
		t.Errorf("expected timestamp 1234567890, got %d", timestamp)
	}

	cipherText, err := base64.StdEncoding.DecodeString(encBase64)
	if err != nil {
		t.Fatalf("failed to decode base64: %v", err)
	}

	plainText, err := rsa.DecryptPKCS1v15(rand.Reader, privKey, cipherText)
	if err != nil {
		t.Fatalf("failed to decrypt PKCS1v15 payload: %v", err)
	}

	if string(plainText) != password {
		t.Errorf("decrypted password mismatch: expected '%s', got '%s'", password, string(plainText))
	}
}

func TestAuthenticationService_EncryptPassword_Errors(t *testing.T) {
	mockReq := newMockUnifiedRequester()
	svc := NewAuthenticationService(mockReq, nil)
	ctx := context.Background()

	mockReq.responseErrs["GetPasswordRSAPublicKey"] = errors.New("api down")
	_, _, err := svc.EncryptPassword(ctx, "user", "pwd")
	if err == nil {
		t.Error("expected error when API fails")
	}

	mockReq.responseErrs["GetPasswordRSAPublicKey"] = nil
	mockReq.responses["GetPasswordRSAPublicKey"] = &pb.CAuthentication_GetPasswordRSAPublicKey_Response{}
	_, _, err = svc.EncryptPassword(ctx, "user", "pwd")
	if err == nil || err.Error() != "steam returned empty rsa parameters" {
		t.Errorf("expected empty RSA parameters error, got %v", err)
	}

	mockReq.responses["GetPasswordRSAPublicKey"] = &pb.CAuthentication_GetPasswordRSAPublicKey_Response{
		PublickeyMod: proto.String("INVALID_HEX"),
		PublickeyExp: proto.String("010001"),
	}
	_, _, err = svc.EncryptPassword(ctx, "user", "pwd")
	if err == nil || err.Error() != "invalid rsa modulus hex string" {
		t.Errorf("expected invalid modulus error, got %v", err)
	}
}

func TestAuthenticationService_BeginAuthSessionViaCredentials(t *testing.T) {
	mockReq := newMockUnifiedRequester()
	svc := NewAuthenticationService(mockReq, nil)
	ctx := context.Background()

	privKey, _ := rsa.GenerateKey(rand.Reader, 1024)
	mockReq.responses["GetPasswordRSAPublicKey"] = &pb.CAuthentication_GetPasswordRSAPublicKey_Response{
		PublickeyMod: proto.String(fmt.Sprintf("%x", privKey.PublicKey.N)),
		PublickeyExp: proto.String(fmt.Sprintf("%x", privKey.PublicKey.E)),
		Timestamp:    proto.Uint64(100),
	}

	mockReq.responses["BeginAuthSessionViaCredentials"] = &pb.CAuthentication_BeginAuthSessionViaCredentials_Response{
		ClientId: proto.Uint64(999),
	}

	resp, err := svc.BeginAuthSessionViaCredentials(ctx, "myuser", "mypwd", "STEAMGUARD123")
	if err != nil {
		t.Fatalf("BeginAuthSessionViaCredentials failed: %v", err)
	}

	if resp.GetClientId() != 999 {
		t.Errorf("expected ClientId 999, got %d", resp.GetClientId())
	}

	req := mockReq.lastReqMsg.(*pb.CAuthentication_BeginAuthSessionViaCredentials_Request)

	if req.GetAccountName() != "myuser" {
		t.Errorf("expected account 'myuser', got '%s'", req.GetAccountName())
	}
	if req.GetEncryptedPassword() == "" || req.GetEncryptedPassword() == "mypwd" {
		t.Error("expected password to be encrypted base64")
	}
	if req.GetGuardData() != "STEAMGUARD123" {
		t.Errorf("expected guard data 'STEAMGUARD123', got '%s'", req.GetGuardData())
	}
	if !req.GetRememberLogin() {
		t.Error("expected RememberLogin to be true")
	}
	if req.DeviceDetails.GetDeviceFriendlyName() != DefaultDeviceConfig().DeviceFriendlyName {
		t.Error("expected device details to be populated")
	}
}

func TestAuthenticationService_PollAuthSessionStatus(t *testing.T) {
	mockReq := newMockUnifiedRequester()
	svc := NewAuthenticationService(mockReq, nil)
	ctx := context.Background()

	mockReq.responses["PollAuthSessionStatus"] = &pb.CAuthentication_PollAuthSessionStatus_Response{
		RefreshToken: proto.String("jwt_refresh_token_here"),
	}

	reqID := []byte{0x01, 0x02, 0x03}
	resp, err := svc.PollAuthSessionStatus(ctx, 12345, reqID)

	if err != nil {
		t.Fatalf("PollAuthSessionStatus failed: %v", err)
	}

	if resp.GetRefreshToken() != "jwt_refresh_token_here" {
		t.Errorf("expected refresh token, got %s", resp.GetRefreshToken())
	}

	req := mockReq.lastReqMsg.(*pb.CAuthentication_PollAuthSessionStatus_Request)
	if req.GetClientId() != 12345 {
		t.Errorf("expected client id 12345, got %d", req.GetClientId())
	}
}

func TestAuthenticationService_UpdateAuthSessionWithSteamGuardCode(t *testing.T) {
	mockReq := newMockUnifiedRequester()
	svc := NewAuthenticationService(mockReq, nil)
	ctx := context.Background()

	err := svc.UpdateAuthSessionWithSteamGuardCode(ctx, 111, 222, "QWERT", pb.EAuthSessionGuardType_k_EAuthSessionGuardType_DeviceCode)
	if err != nil {
		t.Fatalf("UpdateAuthSessionWithSteamGuardCode failed: %v", err)
	}

	req := mockReq.lastReqMsg.(*pb.CAuthentication_UpdateAuthSessionWithSteamGuardCode_Request)
	if req.GetClientId() != 111 || req.GetSteamid() != 222 || req.GetCode() != "QWERT" {
		t.Errorf("invalid request params: %+v", req)
	}
	if req.GetCodeType() != pb.EAuthSessionGuardType_k_EAuthSessionGuardType_DeviceCode {
		t.Errorf("invalid code type")
	}
}

func TestAuthenticationService_GenerateAccessTokenForApp(t *testing.T) {
	mockReq := newMockUnifiedRequester()
	svc := NewAuthenticationService(mockReq, nil)
	ctx := context.Background()

	mockReq.responses["GenerateAccessTokenForApp"] = &pb.CAuthentication_AccessToken_GenerateForApp_Response{
		AccessToken: proto.String("short_lived_access_token"),
	}

	resp, err := svc.GenerateAccessTokenForApp(ctx, "my_refresh_token", 76561198000000000)
	if err != nil {
		t.Fatalf("GenerateAccessTokenForApp failed: %v", err)
	}

	if resp.GetAccessToken() != "short_lived_access_token" {
		t.Errorf("expected access token, got %s", resp.GetAccessToken())
	}

	req := mockReq.lastReqMsg.(*pb.CAuthentication_AccessToken_GenerateForApp_Request)
	if req.GetRefreshToken() != "my_refresh_token" || req.GetSteamid() != 76561198000000000 {
		t.Errorf("invalid request params: %+v", req)
	}
}

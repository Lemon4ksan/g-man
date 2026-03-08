// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package auth

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"fmt"
	"math/big"

	"github.com/lemon4ksan/g-man/pkg/steam/api"
	pb "github.com/lemon4ksan/g-man/pkg/steam/protocol/protobuf"
	"google.golang.org/protobuf/proto"
)

// AuthenticationService acts as a gateway for Steam's Unified WebAPI authentication endpoints.
// It handles password encryption and JWT token lifecycle management.
type AuthenticationService struct {
	client     api.UnifiedRequester
	deviceConf DeviceConfig
}

// NewAuthenticationService creates a new service wrapper around a Unified API client.
// If deviceConf is nil, standard defaults are applied.
func NewAuthenticationService(ur api.UnifiedRequester, deviceConf *DeviceConfig) *AuthenticationService {
	conf := DefaultDeviceConfig()
	if deviceConf != nil {
		conf = *deviceConf
	}

	return &AuthenticationService{
		client:     ur,
		deviceConf: conf,
	}
}

// DeviceConf returns a copy of the current device configuration used during auth.
func (s *AuthenticationService) DeviceConf() DeviceConfig {
	return s.deviceConf
}

// GetPasswordRSAPublicKey retrieves the RSA public key parameters specific to the account.
func (s *AuthenticationService) GetPasswordRSAPublicKey(ctx context.Context, accountName string) (*pb.CAuthentication_GetPasswordRSAPublicKey_Response, error) {
	req := &pb.CAuthentication_GetPasswordRSAPublicKey_Request{
		AccountName: proto.String(accountName),
	}
	resp := &pb.CAuthentication_GetPasswordRSAPublicKey_Response{}

	err := s.client.CallUnified(ctx, "GET", "Authentication", "GetPasswordRSAPublicKey", 1, req, resp)
	return resp, err
}

// EncryptPassword securely encrypts the plaintext password using Steam's provided RSA public key.
// It returns the base64-encoded encrypted password and the timestamp of the key used.
func (s *AuthenticationService) EncryptPassword(ctx context.Context, accountName, password string) (string, uint64, error) {
	rsaInfo, err := s.GetPasswordRSAPublicKey(ctx, accountName)
	if err != nil {
		return "", 0, fmt.Errorf("fetch rsa key: %w", err)
	}

	modHex := rsaInfo.GetPublickeyMod()
	expHex := rsaInfo.GetPublickeyExp()

	if modHex == "" || expHex == "" {
		return "", 0, fmt.Errorf("steam returned empty rsa parameters")
	}

	mod := new(big.Int)
	if _, ok := mod.SetString(modHex, 16); !ok {
		return "", 0, fmt.Errorf("invalid rsa modulus hex string")
	}

	exp := new(big.Int)
	if _, ok := exp.SetString(expHex, 16); !ok {
		return "", 0, fmt.Errorf("invalid rsa exponent hex string")
	}

	pubKey := &rsa.PublicKey{
		N: mod,
		E: int(exp.Int64()),
	}

	encrypted, err := rsa.EncryptPKCS1v15(rand.Reader, pubKey, []byte(password))
	if err != nil {
		return "", 0, fmt.Errorf("encrypt password payload: %w", err)
	}

	return base64.StdEncoding.EncodeToString(encrypted), rsaInfo.GetTimestamp(), nil
}

// BeginAuthSessionViaCredentials initiates the login flow with Steam.
func (s *AuthenticationService) BeginAuthSessionViaCredentials(ctx context.Context, accountName, password string, authCode string) (*pb.CAuthentication_BeginAuthSessionViaCredentials_Response, error) {
	encPassword, timestamp, err := s.EncryptPassword(ctx, accountName, password)
	if err != nil {
		return nil, err
	}

	req := &pb.CAuthentication_BeginAuthSessionViaCredentials_Request{
		AccountName:         proto.String(accountName),
		EncryptedPassword:   proto.String(encPassword),
		EncryptionTimestamp: proto.Uint64(timestamp),
		RememberLogin:       proto.Bool(true),
		Persistence:         pb.ESessionPersistence_k_ESessionPersistence_Persistent.Enum(),
		WebsiteId:           proto.String("Client"),
		DeviceDetails:       s.getDeviceDetails(),
	}

	if authCode != "" {
		req.GuardData = proto.String(authCode)
	}

	resp := &pb.CAuthentication_BeginAuthSessionViaCredentials_Response{}
	err = s.client.CallUnified(ctx, "POST", "Authentication", "BeginAuthSessionViaCredentials", 1, req, resp)
	return resp, err
}

// PollAuthSessionStatus repeatedly checks the status of a pending login (e.g., waiting for Mobile confirmation).
func (s *AuthenticationService) PollAuthSessionStatus(ctx context.Context, clientID uint64, requestID []byte) (*pb.CAuthentication_PollAuthSessionStatus_Response, error) {
	req := &pb.CAuthentication_PollAuthSessionStatus_Request{
		ClientId:  proto.Uint64(clientID),
		RequestId: requestID,
	}
	resp := &pb.CAuthentication_PollAuthSessionStatus_Response{}

	err := s.client.CallUnified(ctx, "POST", "Authentication", "PollAuthSessionStatus", 1, req, resp)
	return resp, err
}

// UpdateAuthSessionWithSteamGuardCode submits a 2FA or Email code for an ongoing session.
func (s *AuthenticationService) UpdateAuthSessionWithSteamGuardCode(ctx context.Context, clientID uint64, steamID uint64, code string, codeType pb.EAuthSessionGuardType) error {
	req := &pb.CAuthentication_UpdateAuthSessionWithSteamGuardCode_Request{
		ClientId: proto.Uint64(clientID),
		Steamid:  proto.Uint64(steamID),
		Code:     proto.String(code),
		CodeType: codeType.Enum(),
	}

	// Steam returns an empty response body on success, so we pass nil for the response struct.
	return s.client.CallUnified(ctx, "POST", "Authentication", "UpdateAuthSessionWithSteamGuardCode", 1, req, nil)
}

// GenerateAccessTokenForApp exchanges a RefreshToken for a short-lived AccessToken.
func (s *AuthenticationService) GenerateAccessTokenForApp(ctx context.Context, refreshToken string, steamID uint64) (*pb.CAuthentication_AccessToken_GenerateForApp_Response, error) {
	req := &pb.CAuthentication_AccessToken_GenerateForApp_Request{
		RefreshToken: proto.String(refreshToken),
		Steamid:      proto.Uint64(steamID),
		RenewalType:  pb.ETokenRenewalType_k_ETokenRenewalType_None.Enum(),
	}
	resp := &pb.CAuthentication_AccessToken_GenerateForApp_Response{}

	err := s.client.CallUnified(ctx, "POST", "Authentication", "GenerateAccessTokenForApp", 1, req, resp)
	return resp, err
}

// getDeviceDetails returns the structured device profile.
func (s *AuthenticationService) getDeviceDetails() *pb.CAuthentication_DeviceDetails {
	return &pb.CAuthentication_DeviceDetails{
		DeviceFriendlyName: proto.String(s.deviceConf.DeviceFriendlyName),
		PlatformType:       s.deviceConf.PlatformType.Enum(),
		OsType:             proto.Int32(int32(s.deviceConf.OSType)),
		GamingDeviceType:   proto.Uint32(s.deviceConf.GamingDeviceType),
	}
}

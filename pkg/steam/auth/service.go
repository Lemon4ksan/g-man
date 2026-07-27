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
	"math/big"

	"google.golang.org/protobuf/proto"

	pb "github.com/lemon4ksan/g-man/pkg/protobuf/steam"
	"github.com/lemon4ksan/g-man/pkg/steam/service"
)

var (
	// ErrEmptyRSAParameters indicates Steam returned empty RSA key parameters.
	ErrEmptyRSAParameters = errors.New("auth: steam returned empty rsa parameters")
	// ErrInvalidRSAModulus indicates the RSA modulus hex string was malformed.
	ErrInvalidRSAModulus = errors.New("auth: invalid rsa modulus hex string")
	// ErrInvalidRSAExponent indicates the RSA exponent hex string was malformed.
	ErrInvalidRSAExponent = errors.New("auth: invalid rsa exponent hex string")
)

// AuthenticationService wraps Steam WebAPI unified authentication methods.
type AuthenticationService struct {
	client service.Doer
	conf   DeviceConfig
}

// NewAuthenticationService constructs an AuthenticationService.
func NewAuthenticationService(c service.Doer, cfg *DeviceConfig) *AuthenticationService {
	conf := DefaultDeviceConfig()
	if cfg != nil {
		conf = *cfg
	}

	return &AuthenticationService{
		client: c,
		conf:   conf,
	}
}

// DeviceConf returns current device details.
func (s *AuthenticationService) DeviceConf() DeviceConfig {
	return s.conf
}

// GetPasswordRSAPublicKey fetches account RSA public key parameters.
func (s *AuthenticationService) GetPasswordRSAPublicKey(
	ctx context.Context,
	accountName string,
) (*pb.CAuthentication_GetPasswordRSAPublicKey_Response, error) {
	req := &pb.CAuthentication_GetPasswordRSAPublicKey_Request{
		AccountName: proto.String(accountName),
	}

	return service.Unified[pb.CAuthentication_GetPasswordRSAPublicKey_Response](
		ctx, s.client, req, service.WithHTTPMethod("GET"),
	)
}

// EncryptPassword encrypts plain password strings using Steam's RSA public key.
func (s *AuthenticationService) EncryptPassword(
	ctx context.Context,
	accountName, password string,
) (string, uint64, error) {
	rsaInfo, err := s.GetPasswordRSAPublicKey(ctx, accountName)
	if err != nil {
		return "", 0, fmt.Errorf("fetch rsa key: %w", err)
	}

	modHex := rsaInfo.GetPublickeyMod()
	expHex := rsaInfo.GetPublickeyExp()

	if modHex == "" || expHex == "" {
		return "", 0, ErrEmptyRSAParameters
	}

	mod := new(big.Int)
	if _, ok := mod.SetString(modHex, 16); !ok {
		return "", 0, ErrInvalidRSAModulus
	}

	exp := new(big.Int)
	if _, ok := exp.SetString(expHex, 16); !ok {
		return "", 0, ErrInvalidRSAExponent
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

// BeginAuthSessionViaCredentials initiates a login session.
func (s *AuthenticationService) BeginAuthSessionViaCredentials(
	ctx context.Context,
	accountName, password, authCode string,
) (*pb.CAuthentication_BeginAuthSessionViaCredentials_Response, error) {
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

	return service.Unified[pb.CAuthentication_BeginAuthSessionViaCredentials_Response](
		ctx, s.client, req,
	)
}

// PollAuthSessionStatus queries status for a pending auth session.
func (s *AuthenticationService) PollAuthSessionStatus(
	ctx context.Context,
	clientID uint64,
	requestID []byte,
) (*pb.CAuthentication_PollAuthSessionStatus_Response, error) {
	req := &pb.CAuthentication_PollAuthSessionStatus_Request{
		ClientId:  proto.Uint64(clientID),
		RequestId: requestID,
	}

	return service.Unified[pb.CAuthentication_PollAuthSessionStatus_Response](
		ctx, s.client, req,
	)
}

// UpdateAuthSessionWithSteamGuardCode submits a 2FA or email code.
func (s *AuthenticationService) UpdateAuthSessionWithSteamGuardCode(
	ctx context.Context,
	clientID, steamID uint64,
	code string,
	codeType pb.EAuthSessionGuardType,
) error {
	req := &pb.CAuthentication_UpdateAuthSessionWithSteamGuardCode_Request{
		ClientId: proto.Uint64(clientID),
		Steamid:  proto.Uint64(steamID),
		Code:     proto.String(code),
		CodeType: codeType.Enum(),
	}

	_, err := service.Unified[service.NoResponse](ctx, s.client, req)

	return err
}

// GenerateAccessTokenForApp exchanges a refresh token for an access token.
func (s *AuthenticationService) GenerateAccessTokenForApp(
	ctx context.Context,
	refreshToken string,
	steamID uint64,
) (*pb.CAuthentication_AccessToken_GenerateForApp_Response, error) {
	req := &pb.CAuthentication_AccessToken_GenerateForApp_Request{
		RefreshToken: proto.String(refreshToken),
		Steamid:      proto.Uint64(steamID),
		RenewalType:  pb.ETokenRenewalType_k_ETokenRenewalType_None.Enum(),
	}

	return service.UnifiedExplicit[pb.CAuthentication_AccessToken_GenerateForApp_Response](
		ctx, s.client, "POST", "Authentication", "GenerateAccessTokenForApp", 1, req,
	)
}

func (s *AuthenticationService) getDeviceDetails() *pb.CAuthentication_DeviceDetails {
	return &pb.CAuthentication_DeviceDetails{
		DeviceFriendlyName: proto.String(s.conf.DeviceFriendlyName),
		PlatformType:       s.conf.PlatformType.Enum(),
		OsType:             proto.Int32(int32(s.conf.OSType)),
		GamingDeviceType:   proto.Uint32(s.conf.GamingDeviceType),
	}
}

// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package auth

import (
	pb "github.com/lemon4ksan/g-man/pkg/protobuf/steam"
	"github.com/lemon4ksan/g-man/pkg/steam/id"
	"github.com/lemon4ksan/g-man/pkg/steam/protocol"
)

// DeviceConfig allows customizing how the client presents itself to Steam.
type DeviceConfig struct {
	DeviceFriendlyName string
	PlatformType       pb.EAuthTokenPlatformType
	OSType             protocol.EOSType
	GamingDeviceType   uint32 // 1 = Desktop, 528 = SteamDeck, etc.
}

// DefaultDeviceConfig returns settings mimicking the official Steam Desktop Client on Windows.
func DefaultDeviceConfig() DeviceConfig {
	return DeviceConfig{
		DeviceFriendlyName: "G-man Bot/1.0",
		PlatformType:       pb.EAuthTokenPlatformType_k_EAuthTokenPlatformType_SteamClient,
		OSType:             protocol.EOSType_Windows10,
		GamingDeviceType:   1,
	}
}

// LogOnDetails contains all parameters needed to authenticate with Steam.
// The struct supports multiple authentication methods:
//
//  1. Refresh Token (modern, preferred):
//     RefreshToken = "eyJ..." // JWT token from previous session
//
//  2. Password + Steam Guard:
//     AccountName = "username"
//     Password    = "password"
//     AuthCode    = "ABC123" (optional, for email Steam Guard)
//     TwoFactorCode = "123456" (optional, for mobile 2FA)
//
//  3. Anonymous:
//     (no credentials) // Limited functionality
type LogOnDetails struct {
	// AccountName is the Steam username for password authentication.
	// Ignored if RefreshToken is provided.
	AccountName string

	// Password is the account password.
	Password string

	// RefreshToken is a JWT token from a previous successful login.
	// This is the preferred authentication method as it's more secure
	// and doesn't require storing passwords.
	RefreshToken string

	// AccessToken is a short-lived token that is generated during login.
	AccessToken string

	// SteamID can be provided to avoid looking it up during login.
	// If not provided, it will be extracted from the refresh token or
	// obtained during authentication.
	SteamID id.ID

	// AuthCode is the email-based Steam Guard code.
	// Required when Steam Guard is enabled and not using 2FA.
	AuthCode string

	// TwoFactorCode is the mobile authenticator code.
	// Required when 2FA is enabled.
	TwoFactorCode string

	// MachineID is the unique machine identifier for the client.
	MachineID []byte

	// CellID is the client region identifier.
	CellID uint32

	// ClientOSType identifies the client operating system.
	// Defaults to Windows 10 if not specified.
	ClientOSType uint32

	// ProtocolVersion is the Steam protocol version.
	// Defaults to [ProtocolVersion] if not specified.
	ProtocolVersion uint32

	// ClientLanguage specifies the language the client should use.
	// Defaults to "english" if not specified.
	ClientLanguage string
}

// NewLogOnDetails creates a new structure with default fields.
func NewLogOnDetails(account, password string) *LogOnDetails {
	return &LogOnDetails{
		AccountName:     account,
		Password:        password,
		ClientOSType:    uint32(protocol.EOSType_Windows10),
		ProtocolVersion: ProtocolVersion,
		ClientLanguage:  "english",
	}
}

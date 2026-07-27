// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package auth

import (
	"errors"
	"os"

	pb "github.com/lemon4ksan/g-man/pkg/protobuf/steam"
	"github.com/lemon4ksan/g-man/pkg/steam/id"
	"github.com/lemon4ksan/g-man/pkg/steam/protocol/enums"
)

var (
	ErrMissingAccountOrToken = errors.New("auth: account name or refresh token is required")
	ErrMissingPassword       = errors.New("auth: password is required when refresh token is missing")
)

// DeviceConfig configures hardware and platform properties presented to Steam servers during logon.
type DeviceConfig struct {
	DeviceFriendlyName string
	PlatformType       pb.EAuthTokenPlatformType
	OSType             enums.EOSType
	GamingDeviceType   uint32
}

// DefaultDeviceConfig builds standard desktop client parameters.
func DefaultDeviceConfig() DeviceConfig {
	hostname, err := os.Hostname()
	if err != nil || hostname == "" {
		hostname = "DESKTOP-PC"
	}

	return DeviceConfig{
		DeviceFriendlyName: hostname,
		PlatformType:       pb.EAuthTokenPlatformType_k_EAuthTokenPlatformType_SteamClient,
		OSType:             enums.EOSType_Windows10,
		GamingDeviceType:   1,
	}
}

// LogOnDetails encapsulates parameters required for Steam authentication via tokens or credentials.
type LogOnDetails struct {
	AccountName     string
	Password        string
	RefreshToken    string
	AccessToken     string
	SteamID         id.ID
	AuthCode        string
	TwoFactorCode   string
	MachineID       []byte
	MachineName     string
	ClientOSType    uint32
	ProtocolVersion uint32
	ClientLanguage  string
}

// Validate checks completeness of logon parameters.
func (l *LogOnDetails) Validate() error {
	if l.ClientOSType == 0 {
		l.ClientOSType = uint32(enums.EOSType_Windows10)
	}

	if l.ProtocolVersion == 0 {
		l.ProtocolVersion = ProtocolVersion
	}

	if l.ClientLanguage == "" {
		l.ClientLanguage = "english"
	}

	if l.RefreshToken == "" && l.AccountName == "" {
		return ErrMissingAccountOrToken
	}

	if l.RefreshToken == "" && l.Password == "" {
		return ErrMissingPassword
	}

	return nil
}

// Wipe clears sensitive password and 2FA strings from memory.
func (l *LogOnDetails) Wipe() {
	l.Password = ""
	l.AuthCode = ""
	l.TwoFactorCode = ""
}

// NewLogOnDetails constructs a LogOnDetails instance with defaults.
func NewLogOnDetails(account, password string) *LogOnDetails {
	hostname, err := os.Hostname()
	if err != nil || hostname == "" {
		hostname = "DESKTOP-PC"
	}

	return &LogOnDetails{
		AccountName:     account,
		Password:        password,
		ClientOSType:    uint32(enums.EOSType_Windows10),
		MachineName:     hostname,
		ProtocolVersion: ProtocolVersion,
		ClientLanguage:  "english",
	}
}

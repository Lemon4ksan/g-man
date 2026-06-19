// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package account

import "github.com/lemon4ksan/miyako/bus"

// InfoEvent is emitted when Steam sends us updated account details.
type InfoEvent struct {
	bus.BaseEvent
	PersonaName                     string
	IPCountry                       string
	CountAuthedComputers            int32
	AccountFlags                    uint32
	SteamguardMachineNameUserChosen string
	IsPhoneVerified                 bool
	TwoFactorState                  uint32
	IsPhoneIdentifying              bool
	IsPhoneNeedingReverify          bool
}

// EmailInfoEvent is emitted when Steam sends us updated email details.
type EmailInfoEvent struct {
	bus.BaseEvent
	EmailAddress                         string
	EmailIsValidated                     bool
	EmailValidationChanged               bool
	CredentialChangeRequiresCode         bool
	PasswordOrSecretqaChangeRequiresCode bool
}

// LimitationsEvent is emitted when Steam sends us updated account limitations.
type LimitationsEvent struct {
	bus.BaseEvent
	IsLimitedAccount                       bool
	IsCommunityBanned                      bool
	IsLockedAccount                        bool
	IsLimitedAccountAllowedToInviteFriends bool
}

// VACBansEvent is emitted when Steam sends us updated VAC ban status.
type VACBansEvent struct {
	bus.BaseEvent
	NumBans uint32
	AppIDs  []uint32
	Ranges  [][2]uint32
}

// WalletInfoEvent is emitted when Steam sends us updated wallet information.
type WalletInfoEvent struct {
	bus.BaseEvent
	HasWallet      bool
	Balance        int64 // Balance is represented as amount in cents
	Currency       int32
	BalanceDelayed int64
	Realm          int32
}

// VanityURLChangedEvent is emitted when Steam sends us vanity URL changes.
type VanityURLChangedEvent struct {
	bus.BaseEvent
	VanityURL string
}

// GiftsUpdatedEvent is emitted when our guest passes/gifts list changes.
type GiftsUpdatedEvent struct {
	bus.BaseEvent
	Gifts []map[string]any
}

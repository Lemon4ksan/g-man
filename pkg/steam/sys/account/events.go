// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package account

import "github.com/lemon4ksan/foundation/async/event"

type InfoEvent struct {
	event.BaseEvent
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

type EmailInfoEvent struct {
	event.BaseEvent
	EmailAddress                         string
	EmailIsValidated                     bool
	EmailValidationChanged               bool
	CredentialChangeRequiresCode         bool
	PasswordOrSecretqaChangeRequiresCode bool
}

type LimitationsEvent struct {
	event.BaseEvent
	IsLimitedAccount                       bool
	IsCommunityBanned                      bool
	IsLockedAccount                        bool
	IsLimitedAccountAllowedToInviteFriends bool
}

type VACBansEvent struct {
	event.BaseEvent
	NumBans uint32
	AppIDs  []uint32
	Ranges  [][2]uint32
}

type WalletInfoEvent struct {
	event.BaseEvent
	HasWallet      bool
	Balance        int64
	Currency       int32
	BalanceDelayed int64
	Realm          int32
}

type VanityURLChangedEvent struct {
	event.BaseEvent
	VanityURL string
}

type GiftsUpdatedEvent struct {
	event.BaseEvent
	Gifts []map[string]any
}

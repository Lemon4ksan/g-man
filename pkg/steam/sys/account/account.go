// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package account tracks account limitations, VAC status, wallet balances, and guest pass lists.
package account

import (
	"slices"
	"sync/atomic"

	pb "github.com/lemon4ksan/g-man/protobuf/steam"
	"github.com/lemon4ksan/g-man/pkg/steam/client"
	"github.com/lemon4ksan/g-man/pkg/steam/module"
)

const ModuleName string = "account"

// WithModule registers the Account module in the client.
func WithModule() client.Option {
	return client.WithModule(New())
}

// From retrieves the Account module instance from the client.
func From(c *client.Client) *Account {
	return client.GetModule[*Account](c)
}

// Snapshot is an immutable point-in-time state of the Steam account.
type Snapshot struct {
	Info        InfoEvent
	Email       EmailInfoEvent
	Limitations LimitationsEvent
	VACBans     VACBansEvent
	Wallet      WalletInfoEvent
	VanityURL   string
	Gifts       []map[string]any
}

// Account caches account state via lock-free atomic snapshots.
//
// Thread Safety:
//   - Lock-free for all reads (0 mutexes, atomic pointer load).
//   - Safe for concurrent use across all methods.
type Account struct {
	module.Base

	events Events
	state  atomic.Pointer[Snapshot]
}

// New constructs an Account module instance.
func New() *Account {
	a := &Account{
		Base: module.New(ModuleName),
	}
	a.state.Store(&Snapshot{})

	return a
}

func (a *Account) Init(init module.InitContext) error {
	if err := a.Base.Init(init); err != nil {
		return err
	}

	a.events = NewEvents(init)
	a.events.OnAccountInfo(a.handleAccountInfo)
	a.events.OnEmailAddrInfo(a.handleEmailAddrInfo)
	a.events.OnIsLimitedAccount(a.handleIsLimitedAccount)
	a.events.OnVACBanStatus(a.handleVACBanStatus)
	a.events.OnWalletInfoUpdate(a.handleWalletInfoUpdate)
	a.events.OnVanityURLChanged(a.handleVanityURLChanged)
	a.events.OnUpdateGuestPassesList(a.handleGuestPasses)

	return nil
}

func (a *Account) Close() error {
	if a.events != nil {
		_ = a.events.Close()
	}

	return a.Base.Close()
}

// Snapshot returns an immutable point-in-time state snapshot.
func (a *Account) Snapshot() Snapshot {
	return *a.state.Load()
}

// Info returns cached account info details.
func (a *Account) Info() InfoEvent {
	return a.state.Load().Info
}

// Email returns cached email address details.
func (a *Account) Email() EmailInfoEvent {
	return a.state.Load().Email
}

// Limitations returns cached account limitation flags.
func (a *Account) Limitations() LimitationsEvent {
	return a.state.Load().Limitations
}

// VACBans returns cached VAC ban details.
func (a *Account) VACBans() VACBansEvent {
	return a.state.Load().VACBans
}

// Wallet returns cached wallet balance details.
func (a *Account) Wallet() WalletInfoEvent {
	return a.state.Load().Wallet
}

// VanityURL returns cached vanity URL slug.
func (a *Account) VanityURL() string {
	return a.state.Load().VanityURL
}

// Gifts returns a cloned slice of cached guest passes.
func (a *Account) Gifts() []map[string]any {
	return slices.Clone(a.state.Load().Gifts)
}

func (a *Account) updateState(fn func(next *Snapshot)) {
	for {
		current := a.state.Load()
		next := *current
		fn(&next)

		if a.state.CompareAndSwap(current, &next) {
			return
		}
	}
}

func (a *Account) handleAccountInfo(msg *pb.CMsgClientAccountInfo) {
	ev := InfoEvent{
		PersonaName:                     msg.GetPersonaName(),
		IPCountry:                       msg.GetIpCountry(),
		CountAuthedComputers:            msg.GetCountAuthedComputers(),
		AccountFlags:                    msg.GetAccountFlags(),
		SteamguardMachineNameUserChosen: msg.GetSteamguardMachineNameUserChosen(),
		IsPhoneVerified:                 msg.GetIsPhoneVerified(),
		TwoFactorState:                  msg.GetTwoFactorState(),
		IsPhoneIdentifying:              msg.GetIsPhoneIdentifying(),
		IsPhoneNeedingReverify:          msg.GetIsPhoneNeedingReverify(),
	}

	a.updateState(func(s *Snapshot) {
		s.Info = ev
	})

	a.Bus.Publish(&ev)
}

func (a *Account) handleEmailAddrInfo(msg *pb.CMsgClientEmailAddrInfo) {
	ev := EmailInfoEvent{
		EmailAddress:                         msg.GetEmailAddress(),
		EmailIsValidated:                     msg.GetEmailIsValidated(),
		EmailValidationChanged:               msg.GetEmailValidationChanged(),
		CredentialChangeRequiresCode:         msg.GetCredentialChangeRequiresCode(),
		PasswordOrSecretqaChangeRequiresCode: msg.GetPasswordOrSecretqaChangeRequiresCode(),
	}

	a.updateState(func(s *Snapshot) {
		s.Email = ev
	})

	a.Bus.Publish(&ev)
}

func (a *Account) handleIsLimitedAccount(msg *pb.CMsgClientIsLimitedAccount) {
	ev := LimitationsEvent{
		IsLimitedAccount:                       msg.GetBisLimitedAccount(),
		IsCommunityBanned:                      msg.GetBisCommunityBanned(),
		IsLockedAccount:                        msg.GetBisLockedAccount(),
		IsLimitedAccountAllowedToInviteFriends: msg.GetBisLimitedAccountAllowedToInviteFriends(),
	}

	a.updateState(func(s *Snapshot) {
		s.Limitations = ev
	})

	a.Bus.Publish(&ev)
}

func (a *Account) handleVACBanStatus(bans *VACBansEvent) {
	if bans == nil {
		return
	}

	a.updateState(func(s *Snapshot) {
		s.VACBans = *bans
	})

	a.Bus.Publish(bans)
}

func (a *Account) handleWalletInfoUpdate(msg *pb.CMsgClientWalletInfoUpdate) {
	bal := int64(msg.GetBalance())
	if msg.Balance64 != nil {
		bal = msg.GetBalance64()
	}

	balDel := int64(msg.GetBalanceDelayed())
	if msg.Balance64Delayed != nil {
		balDel = msg.GetBalance64Delayed()
	}

	ev := WalletInfoEvent{
		HasWallet:      msg.GetHasWallet(),
		Balance:        bal,
		Currency:       msg.GetCurrency(),
		BalanceDelayed: balDel,
		Realm:          msg.GetRealm(),
	}

	a.updateState(func(s *Snapshot) {
		s.Wallet = ev
	})

	a.Bus.Publish(&ev)
}

func (a *Account) handleVanityURLChanged(msg *pb.CMsgClientVanityURLChangedNotification) {
	ev := VanityURLChangedEvent{
		VanityURL: msg.GetVanityUrl(),
	}

	a.updateState(func(s *Snapshot) {
		s.VanityURL = ev.VanityURL
	})

	a.Bus.Publish(&ev)
}

func (a *Account) handleGuestPasses(gifts []map[string]any) {
	if gifts == nil {
		return
	}

	a.updateState(func(s *Snapshot) {
		s.Gifts = gifts
	})

	a.Bus.Publish(&GiftsUpdatedEvent{Gifts: slices.Clone(gifts)})
}

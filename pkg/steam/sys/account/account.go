// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package account tracks account limitations, VAC status, wallet balances, and guest pass lists.
package account

import (
	"encoding/binary"
	"sync"

	"github.com/lemon4ksan/miyako/log"

	pb "github.com/lemon4ksan/g-man/pkg/protobuf/steam"
	"github.com/lemon4ksan/g-man/pkg/steam"
	"github.com/lemon4ksan/g-man/pkg/steam/encoding/bvdf"
	"github.com/lemon4ksan/g-man/pkg/steam/module"
	"github.com/lemon4ksan/g-man/pkg/steam/protocol"
	"github.com/lemon4ksan/g-man/pkg/steam/protocol/enums"
)

const ModuleName string = "account"

// WithModule registers the Account module in the client.
func WithModule() steam.Option {
	return steam.WithModule(New())
}

// From retrieves the Account module instance from the client.
func From(c *steam.Client) *Account {
	return steam.GetModule[*Account](c)
}

// Account caches account info, wallet state, and VAC ban status updates sent by Steam.
//
// Thread Safety:
//   - Safe for concurrent use across all getter methods.
type Account struct {
	module.Base

	mu          sync.RWMutex
	info        InfoEvent
	email       EmailInfoEvent
	limitations LimitationsEvent
	vacBans     VACBansEvent
	wallet      WalletInfoEvent
	vanityURL   VanityURLChangedEvent
	gifts       []map[string]any

	unregFuncs []func()
}

// New constructs an Account module instance.
func New() *Account {
	return &Account{
		Base: module.New(ModuleName),
	}
}

func (a *Account) Init(init module.InitContext) error {
	if err := a.Base.Init(init); err != nil {
		return err
	}

	init.RegisterPacketHandler(enums.EMsg_ClientAccountInfo, a.handleAccountInfo)
	init.RegisterPacketHandler(enums.EMsg_ClientEmailAddrInfo, a.handleEmailAddrInfo)
	init.RegisterPacketHandler(enums.EMsg_ClientIsLimitedAccount, a.handleIsLimitedAccount)
	init.RegisterPacketHandler(enums.EMsg_ClientVACBanStatus, a.handleVACBanStatus)
	init.RegisterPacketHandler(enums.EMsg_ClientWalletInfoUpdate, a.handleWalletInfoUpdate)
	init.RegisterPacketHandler(enums.EMsg_ClientVanityURLChangedNotification, a.handleVanityURLChangedNotification)
	init.RegisterPacketHandler(enums.EMsg_ClientUpdateGuestPassesList, a.handleUpdateGuestPassesList)

	a.unregFuncs = append(a.unregFuncs, func() {
		init.UnregisterPacketHandler(enums.EMsg_ClientAccountInfo)
		init.UnregisterPacketHandler(enums.EMsg_ClientEmailAddrInfo)
		init.UnregisterPacketHandler(enums.EMsg_ClientIsLimitedAccount)
		init.UnregisterPacketHandler(enums.EMsg_ClientVACBanStatus)
		init.UnregisterPacketHandler(enums.EMsg_ClientWalletInfoUpdate)
		init.UnregisterPacketHandler(enums.EMsg_ClientVanityURLChangedNotification)
		init.UnregisterPacketHandler(enums.EMsg_ClientUpdateGuestPassesList)
	})

	return nil
}

func (a *Account) Close() error {
	a.mu.Lock()
	for _, unreg := range a.unregFuncs {
		unreg()
	}

	a.unregFuncs = nil
	a.mu.Unlock()

	return a.Base.Close()
}

// GetAccountInfo returns cached account info details.
func (a *Account) GetAccountInfo() InfoEvent {
	a.mu.RLock()
	defer a.mu.RUnlock()

	return a.info
}

// GetEmailInfo returns cached email address details.
func (a *Account) GetEmailInfo() EmailInfoEvent {
	a.mu.RLock()
	defer a.mu.RUnlock()

	return a.email
}

// GetLimitations returns cached account limitation flags.
func (a *Account) GetLimitations() LimitationsEvent {
	a.mu.RLock()
	defer a.mu.RUnlock()

	return a.limitations
}

// GetVACBans returns cached VAC ban details.
func (a *Account) GetVACBans() VACBansEvent {
	a.mu.RLock()
	defer a.mu.RUnlock()

	return a.vacBans
}

// GetWalletInfo returns cached wallet balance details.
func (a *Account) GetWalletInfo() WalletInfoEvent {
	a.mu.RLock()
	defer a.mu.RUnlock()

	return a.wallet
}

// GetVanityURL returns cached vanity URL slug.
func (a *Account) GetVanityURL() string {
	a.mu.RLock()
	defer a.mu.RUnlock()

	return a.vanityURL.VanityURL
}

// GetGifts returns cached guest passes/gifts list.
func (a *Account) GetGifts() []map[string]any {
	a.mu.RLock()
	defer a.mu.RUnlock()

	return a.gifts
}

func (a *Account) handleAccountInfo(packet *protocol.Packet) {
	msg := &pb.CMsgClientAccountInfo{}
	if err := protocol.UnmarshalProto(packet.Payload, msg); err != nil {
		a.Logger.Error("Failed to unmarshal account info", log.Err(err))
		return
	}

	a.mu.Lock()
	a.info = InfoEvent{
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
	a.mu.Unlock()

	a.Bus.Publish(&a.info)
}

func (a *Account) handleEmailAddrInfo(packet *protocol.Packet) {
	msg := &pb.CMsgClientEmailAddrInfo{}
	if err := protocol.UnmarshalProto(packet.Payload, msg); err != nil {
		a.Logger.Error("Failed to unmarshal email addr info", log.Err(err))
		return
	}

	a.mu.Lock()
	a.email = EmailInfoEvent{
		EmailAddress:                         msg.GetEmailAddress(),
		EmailIsValidated:                     msg.GetEmailIsValidated(),
		EmailValidationChanged:               msg.GetEmailValidationChanged(),
		CredentialChangeRequiresCode:         msg.GetCredentialChangeRequiresCode(),
		PasswordOrSecretqaChangeRequiresCode: msg.GetPasswordOrSecretqaChangeRequiresCode(),
	}
	a.mu.Unlock()

	a.Bus.Publish(&a.email)
}

func (a *Account) handleIsLimitedAccount(packet *protocol.Packet) {
	msg := &pb.CMsgClientIsLimitedAccount{}
	if err := protocol.UnmarshalProto(packet.Payload, msg); err != nil {
		a.Logger.Error("Failed to unmarshal ClientIsLimited", log.Err(err))
		return
	}

	a.mu.Lock()
	a.limitations = LimitationsEvent{
		IsLimitedAccount:                       msg.GetBisLimitedAccount(),
		IsCommunityBanned:                      msg.GetBisCommunityBanned(),
		IsLockedAccount:                        msg.GetBisLockedAccount(),
		IsLimitedAccountAllowedToInviteFriends: msg.GetBisLimitedAccountAllowedToInviteFriends(),
	}
	a.mu.Unlock()

	a.Bus.Publish(&a.limitations)
}

func (a *Account) handleVACBanStatus(packet *protocol.Packet) {
	if len(packet.Payload) < 4 {
		a.Logger.Warn("VACBanStatus payload too short")
		return
	}

	numBans := binary.LittleEndian.Uint32(packet.Payload[0:4])
	offset := 4
	appIDs := make([]uint32, 0)
	ranges := make([][2]uint32, 0)

	for range numBans {
		if offset+12 > len(packet.Payload) {
			break
		}

		rangeStart := binary.LittleEndian.Uint32(packet.Payload[offset : offset+4])
		rangeEnd := binary.LittleEndian.Uint32(packet.Payload[offset+4 : offset+8])
		offset += 12

		if rangeEnd < rangeStart {
			rangeStart, rangeEnd = rangeEnd, rangeStart
		}

		ranges = append(ranges, [2]uint32{rangeStart, rangeEnd})

		for j := rangeStart; j <= rangeEnd; j++ {
			appIDs = append(appIDs, j)
		}
	}

	a.mu.Lock()
	a.vacBans = VACBansEvent{
		NumBans: numBans,
		AppIDs:  appIDs,
		Ranges:  ranges,
	}
	a.mu.Unlock()

	a.Bus.Publish(&a.vacBans)
}

func (a *Account) handleWalletInfoUpdate(packet *protocol.Packet) {
	msg := &pb.CMsgClientWalletInfoUpdate{}
	if err := protocol.UnmarshalProto(packet.Payload, msg); err != nil {
		a.Logger.Error("Failed to unmarshal wallet info update", log.Err(err))
		return
	}

	balance := int64(msg.GetBalance())
	if msg.Balance64 != nil {
		balance = msg.GetBalance64()
	}

	balanceDelayed := int64(msg.GetBalanceDelayed())
	if msg.Balance64Delayed != nil {
		balanceDelayed = msg.GetBalance64Delayed()
	}

	a.mu.Lock()
	a.wallet = WalletInfoEvent{
		HasWallet:      msg.GetHasWallet(),
		Balance:        balance,
		Currency:       msg.GetCurrency(),
		BalanceDelayed: balanceDelayed,
		Realm:          msg.GetRealm(),
	}
	a.mu.Unlock()

	a.Bus.Publish(&a.wallet)
}

func (a *Account) handleVanityURLChangedNotification(packet *protocol.Packet) {
	msg := &pb.CMsgClientVanityURLChangedNotification{}
	if err := protocol.UnmarshalProto(packet.Payload, msg); err != nil {
		a.Logger.Error("Failed to unmarshal vanity URL changed notification", log.Err(err))
		return
	}

	a.mu.Lock()
	a.vanityURL = VanityURLChangedEvent{
		VanityURL: msg.GetVanityUrl(),
	}
	a.mu.Unlock()

	a.Bus.Publish(&a.vanityURL)
}

func (a *Account) handleUpdateGuestPassesList(packet *protocol.Packet) {
	if len(packet.Payload) < 12 {
		a.Logger.Warn("UpdateGuestPassesList payload too short")
		return
	}

	eresult := binary.LittleEndian.Uint32(packet.Payload[0:4])
	if enums.EResult(eresult) != enums.EResult_OK {
		return
	}

	countToGive := binary.LittleEndian.Uint32(packet.Payload[4:8])
	countToRedeem := binary.LittleEndian.Uint32(packet.Payload[8:12])

	offset := 12
	for range countToGive {
		var discard map[string]any
		if err := bvdf.UnmarshalOffset(packet.Payload, &offset, &discard); err != nil {
			a.Logger.Error("Failed to parse discarded guest pass", log.Err(err))
			return
		}
	}

	gifts := make([]map[string]any, 0, countToRedeem)
	for range countToRedeem {
		var gift map[string]any
		if err := bvdf.UnmarshalOffset(packet.Payload, &offset, &gift); err != nil {
			a.Logger.Error("Failed to parse guest pass/gift KV", log.Err(err))
			return
		}

		if msgObj, ok := gift["MessageObject"].(map[string]any); ok {
			gift = msgObj
		}

		gifts = append(gifts, gift)
	}

	a.mu.Lock()
	a.gifts = gifts
	a.mu.Unlock()

	a.Bus.Publish(&GiftsUpdatedEvent{Gifts: gifts})
}

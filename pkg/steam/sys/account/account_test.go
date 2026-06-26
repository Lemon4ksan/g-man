// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package account

import (
	"encoding/binary"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	proto "google.golang.org/protobuf/proto"

	pb "github.com/lemon4ksan/g-man/pkg/protobuf/steam"
	"github.com/lemon4ksan/g-man/pkg/steam/protocol"
	"github.com/lemon4ksan/g-man/pkg/steam/protocol/enums"
	module "github.com/lemon4ksan/g-man/test/mock"
)

func setup(t *testing.T) (*Account, *module.InitContext) {
	t.Helper()

	a := New()
	ictx := module.NewInitContext()

	require.NoError(t, a.Init(ictx), "failed to init account module")

	t.Cleanup(func() {
		_ = a.Close()
	})

	return a, ictx
}

func TestAccount_InitAndClose(t *testing.T) {
	a := New()
	ictx := module.NewInitContext()

	assert.Equal(t, ModuleName, a.Name())

	err := a.Init(ictx)
	require.NoError(t, err)

	ictx.AssertPacketHandlerRegistered(t, enums.EMsg_ClientAccountInfo)
	ictx.AssertPacketHandlerRegistered(t, enums.EMsg_ClientEmailAddrInfo)

	err = a.Close()
	require.NoError(t, err)

	ictx.AssertPacketHandlerUnregistered(t, enums.EMsg_ClientAccountInfo)
	ictx.AssertPacketHandlerUnregistered(t, enums.EMsg_ClientEmailAddrInfo)
}

func TestAccount_HandleAccountInfo(t *testing.T) {
	a, ictx := setup(t)

	sub := ictx.Bus().Subscribe(&InfoEvent{})

	ictx.EmitPacket(t, enums.EMsg_ClientAccountInfo, &pb.CMsgClientAccountInfo{
		PersonaName:          proto.String("Arseny"),
		IpCountry:            proto.String("RU"),
		CountAuthedComputers: proto.Int32(2),
		AccountFlags:         proto.Uint32(1337),
	})

	info := a.GetAccountInfo()
	assert.Equal(t, "Arseny", info.PersonaName)
	assert.Equal(t, "RU", info.IPCountry)
	assert.Equal(t, int32(2), info.CountAuthedComputers)
	assert.Equal(t, uint32(1337), info.AccountFlags)

	select {
	case ev := <-sub.C():
		e := ev.(*InfoEvent)
		assert.Equal(t, "Arseny", e.PersonaName)
	case <-time.After(100 * time.Millisecond):
		t.Fatal("Event not received")
	}
}

func TestAccount_HandleEmailAddrInfo(t *testing.T) {
	a, ictx := setup(t)

	sub := ictx.Bus().Subscribe(&EmailInfoEvent{})

	ictx.EmitPacket(t, enums.EMsg_ClientEmailAddrInfo, &pb.CMsgClientEmailAddrInfo{
		EmailAddress:     proto.String("test@test.com"),
		EmailIsValidated: proto.Bool(true),
	})

	email := a.GetEmailInfo()
	assert.Equal(t, "test@test.com", email.EmailAddress)
	assert.True(t, email.EmailIsValidated)

	select {
	case ev := <-sub.C():
		e := ev.(*EmailInfoEvent)
		assert.Equal(t, "test@test.com", e.EmailAddress)
	case <-time.After(100 * time.Millisecond):
		t.Fatal("Event not received")
	}
}

func TestAccount_HandleIsLimitedAccount(t *testing.T) {
	a, ictx := setup(t)

	sub := ictx.Bus().Subscribe(&LimitationsEvent{})

	ictx.EmitPacket(t, enums.EMsg_ClientIsLimitedAccount, &pb.CMsgClientIsLimitedAccount{
		BisLimitedAccount: proto.Bool(true),
	})

	limits := a.GetLimitations()
	assert.True(t, limits.IsLimitedAccount)

	select {
	case ev := <-sub.C():
		e := ev.(*LimitationsEvent)
		assert.True(t, e.IsLimitedAccount)
	case <-time.After(100 * time.Millisecond):
		t.Fatal("Event not received")
	}
}

func TestAccount_HandleVACBanStatus(t *testing.T) {
	a, ictx := setup(t)

	sub := ictx.Bus().Subscribe(&VACBansEvent{})

	payload := make([]byte, 16)
	binary.LittleEndian.PutUint32(payload[0:4], 1)
	binary.LittleEndian.PutUint32(payload[4:8], 440)
	binary.LittleEndian.PutUint32(payload[8:12], 440)
	binary.LittleEndian.PutUint32(payload[12:16], 0)

	a.handleVACBanStatus(&protocol.Packet{
		EMsg:    enums.EMsg_ClientVACBanStatus,
		Payload: payload,
	})

	vac := a.GetVACBans()
	assert.Equal(t, uint32(1), vac.NumBans)
	assert.Contains(t, vac.AppIDs, uint32(440))

	select {
	case ev := <-sub.C():
		e := ev.(*VACBansEvent)
		assert.Equal(t, uint32(1), e.NumBans)
	case <-time.After(100 * time.Millisecond):
		t.Fatal("Event not received")
	}
}

func TestAccount_HandleWalletInfoUpdate(t *testing.T) {
	a, ictx := setup(t)

	sub := ictx.Bus().Subscribe(&WalletInfoEvent{})

	ictx.EmitPacket(t, enums.EMsg_ClientWalletInfoUpdate, &pb.CMsgClientWalletInfoUpdate{
		HasWallet: proto.Bool(true),
		Balance:   proto.Int32(1050),
		Currency:  proto.Int32(1),
	})

	wallet := a.GetWalletInfo()
	assert.True(t, wallet.HasWallet)
	assert.Equal(t, int64(1050), wallet.Balance)

	select {
	case ev := <-sub.C():
		e := ev.(*WalletInfoEvent)
		assert.Equal(t, int64(1050), e.Balance)
	case <-time.After(100 * time.Millisecond):
		t.Fatal("Event not received")
	}
}

func TestAccount_HandleVanityURLChangedNotification(t *testing.T) {
	a, ictx := setup(t)

	sub := ictx.Bus().Subscribe(&VanityURLChangedEvent{})

	ictx.EmitPacket(t, enums.EMsg_ClientVanityURLChangedNotification, &pb.CMsgClientVanityURLChangedNotification{
		VanityUrl: proto.String("custom_vanity"),
	})

	assert.Equal(t, "custom_vanity", a.GetVanityURL())

	select {
	case ev := <-sub.C():
		e := ev.(*VanityURLChangedEvent)
		assert.Equal(t, "custom_vanity", e.VanityURL)
	case <-time.After(100 * time.Millisecond):
		t.Fatal("Event not received")
	}
}

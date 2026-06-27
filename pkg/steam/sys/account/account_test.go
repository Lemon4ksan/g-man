// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package account

import (
	"bytes"
	"encoding/binary"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	proto "google.golang.org/protobuf/proto"

	pb "github.com/lemon4ksan/g-man/pkg/protobuf/steam"
	"github.com/lemon4ksan/g-man/pkg/steam/protocol"
	"github.com/lemon4ksan/g-man/pkg/steam/protocol/enums"
	"github.com/lemon4ksan/g-man/test/mock"
)

func setup(t *testing.T) (*Account, *mock.InitContext) {
	t.Helper()

	a := New()
	ictx := mock.NewInitContext()

	require.NoError(t, a.Init(ictx), "failed to init account module")

	t.Cleanup(func() {
		_ = a.Close()
	})

	return a, ictx
}

func encodeCString(b *bytes.Buffer, s string) {
	b.WriteString(s)
	b.WriteByte(0)
}

func TestAccount_InitAndClose(t *testing.T) {
	t.Parallel()

	t.Run("success_lifecycle", func(t *testing.T) {
		t.Parallel()

		a := New()
		ictx := mock.NewInitContext()

		assert.Equal(t, ModuleName, a.Name())

		err := a.Init(ictx)
		require.NoError(t, err)

		ictx.AssertPacketHandlerRegistered(t, enums.EMsg_ClientAccountInfo)
		ictx.AssertPacketHandlerRegistered(t, enums.EMsg_ClientEmailAddrInfo)

		err = a.Close()
		require.NoError(t, err)

		ictx.AssertPacketHandlerUnregistered(t, enums.EMsg_ClientAccountInfo)
		ictx.AssertPacketHandlerUnregistered(t, enums.EMsg_ClientEmailAddrInfo)
	})
}

func TestAccount_HandleAccountInfo(t *testing.T) {
	t.Parallel()

	t.Run("success", func(t *testing.T) {
		t.Parallel()
		a, ictx := setup(t)

		sub := ictx.Bus().Subscribe(&InfoEvent{})
		defer sub.Unsubscribe()

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
	})

	t.Run("error_unmarshal", func(t *testing.T) {
		t.Parallel()
		a, _ := setup(t)

		assert.NotPanics(t, func() {
			a.handleAccountInfo(&protocol.Packet{
				EMsg:    enums.EMsg_ClientAccountInfo,
				Payload: []byte{0xFF}, // invalid proto
			})
		})
	})
}

func TestAccount_HandleEmailAddrInfo(t *testing.T) {
	t.Parallel()

	t.Run("success", func(t *testing.T) {
		t.Parallel()
		a, ictx := setup(t)

		sub := ictx.Bus().Subscribe(&EmailInfoEvent{})
		defer sub.Unsubscribe()

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
	})

	t.Run("error_unmarshal", func(t *testing.T) {
		t.Parallel()
		a, _ := setup(t)

		assert.NotPanics(t, func() {
			a.handleEmailAddrInfo(&protocol.Packet{
				EMsg:    enums.EMsg_ClientEmailAddrInfo,
				Payload: []byte{0xFF}, // invalid proto
			})
		})
	})
}

func TestAccount_HandleIsLimitedAccount(t *testing.T) {
	t.Parallel()

	t.Run("success", func(t *testing.T) {
		t.Parallel()
		a, ictx := setup(t)

		sub := ictx.Bus().Subscribe(&LimitationsEvent{})
		defer sub.Unsubscribe()

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
	})

	t.Run("error_unmarshal", func(t *testing.T) {
		t.Parallel()
		a, _ := setup(t)

		assert.NotPanics(t, func() {
			a.handleIsLimitedAccount(&protocol.Packet{
				EMsg:    enums.EMsg_ClientIsLimitedAccount,
				Payload: []byte{0xFF}, // invalid proto
			})
		})
	})
}

func TestAccount_HandleVACBanStatus(t *testing.T) {
	t.Parallel()

	t.Run("success", func(t *testing.T) {
		t.Parallel()
		a, ictx := setup(t)

		sub := ictx.Bus().Subscribe(&VACBansEvent{})
		defer sub.Unsubscribe()

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
	})

	t.Run("short_payload", func(t *testing.T) {
		t.Parallel()
		a, _ := setup(t)

		assert.NotPanics(t, func() {
			a.handleVACBanStatus(&protocol.Packet{
				EMsg:    enums.EMsg_ClientVACBanStatus,
				Payload: []byte{1, 2}, // < 4 bytes
			})
		})
	})

	t.Run("inverted_ranges", func(t *testing.T) {
		t.Parallel()
		a, _ := setup(t)

		payload := make([]byte, 16)
		binary.LittleEndian.PutUint32(payload[0:4], 1)
		// rangeStart = 500, rangeEnd = 400 (inverted)
		binary.LittleEndian.PutUint32(payload[4:8], 500)
		binary.LittleEndian.PutUint32(payload[8:12], 400)
		binary.LittleEndian.PutUint32(payload[12:16], 0)

		a.handleVACBanStatus(&protocol.Packet{
			EMsg:    enums.EMsg_ClientVACBanStatus,
			Payload: payload,
		})

		vac := a.GetVACBans()
		assert.Equal(t, uint32(1), vac.NumBans)
		assert.Contains(t, vac.AppIDs, uint32(400))
		assert.Contains(t, vac.AppIDs, uint32(500))
		assert.Equal(t, uint32(400), vac.Ranges[0][0])
		assert.Equal(t, uint32(500), vac.Ranges[0][1])
	})

	t.Run("payload_boundary_break", func(t *testing.T) {
		t.Parallel()
		a, _ := setup(t)

		payload := make([]byte, 8)
		binary.LittleEndian.PutUint32(payload[0:4], 2) // claims 2, but payload only has space for 0.3 bans
		binary.LittleEndian.PutUint32(payload[4:8], 100)

		assert.NotPanics(t, func() {
			a.handleVACBanStatus(&protocol.Packet{
				EMsg:    enums.EMsg_ClientVACBanStatus,
				Payload: payload,
			})
		})
	})
}

func TestAccount_HandleWalletInfoUpdate(t *testing.T) {
	t.Parallel()

	t.Run("success", func(t *testing.T) {
		t.Parallel()
		a, ictx := setup(t)

		sub := ictx.Bus().Subscribe(&WalletInfoEvent{})
		defer sub.Unsubscribe()

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
	})

	t.Run("balance64_fields", func(t *testing.T) {
		t.Parallel()
		a, ictx := setup(t)

		sub := ictx.Bus().Subscribe(&WalletInfoEvent{})
		defer sub.Unsubscribe()

		ictx.EmitPacket(t, enums.EMsg_ClientWalletInfoUpdate, &pb.CMsgClientWalletInfoUpdate{
			HasWallet:        proto.Bool(true),
			Balance:          proto.Int32(100),
			Balance64:        proto.Int64(100000000), // should override Balance
			BalanceDelayed:   proto.Int32(50),
			Balance64Delayed: proto.Int64(50000000), // should override BalanceDelayed
		})

		wallet := a.GetWalletInfo()
		assert.Equal(t, int64(100000000), wallet.Balance)
		assert.Equal(t, int64(50000000), wallet.BalanceDelayed)
	})

	t.Run("error_unmarshal", func(t *testing.T) {
		t.Parallel()
		a, _ := setup(t)

		assert.NotPanics(t, func() {
			a.handleWalletInfoUpdate(&protocol.Packet{
				EMsg:    enums.EMsg_ClientWalletInfoUpdate,
				Payload: []byte{0xFF}, // invalid proto
			})
		})
	})
}

func TestAccount_HandleVanityURLChangedNotification(t *testing.T) {
	t.Parallel()

	t.Run("success", func(t *testing.T) {
		t.Parallel()
		a, ictx := setup(t)

		sub := ictx.Bus().Subscribe(&VanityURLChangedEvent{})
		defer sub.Unsubscribe()

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
	})

	t.Run("error_unmarshal", func(t *testing.T) {
		t.Parallel()
		a, _ := setup(t)

		assert.NotPanics(t, func() {
			a.handleVanityURLChangedNotification(&protocol.Packet{
				EMsg:    enums.EMsg_ClientVanityURLChangedNotification,
				Payload: []byte{0xFF}, // invalid proto
			})
		})
	})
}

func TestAccount_HandleUpdateGuestPassesList(t *testing.T) {
	t.Parallel()

	t.Run("success_complete", func(t *testing.T) {
		t.Parallel()
		a, ictx := setup(t)

		sub := ictx.Bus().Subscribe(&GiftsUpdatedEvent{})
		defer sub.Unsubscribe()

		buf := new(bytes.Buffer)
		_ = binary.Write(buf, binary.LittleEndian, uint32(enums.EResult_OK))
		_ = binary.Write(buf, binary.LittleEndian, uint32(1)) // countToGive
		_ = binary.Write(buf, binary.LittleEndian, uint32(1)) // countToRedeem

		// First BVDF (discarded):
		buf.WriteByte(0) // kvTypeNone
		encodeCString(buf, "gift")
		buf.WriteByte(1) // kvTypeString
		encodeCString(buf, "key")
		encodeCString(buf, "val")
		buf.WriteByte(8) // kvTypeEnd
		buf.WriteByte(8) // kvTypeEnd

		// Second BVDF (redeem) with MessageObject:
		buf.WriteByte(0) // kvTypeNone
		encodeCString(buf, "MessageObject")
		buf.WriteByte(1) // kvTypeString
		encodeCString(buf, "name")
		encodeCString(buf, "my_gift")
		buf.WriteByte(8) // kvTypeEnd
		buf.WriteByte(8) // kvTypeEnd

		a.handleUpdateGuestPassesList(&protocol.Packet{
			EMsg:    enums.EMsg_ClientUpdateGuestPassesList,
			Payload: buf.Bytes(),
		})

		gifts := a.GetGifts()
		require.Len(t, gifts, 1)
		assert.Equal(t, "my_gift", gifts[0]["name"])

		select {
		case ev := <-sub.C():
			e := ev.(*GiftsUpdatedEvent)
			require.Len(t, e.Gifts, 1)
			assert.Equal(t, "my_gift", e.Gifts[0]["name"])
		case <-time.After(100 * time.Millisecond):
			t.Fatal("Event not received")
		}
	})

	t.Run("short_payload", func(t *testing.T) {
		t.Parallel()
		a, _ := setup(t)

		assert.NotPanics(t, func() {
			a.handleUpdateGuestPassesList(&protocol.Packet{
				EMsg:    enums.EMsg_ClientUpdateGuestPassesList,
				Payload: []byte{1, 2, 3}, // < 12 bytes
			})
		})
	})

	t.Run("non_ok_eresult", func(t *testing.T) {
		t.Parallel()
		a, _ := setup(t)

		buf := new(bytes.Buffer)
		_ = binary.Write(buf, binary.LittleEndian, uint32(enums.EResult_Fail))
		_ = binary.Write(buf, binary.LittleEndian, uint32(0))
		_ = binary.Write(buf, binary.LittleEndian, uint32(0))

		assert.NotPanics(t, func() {
			a.handleUpdateGuestPassesList(&protocol.Packet{
				EMsg:    enums.EMsg_ClientUpdateGuestPassesList,
				Payload: buf.Bytes(),
			})
		})
		assert.Empty(t, a.GetGifts())
	})

	t.Run("parse_discard_error", func(t *testing.T) {
		t.Parallel()
		a, _ := setup(t)

		buf := new(bytes.Buffer)
		_ = binary.Write(buf, binary.LittleEndian, uint32(enums.EResult_OK))
		_ = binary.Write(buf, binary.LittleEndian, uint32(1)) // countToGive
		_ = binary.Write(buf, binary.LittleEndian, uint32(0)) // countToRedeem
		buf.Write([]byte{0xFF, 0xFF})                         // invalid BVDF bytes

		assert.NotPanics(t, func() {
			a.handleUpdateGuestPassesList(&protocol.Packet{
				EMsg:    enums.EMsg_ClientUpdateGuestPassesList,
				Payload: buf.Bytes(),
			})
		})
	})

	t.Run("parse_redeem_error", func(t *testing.T) {
		t.Parallel()
		a, _ := setup(t)

		buf := new(bytes.Buffer)
		_ = binary.Write(buf, binary.LittleEndian, uint32(enums.EResult_OK))
		_ = binary.Write(buf, binary.LittleEndian, uint32(0)) // countToGive
		_ = binary.Write(buf, binary.LittleEndian, uint32(1)) // countToRedeem
		buf.Write([]byte{0xFF, 0xFF})                         // invalid BVDF bytes

		assert.NotPanics(t, func() {
			a.handleUpdateGuestPassesList(&protocol.Packet{
				EMsg:    enums.EMsg_ClientUpdateGuestPassesList,
				Payload: buf.Bytes(),
			})
		})
	})
}

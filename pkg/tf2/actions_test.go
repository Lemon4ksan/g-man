// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package tf2

import (
	"bytes"
	"context"
	"encoding/binary"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	pb "github.com/lemon4ksan/g-man/pkg/protobuf/tf2"
)

func TestTF2_Actions(t *testing.T) {
	tf, _, mCoord := setupTF2(t)
	ctx := context.Background()

	tests := []struct {
		name         string
		action       func() error
		expectedMsg  pb.EGCItemMsg
		expectedBody func() []byte
	}{
		{
			name:        "RemoveItemName",
			action:      func() error { return tf.RemoveItemName(ctx, 123) },
			expectedMsg: pb.EGCItemMsg_k_EMsgGCRemoveItemName,
			expectedBody: func() []byte {
				b := make([]byte, 9)
				binary.LittleEndian.PutUint64(b[0:8], 123)
				b[8] = 0 // Name

				return b
			},
		},
		{
			name:        "RemoveItemDescription",
			action:      func() error { return tf.RemoveItemDescription(ctx, 123) },
			expectedMsg: pb.EGCItemMsg_k_EMsgGCRemoveItemName,
			expectedBody: func() []byte {
				b := make([]byte, 9)
				binary.LittleEndian.PutUint64(b[0:8], 123)
				b[8] = 1 // Description

				return b
			},
		},
		{
			name: "NameItem",
			action: func() error {
				return tf.NameItem(ctx, 100, 200, "Cool Weapon")
			},
			expectedMsg: pb.EGCItemMsg_k_EMsgGCNameItem,
			expectedBody: func() []byte {
				buf := new(bytes.Buffer)
				_ = binary.Write(buf, binary.LittleEndian, uint64(100)) // ToolID
				_ = binary.Write(buf, binary.LittleEndian, uint64(200)) // ItemID
				buf.WriteByte(0)                                        // IsDescription = false
				buf.WriteString("Cool Weapon")
				buf.WriteByte(0) // Null terminator

				return buf.Bytes()
			},
		},
		{
			name: "DescribeItem",
			action: func() error {
				return tf.DescribeItem(ctx, 100, 200, "Epic Description")
			},
			expectedMsg: pb.EGCItemMsg_k_EMsgGCNameItem,
			expectedBody: func() []byte {
				buf := new(bytes.Buffer)
				_ = binary.Write(buf, binary.LittleEndian, uint64(100)) // ToolID
				_ = binary.Write(buf, binary.LittleEndian, uint64(200)) // ItemID
				buf.WriteByte(1)                                        // IsDescription = true
				buf.WriteString("Epic Description")
				buf.WriteByte(0) // Null terminator

				return buf.Bytes()
			},
		},
		{
			name:        "AcknowledgeItem",
			action:      func() error { return tf.AcknowledgeItem(ctx, 123) },
			expectedMsg: pb.EGCItemMsg_k_EMsgGCSetSingleItemPosition,
			expectedBody: func() []byte {
				b := make([]byte, 12)
				binary.LittleEndian.PutUint64(b[0:8], 123)
				binary.LittleEndian.PutUint32(b[8:12], 1) // Default pos for ack

				return b
			},
		},
		{
			name:        "SetItemStyle",
			action:      func() error { return tf.SetItemStyle(ctx, 123, 1) },
			expectedMsg: pb.EGCItemMsg_k_EMsgGCSetItemStyle,
			expectedBody: func() []byte {
				buf := new(bytes.Buffer)
				_ = binary.Write(buf, binary.LittleEndian, uint64(123))
				_ = binary.Write(buf, binary.LittleEndian, uint8(1)) // SDK: uint8 m_iStyle

				return buf.Bytes()
			},
		},
		{
			name:        "SetItemPosition",
			action:      func() error { return tf.SetItemPosition(ctx, 123, 42) },
			expectedMsg: pb.EGCItemMsg_k_EMsgGCSetSingleItemPosition,
			expectedBody: func() []byte {
				buf := new(bytes.Buffer)
				_ = binary.Write(buf, binary.LittleEndian, uint64(123))
				_ = binary.Write(buf, binary.LittleEndian, uint32(42))

				return buf.Bytes()
			},
		},
		{
			name:        "DeleteItem",
			action:      func() error { return tf.DeleteItem(ctx, 123) },
			expectedMsg: pb.EGCItemMsg_k_EMsgGCDelete,
			expectedBody: func() []byte {
				buf := new(bytes.Buffer)
				_ = binary.Write(buf, binary.LittleEndian, uint64(123))
				return buf.Bytes()
			},
		},
		{
			name:        "UnlockCrate",
			action:      func() error { return tf.UnlockCrate(ctx, 100, 200) },
			expectedMsg: pb.EGCItemMsg_k_EMsgGCUnlockCrate,
			expectedBody: func() []byte {
				buf := new(bytes.Buffer)
				_ = binary.Write(buf, binary.LittleEndian, uint64(100))
				_ = binary.Write(buf, binary.LittleEndian, uint64(200))

				return buf.Bytes()
			},
		},
		{
			name:        "WrapItem",
			action:      func() error { return tf.WrapItem(ctx, 100, 200) },
			expectedMsg: pb.EGCItemMsg_k_EMsgGCGiftWrapItem,
			expectedBody: func() []byte {
				buf := new(bytes.Buffer)
				_ = binary.Write(buf, binary.LittleEndian, uint64(100))
				_ = binary.Write(buf, binary.LittleEndian, uint64(200))

				return buf.Bytes()
			},
		},
		{
			name:        "DeliverGift",
			action:      func() error { return tf.DeliverGift(ctx, 100, 76561198000000000) },
			expectedMsg: pb.EGCItemMsg_k_EMsgGCDeliverGift,
			expectedBody: func() []byte {
				buf := new(bytes.Buffer)
				_ = binary.Write(buf, binary.LittleEndian, uint64(100))
				_ = binary.Write(buf, binary.LittleEndian, uint64(76561198000000000))

				return buf.Bytes()
			},
		},
		{
			name:        "InviteToTrade",
			action:      func() error { return tf.InviteToTrade(ctx, 76561198000000000) },
			expectedMsg: pb.EGCItemMsg_k_EMsgGCTrading_InitiateTradeRequest,
			expectedBody: func() []byte {
				buf := new(bytes.Buffer)
				_ = binary.Write(buf, binary.LittleEndian, uint32(0))
				_ = binary.Write(buf, binary.LittleEndian, uint64(76561198000000000))

				return buf.Bytes()
			},
		},
		{
			name:        "RespondToTrade_Accept",
			action:      func() error { return tf.RespondToTrade(ctx, 42, true) },
			expectedMsg: pb.EGCItemMsg_k_EMsgGCTrading_InitiateTradeResponse,
			expectedBody: func() []byte {
				buf := new(bytes.Buffer)
				_ = binary.Write(buf, binary.LittleEndian, uint32(0)) // Accept
				_ = binary.Write(buf, binary.LittleEndian, uint32(42))

				return buf.Bytes()
			},
		},
		{
			name:        "ApplyPaint",
			action:      func() error { return tf.ApplyPaint(ctx, 100, 200) },
			expectedMsg: pb.EGCItemMsg_k_EMsgGCPaintItem,
			expectedBody: func() []byte {
				buf := new(bytes.Buffer)
				_ = binary.Write(buf, binary.LittleEndian, uint64(100))
				_ = binary.Write(buf, binary.LittleEndian, uint64(200))

				return buf.Bytes()
			},
		},
		{
			name:        "UnwrapGift",
			action:      func() error { return tf.UnwrapGift(ctx, 123) },
			expectedMsg: pb.EGCItemMsg_k_EMsgGCUnwrapGiftRequest,
			expectedBody: func() []byte {
				b := make([]byte, 8)
				binary.LittleEndian.PutUint64(b, 123)
				return b
			},
		},
		{
			name:        "RespondToTrade_Decline",
			action:      func() error { return tf.RespondToTrade(ctx, 42, false) },
			expectedMsg: pb.EGCItemMsg_k_EMsgGCTrading_InitiateTradeResponse,
			expectedBody: func() []byte {
				buf := new(bytes.Buffer)
				_ = binary.Write(buf, binary.LittleEndian, uint32(1)) // Decline
				_ = binary.Write(buf, binary.LittleEndian, uint32(42))

				return buf.Bytes()
			},
		},
		{
			name:        "CancelTradeRequest",
			action:      func() error { return tf.CancelTradeRequest(ctx) },
			expectedMsg: pb.EGCItemMsg_k_EMsgGCTrading_CancelSession,
			expectedBody: func() []byte {
				return nil
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.action()
			require.NoError(t, err)

			assert.Equal(t, uint32(tt.expectedMsg), mCoord.lastSendMsgType)

			if tt.expectedBody != nil {
				assert.Equal(t, tt.expectedBody(), mCoord.lastSendPayload)
			}
		})
	}
}

func TestTF2_ProtoActions(t *testing.T) {
	tf, _, mCoord := setupTF2(t)
	ctx := context.Background()

	t.Run("UseItem", func(t *testing.T) {
		err := tf.UseItem(ctx, 123)
		require.NoError(t, err)
		assert.Equal(t, uint32(pb.EGCItemMsg_k_EMsgGCUseItemRequest), mCoord.lastSendMsgType)
	})

	t.Run("SortBackpack", func(t *testing.T) {
		err := tf.SortBackpack(ctx, 1)
		require.NoError(t, err)
		assert.Equal(t, uint32(pb.EGCItemMsg_k_EMsgGCSortItems), mCoord.lastSendMsgType)
	})

	t.Run("EquipItem", func(t *testing.T) {
		err := tf.EquipItem(ctx, 123, 2, 3)
		require.NoError(t, err)
		assert.Equal(t, uint32(pb.EGCItemMsg_k_EMsgGCAdjustItemEquippedState), mCoord.lastSendMsgType)
	})

	t.Run("MoveItems", func(t *testing.T) {
		items := []ItemPos{{Id: 1, Position: 2}, {Id: 3, Position: 4}}
		err := tf.MoveItems(ctx, items)
		require.NoError(t, err)
		assert.Equal(t, uint32(pb.EGCItemMsg_k_EMsgGCSetItemPositions), mCoord.lastSendMsgType)
	})

	t.Run("RemoveItemPaint", func(t *testing.T) {
		err := tf.RemoveItemPaint(ctx, 123)
		require.NoError(t, err)
		assert.Equal(t, uint32(pb.EGCItemMsg_k_EMsgGCRemoveItemPaint), mCoord.lastSendMsgType)
	})

	t.Run("RemoveMakersMark", func(t *testing.T) {
		err := tf.RemoveMakersMark(ctx, 123)
		require.NoError(t, err)
		assert.Equal(t, uint32(pb.EGCItemMsg_k_EMsgGCRemoveMakersMark), mCoord.lastSendMsgType)
	})

	t.Run("ResetStrangeScores", func(t *testing.T) {
		err := tf.ResetStrangeScores(ctx, 123)
		require.NoError(t, err)
		assert.Equal(t, uint32(pb.EGCItemMsg_k_EMsgGCResetStrangeScores), mCoord.lastSendMsgType)
	})

	t.Run("RemoveKillstreak", func(t *testing.T) {
		err := tf.RemoveKillstreak(ctx, 123)
		require.NoError(t, err)
		assert.Equal(t, uint32(pb.EGCItemMsg_k_EMsgGCRemoveKillStreak), mCoord.lastSendMsgType)
	})

	t.Run("RemoveFestivizer", func(t *testing.T) {
		err := tf.RemoveFestivizer(ctx, 123)
		require.NoError(t, err)
		assert.Equal(t, uint32(pb.EGCItemMsg_k_EMsgGCRemoveFestivizer), mCoord.lastSendMsgType)
	})

	t.Run("RemoveGiftedBy", func(t *testing.T) {
		err := tf.RemoveGiftedBy(ctx, 123)
		require.NoError(t, err)
		assert.Equal(t, uint32(pb.EGCItemMsg_k_EMsgGCRemoveGiftedBy), mCoord.lastSendMsgType)
	})

	t.Run("ShuffleCrate", func(t *testing.T) {
		err := tf.ShuffleCrate(ctx, 123, "CODE")
		require.NoError(t, err)
		assert.Equal(t, uint32(pb.EGCItemMsg_k_EMsgGCShuffleCrateContents), mCoord.lastSendMsgType)
	})

	t.Run("ApplyAutograph", func(t *testing.T) {
		err := tf.ApplyAutograph(ctx, 1, 2)
		require.NoError(t, err)
		assert.Equal(t, uint32(pb.EGCItemMsg_k_EMsgGCApplyAutograph), mCoord.lastSendMsgType)
	})

	t.Run("RequestMarketData", func(t *testing.T) {
		err := tf.RequestMarketData(ctx, 1) // USD
		require.NoError(t, err)
		assert.Equal(t, uint32(pb.EGCItemMsg_k_EMsgGCClientRequestMarketData), mCoord.lastSendMsgType)
	})

	t.Run("RequestFriends", func(t *testing.T) {
		err := tf.RequestFriends(ctx, []uint32{1, 2, 3})
		require.NoError(t, err)
		assert.Equal(t, uint32(pb.ETFGCMsg_k_EMsgGCRequestTF2Friends), mCoord.lastSendMsgType)
	})

	t.Run("ApplyStrangePart", func(t *testing.T) {
		err := tf.ApplyStrangePart(ctx, 1, 2)
		require.NoError(t, err)
		assert.Equal(t, uint32(pb.EGCItemMsg_k_EMsgGCApplyStrangePart), mCoord.lastSendMsgType)
	})

	t.Run("ApplyStrangifier", func(t *testing.T) {
		err := tf.ApplyStrangifier(ctx, 1, 2)
		require.NoError(t, err)
		assert.Equal(t, uint32(pb.EGCItemMsg_k_EMsgGCApplyXifier), mCoord.lastSendMsgType)
	})
}

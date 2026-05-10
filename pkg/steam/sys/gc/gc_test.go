// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package gc

import (
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"

	pb "github.com/lemon4ksan/g-man/pkg/protobuf/steam"
	"github.com/lemon4ksan/g-man/pkg/steam/protocol"
	"github.com/lemon4ksan/g-man/pkg/steam/protocol/enums"
	"github.com/lemon4ksan/g-man/test/module"
)

const (
	AppidTf2 uint32 = 440
)

func setupCoordinator(t *testing.T) (*Coordinator, *module.InitContext) {
	t.Helper()

	c := New()
	ictx := module.NewInitContext()

	require.NoError(t, c.Init(ictx))

	t.Cleanup(func() {
		_ = c.Close()
	})

	return c, ictx
}

func emitGC(t *testing.T, ictx *module.InitContext, appID, msgType uint32, payload []byte, jobID uint64) {
	t.Helper()

	inner := &protocol.GCPacket{
		AppID:       appID,
		MsgType:     msgType,
		TargetJobID: jobID,
		Payload:     payload,
	}

	gcData, err := inner.Serialize()
	require.NoError(t, err)

	ictx.EmitPacket(t, enums.EMsg_ClientFromGC, &pb.CMsgGCClient{
		Appid:   proto.Uint32(appID),
		Msgtype: proto.Uint32(msgType),
		Payload: gcData,
	})
}

func TestCoordinator_InitAndClose(t *testing.T) {
	c, ictx := setupCoordinator(t)
	ictx.AssertPacketHandlerRegistered(t, enums.EMsg_ClientFromGC)

	err := c.Close()
	require.NoError(t, err)
	ictx.AssertPacketHandlerUnregistered(t, enums.EMsg_ClientFromGC)
	assert.Nil(t, c.unregFuncs)
}

func TestCoordinator_SendMethods(t *testing.T) {
	c, _ := setupCoordinator(t)
	ctx := t.Context()

	t.Run("Send (Proto)", func(t *testing.T) {
		err := c.Send(ctx, AppidTf2, 1001, &pb.CMsgGCClient{})
		assert.NoError(t, err)
	})

	t.Run("SendRaw", func(t *testing.T) {
		err := c.SendRaw(ctx, AppidTf2, 1002, []byte("raw"))
		assert.NoError(t, err)
	})
}

func TestCoordinator_CallAndResolve(t *testing.T) {
	c, ictx := setupCoordinator(t)
	ctx := t.Context()

	t.Run("Call Missing Callback", func(t *testing.T) {
		err := c.Call(ctx, AppidTf2, 1001, nil, nil)
		assert.ErrorContains(t, err, "callback is required")
	})

	t.Run("Call and Resolve Success", func(t *testing.T) {
		resolved := make(chan struct{})
		err := c.Call(ctx, AppidTf2, 1001, &pb.CMsgGCClient{}, func(p *protocol.GCPacket, err error) {
			assert.NoError(t, err)
			assert.Equal(t, []byte("pong"), p.Payload)
			close(resolved)
		})
		require.NoError(t, err)

		jobID := c.jobManager.NextID() - 1
		emitGC(t, ictx, AppidTf2, 1002, []byte("pong"), jobID)

		select {
		case <-resolved:
		case <-time.After(500 * time.Millisecond):
			t.Fatal("timeout waiting for job resolution")
		}
	})

	t.Run("CallRaw Success", func(t *testing.T) {
		err := c.CallRaw(ctx, AppidTf2, 1001, nil, func(p *protocol.GCPacket, err error) {})
		assert.NoError(t, err)
	})
}

func TestCoordinator_Routing(t *testing.T) {
	_, ictx := setupCoordinator(t)
	sub := ictx.Bus().Subscribe(&MessageEvent{})

	t.Run("Fallthrough to Bus (No JobID)", func(t *testing.T) {
		emitGC(t, ictx, AppidTf2, 5001, []byte("data"), protocol.NoJob)

		select {
		case ev := <-sub.C():
			assert.Equal(t, uint32(5001), ev.(*MessageEvent).Packet.MsgType)
		case <-time.After(100 * time.Millisecond):
			t.Fatal("event not on bus")
		}
	})

	t.Run("Fallthrough to Bus (Unrecognized JobID)", func(t *testing.T) {
		// TargetJobID set to 12345, which we haven't registered
		emitGC(t, ictx, AppidTf2, 5002, []byte("data"), 12345)

		select {
		case ev := <-sub.C():
			assert.Equal(t, uint32(5002), ev.(*MessageEvent).Packet.MsgType)
		case <-time.After(100 * time.Millisecond):
			t.Fatal("event should have fallen back to bus for unknown job")
		}
	})
}

func TestCoordinator_Errors(t *testing.T) {
	c, ictx := setupCoordinator(t)
	ctx := t.Context()

	t.Run("Parse GCPacket Failure", func(t *testing.T) {
		ictx.EmitPacket(t, enums.EMsg_ClientFromGC, &pb.CMsgGCClient{
			Appid:   proto.Uint32(440),
			Payload: []byte{0x00}, // Too short for GC header
		})
		// Should log and return gracefully
	})

	t.Run("Transport Send Error Resolves Job", func(t *testing.T) {
		ictx.MockServiceAccessor().ResponseErrs[enums.EMsg_ClientToGC.String()] = errors.New("io timeout")

		resolved := make(chan struct{})
		err := c.Call(ctx, AppidTf2, 1001, nil, func(p *protocol.GCPacket, err error) {
			assert.Error(t, err)
			assert.Contains(t, err.Error(), "io timeout")
			close(resolved)
		})

		assert.Error(t, err)

		select {
		case <-resolved:
		case <-time.After(100 * time.Millisecond):
			t.Fatal("callback not resolved on transport error")
		}
	})

	t.Run("Job Manager Full", func(t *testing.T) {
		// Fill manager manually to hit error branch in .Add
		for i := range 2000 {
			_ = c.jobManager.Add(uint64(i+10), func(p *protocol.GCPacket, err error) {})
		}

		err := c.Call(ctx, AppidTf2, 1001, nil, func(p *protocol.GCPacket, err error) {})
		assert.ErrorContains(t, err, "gc job track")
	})
}

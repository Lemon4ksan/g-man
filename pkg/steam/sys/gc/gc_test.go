// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package gc

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"

	pb "github.com/lemon4ksan/g-man/pkg/protobuf/steam"
	"github.com/lemon4ksan/g-man/pkg/steam/protocol"
	"github.com/lemon4ksan/g-man/pkg/steam/protocol/enums"
	"github.com/lemon4ksan/g-man/test/mock"
)

const (
	AppidTF2 uint32 = 440
)

func setupCoordinator(t *testing.T) (*Coordinator, *mock.InitContext) {
	t.Helper()

	c := New()
	ictx := mock.NewInitContext()

	require.NoError(t, c.Init(ictx))

	t.Cleanup(func() {
		_ = c.Close()
	})

	return c, ictx
}

func emitGC(t *testing.T, ictx *mock.InitContext, appID, msgType uint32, payload []byte, jobID uint64) {
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

func TestFrom_NilClient_ReturnsNil(t *testing.T) {
	t.Parallel()

	assert.Nil(t, From(nil))
}

func TestWithModule_ValidOption_ReturnsNonNil(t *testing.T) {
	t.Parallel()

	opt := WithModule()
	assert.NotNil(t, opt)
}

func TestInit_SuccessLifecycle_RegistersAndUnregistersEMsg(t *testing.T) {
	t.Parallel()

	t.Run("success_lifecycle", func(t *testing.T) {
		t.Parallel()
		c, ictx := setupCoordinator(t)
		assert.Equal(t, ModuleName, c.Name())
		ictx.AssertPacketHandlerRegistered(t, enums.EMsg_ClientFromGC)

		err := c.Close()
		require.NoError(t, err)
		ictx.AssertPacketHandlerUnregistered(t, enums.EMsg_ClientFromGC)
		assert.Nil(t, c.unregFuncs)
	})
}

func TestSend_VariousPayloads_SendsCorrectly(t *testing.T) {
	t.Parallel()
	ctx := t.Context()

	t.Run("send_proto", func(t *testing.T) {
		t.Parallel()
		c, _ := setupCoordinator(t)
		err := c.Send(ctx, AppidTF2, 1001, &pb.CMsgGCClient{})
		assert.NoError(t, err)
	})

	t.Run("send_raw", func(t *testing.T) {
		t.Parallel()
		c, _ := setupCoordinator(t)
		err := c.SendRaw(ctx, AppidTF2, 1002, []byte("raw"))
		assert.NoError(t, err)
	})

	t.Run("send_large_payload_bypasses_pool", func(t *testing.T) {
		t.Parallel()
		c, _ := setupCoordinator(t)

		largePayload := make([]byte, 70000)
		err := c.Send(ctx, AppidTF2, 1001, &pb.CMsgGCClient{
			Payload: largePayload,
		})
		assert.NoError(t, err)
	})
}

func TestCall_VariousCallbacks_ResolvesOrReturnsError(t *testing.T) {
	t.Parallel()
	ctx := t.Context()

	t.Run("call_missing_callback", func(t *testing.T) {
		t.Parallel()
		c, _ := setupCoordinator(t)
		err := c.Call(ctx, AppidTF2, 1001, nil, nil)
		assert.ErrorContains(t, err, "callback is required")
	})

	t.Run("call_raw_missing_callback", func(t *testing.T) {
		t.Parallel()
		c, _ := setupCoordinator(t)
		err := c.CallRaw(ctx, AppidTF2, 1001, nil, nil)
		assert.ErrorContains(t, err, "callback is required")
	})

	t.Run("call_and_resolve_success", func(t *testing.T) {
		t.Parallel()
		c, ictx := setupCoordinator(t)

		resolved := make(chan struct{})
		err := c.Call(
			ctx,
			AppidTF2,
			1001,
			&pb.CMsgGCClient{},
			func(_ context.Context, p *protocol.GCPacket, err error) {
				assert.NoError(t, err)
				assert.Equal(t, []byte("pong"), p.Payload)
				close(resolved)
			},
		)
		require.NoError(t, err)

		jobID := c.jobManager.NextID() - 1
		emitGC(t, ictx, AppidTF2, 1002, []byte("pong"), jobID)

		waitCtx, cancel := context.WithTimeout(t.Context(), 500*time.Millisecond)
		t.Cleanup(cancel)

		select {
		case <-resolved:
		case <-waitCtx.Done():
			t.Fatal("timeout waiting for job resolution")
		}
	})

	t.Run("call_raw_success", func(t *testing.T) {
		t.Parallel()
		c, _ := setupCoordinator(t)
		err := c.CallRaw(ctx, AppidTF2, 1001, nil, func(_ context.Context, p *protocol.GCPacket, err error) {})
		assert.NoError(t, err)
	})
}

func TestHandleClientFromGC_VariousJobIDs_RoutesToBusOrHandler(t *testing.T) {
	t.Parallel()

	t.Run("fallthrough_to_bus_no_job_id", func(t *testing.T) {
		t.Parallel()
		_, ictx := setupCoordinator(t)

		sub := ictx.Bus().Subscribe(&MessageEvent{})
		defer sub.Unsubscribe()

		emitGC(t, ictx, AppidTF2, 5001, []byte("data"), protocol.NoJob)

		waitCtx, cancel := context.WithTimeout(t.Context(), 500*time.Millisecond)
		t.Cleanup(cancel)

		select {
		case ev := <-sub.C():
			assert.Equal(t, uint32(5001), ev.(*MessageEvent).Packet.MsgType)
		case <-waitCtx.Done():
			t.Fatal("event not on bus")
		}
	})

	t.Run("fallthrough_to_bus_unrecognized_job_id", func(t *testing.T) {
		t.Parallel()
		_, ictx := setupCoordinator(t)

		sub := ictx.Bus().Subscribe(&MessageEvent{})
		defer sub.Unsubscribe()

		emitGC(t, ictx, AppidTF2, 5002, []byte("data"), 12345)

		waitCtx, cancel := context.WithTimeout(t.Context(), 500*time.Millisecond)
		t.Cleanup(cancel)

		select {
		case ev := <-sub.C():
			assert.Equal(t, uint32(5002), ev.(*MessageEvent).Packet.MsgType)
		case <-waitCtx.Done():
			t.Fatal("event should have fallen back to bus for unknown job")
		}
	})
}

func TestHandleClientFromGC_ErrorConditions_HandlesGracefully(t *testing.T) {
	t.Parallel()
	ctx := t.Context()

	t.Run("parse_gc_packet_failure", func(t *testing.T) {
		t.Parallel()
		_, ictx := setupCoordinator(t)

		ictx.EmitPacket(t, enums.EMsg_ClientFromGC, &pb.CMsgGCClient{
			Appid:   proto.Uint32(440),
			Payload: []byte{0x00},
		})
	})

	t.Run("transport_send_error_resolves_job", func(t *testing.T) {
		t.Parallel()
		c, ictx := setupCoordinator(t)

		ictx.MockService().ResponseErrs[enums.EMsg_ClientToGC.String()] = errors.New("io timeout")

		resolved := make(chan struct{})
		err := c.Call(ctx, AppidTF2, 1001, nil, func(_ context.Context, p *protocol.GCPacket, err error) {
			assert.Error(t, err)
			assert.Contains(t, err.Error(), "io timeout")
			close(resolved)
		})

		assert.Error(t, err)

		waitCtx, cancel := context.WithTimeout(t.Context(), 500*time.Millisecond)
		t.Cleanup(cancel)

		select {
		case <-resolved:
		case <-waitCtx.Done():
			t.Fatal("callback not resolved on transport error")
		}
	})

	t.Run("job_manager_full", func(t *testing.T) {
		t.Parallel()
		c, _ := setupCoordinator(t)

		for i := range 2000 {
			_ = c.jobManager.Add(uint64(i+10), func(_ context.Context, p *protocol.GCPacket, err error) {})
		}

		err := c.Call(ctx, AppidTF2, 1001, nil, func(_ context.Context, p *protocol.GCPacket, err error) {})
		assert.ErrorContains(t, err, "gc job track")
	})

	t.Run("envelope_unmarshal_error", func(t *testing.T) {
		t.Parallel()
		c, _ := setupCoordinator(t)

		assert.NotPanics(t, func() {
			c.handleClientFromGC(&protocol.Packet{
				EMsg:    enums.EMsg_ClientFromGC,
				Payload: []byte{0xFF, 0xFF},
			})
		})
	})
}

func TestRegisterGCHandler_CustomHandler_RoutesSuccessfully(t *testing.T) {
	t.Parallel()

	t.Run("specific_handler_routed", func(t *testing.T) {
		t.Parallel()
		c, ictx := setupCoordinator(t)
		called := make(chan *protocol.GCPacket, 1)

		c.RegisterGCHandler(AppidTF2, 6001, func(packet *protocol.GCPacket) {
			called <- packet
		})

		sub := ictx.Bus().Subscribe(&MessageEvent{})
		defer sub.Unsubscribe()

		emitGC(t, ictx, AppidTF2, 6001, []byte("specific-data"), protocol.NoJob)

		waitCtx, cancel := context.WithTimeout(t.Context(), 500*time.Millisecond)
		t.Cleanup(cancel)

		select {
		case p := <-called:
			assert.Equal(t, []byte("specific-data"), p.Payload)
		case <-waitCtx.Done():
			t.Fatal("specific handler was not called")
		}

		negCtx, negCancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
		t.Cleanup(negCancel)

		select {
		case ev := <-sub.C():
			t.Fatalf("bus received message that should have been routed to specific handler: %v", ev)
		case <-negCtx.Done():
			// Success, didn't reach the bus before timeout
		}
	})

	t.Run("unregistered_fallback_to_bus", func(t *testing.T) {
		t.Parallel()
		c, ictx := setupCoordinator(t)
		c.UnregisterGCHandler(AppidTF2, 6001)

		sub := ictx.Bus().Subscribe(&MessageEvent{})
		defer sub.Unsubscribe()

		emitGC(t, ictx, AppidTF2, 6001, []byte("fallback-data"), protocol.NoJob)

		waitCtx, cancel := context.WithTimeout(t.Context(), 500*time.Millisecond)
		t.Cleanup(cancel)

		select {
		case ev := <-sub.C():
			assert.Equal(t, uint32(6001), ev.(*MessageEvent).Packet.MsgType)
			assert.Equal(t, []byte("fallback-data"), ev.(*MessageEvent).Packet.Payload)
		case <-waitCtx.Done():
			t.Fatal("event should have fallen back to bus after unregistering handler")
		}
	})
}

func TestRegisterGCHandler_NilMap_InitializesAndRegisters(t *testing.T) {
	t.Parallel()

	c := &Coordinator{}
	handler := func(packet *protocol.GCPacket) {}

	assert.NotPanics(t, func() {
		c.RegisterGCHandler(AppidTF2, 1001, handler)
	})

	c.handlersMu.RLock()
	assert.NotNil(t, c.gcHandlers)
	assert.NotNil(t, c.gcHandlers[AppidTF2])
	assert.NotNil(t, c.gcHandlers[AppidTF2][1001])
	c.handlersMu.RUnlock()
}

func TestUnregisterGCHandler_NilMap_DoesNotPanic(t *testing.T) {
	t.Parallel()

	c := &Coordinator{}
	assert.NotPanics(t, func() {
		c.UnregisterGCHandler(AppidTF2, 1001)
	})
}

func TestUnregisterGCHandler_NilAppMap_DoesNotPanic(t *testing.T) {
	t.Parallel()

	c := New()
	assert.NotPanics(t, func() {
		c.UnregisterGCHandler(AppidTF2, 1001)
	})
}

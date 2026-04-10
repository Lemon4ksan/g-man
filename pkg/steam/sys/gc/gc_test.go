// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package gc

import (
	"reflect"
	"testing"
	"time"

	pb "github.com/lemon4ksan/g-man/pkg/protobuf/steam"
	"github.com/lemon4ksan/g-man/pkg/steam/protocol"
	"github.com/lemon4ksan/g-man/test/module"
	"google.golang.org/protobuf/proto"
)

const (
	AppID_TF2 uint32 = 440
	AppID_CS2 uint32 = 730
)

func setupCoordinator(t *testing.T) (*Coordinator, *module.InitContext) {
	t.Helper()
	c := New()
	ictx := module.NewInitContext()

	if err := c.Init(ictx); err != nil {
		t.Fatalf("failed to init coordinator: %v", err)
	}

	t.Cleanup(func() {
		_ = c.Close()
	})

	return c, ictx
}

func emitGC(t *testing.T, ictx *module.InitContext, appID uint32, msgType uint32, payload []byte, jobID uint64) {
	t.Helper()
	inner := &protocol.GCPacket{
		AppID:       appID,
		MsgType:     msgType,
		TargetJobID: jobID,
		Payload:     payload,
	}
	gcData, err := inner.Serialize()
	if err != nil {
		t.Fatalf("failed to serialize GC packet: %v", err)
	}

	ictx.EmitPacket(t, protocol.EMsg_ClientFromGC, &pb.CMsgGCClient{
		Appid:   proto.Uint32(appID),
		Msgtype: proto.Uint32(msgType),
		Payload: gcData,
	})
}

func TestCoordinator_InitAndClose(t *testing.T) {
	c := New()
	ictx := module.NewInitContext()

	t.Run("Name", func(t *testing.T) {
		if c.Name() != ModuleName {
			t.Errorf("expected %s, got %s", ModuleName, c.Name())
		}
	})

	t.Run("Registration", func(t *testing.T) {
		_ = c.Init(ictx)
		if _, ok := ictx.GetPacketHandler(protocol.EMsg_ClientFromGC); !ok {
			t.Error("EMsg_ClientFromGC handler not registered")
		}
	})

	t.Run("Cleanup", func(t *testing.T) {
		_ = c.Close()
		if _, ok := ictx.GetPacketHandler(protocol.EMsg_ClientFromGC); ok {
			t.Error("handler should be unregistered after Close")
		}
	})
}

func TestCoordinator_SendRaw(t *testing.T) {
	c, ictx := setupCoordinator(t)
	msgType := uint32(1005)
	payload := []byte{0xDE, 0xAD, 0xBE, 0xEF}

	err := c.SendRaw(t.Context(), AppID_TF2, msgType, payload)
	if err != nil {
		t.Fatalf("SendRaw failed: %v", err)
	}

	req := &pb.CMsgGCClient{}
	ictx.MockService().GetLastCall(req)

	if req.GetAppid() != AppID_TF2 {
		t.Errorf("expected AppID %d, got %d", AppID_TF2, req.GetAppid())
	}

	expectedMsgType := msgType | protocol.ProtoMask
	if req.GetMsgtype() != expectedMsgType {
		t.Errorf("expected msg type %d, got %d", expectedMsgType, req.GetMsgtype())
	}
}

func TestCoordinator_Call(t *testing.T) {
	c, ictx := setupCoordinator(t)

	appID := AppID_CS2
	msgType := uint32(4004)
	replyType := uint32(4005)

	resultChan := make(chan *protocol.GCPacket, 1)

	err := c.Call(t.Context(), appID, msgType, &pb.CMsgGCClient{}, func(p *protocol.GCPacket, err error) {
		if err != nil {
			t.Errorf("callback error: %v", err)
		}
		resultChan <- p
	})
	if err != nil {
		t.Fatalf("Call failed: %v", err)
	}

	if c.jobManager.Count() != 1 {
		t.Errorf("expected 1 active job, got %d", c.jobManager.Count())
	}

	jobID := uint64(1)
	emitGC(t, ictx, appID, replyType, []byte("response"), jobID)

	select {
	case p := <-resultChan:
		if p.MsgType != replyType || string(p.Payload) != "response" {
			t.Errorf("unexpected response packet: %+v", p)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("timed out waiting for job callback")
	}

	if c.jobManager.Count() != 0 {
		t.Error("job should be removed after resolution")
	}
}

func TestCoordinator_Routing(t *testing.T) {
	c, ictx := setupCoordinator(t)
	sub := ictx.Bus().Subscribe(&GCMessageEvent{})

	t.Run("Route to Bus", func(t *testing.T) {
		msgType := uint32(2002)
		payload := []byte{0x01, 0x02}

		emitGC(t, ictx, AppID_TF2, msgType, payload, protocol.NoJob)

		select {
		case ev := <-sub.C():
			gcEv := ev.(*GCMessageEvent)
			if gcEv.Packet.MsgType != msgType || !reflect.DeepEqual(gcEv.Packet.Payload, payload) {
				t.Errorf("invalid event data: %+v", gcEv)
			}
		case <-time.After(500 * time.Millisecond):
			t.Fatal("GCMessageEvent not received")
		}
	})

	t.Run("Route to Job Only", func(t *testing.T) {
		jobID := uint64(999)
		hit := make(chan bool, 1)

		_ = c.jobManager.Add(jobID, func(p *protocol.GCPacket, err error) {
			hit <- true
		})

		emitGC(t, ictx, AppID_TF2, 3003, nil, jobID)

		select {
		case <-hit:
			// OK
		case <-time.After(500 * time.Millisecond):
			t.Fatal("job callback was not executed")
		}

		select {
		case ev := <-sub.C():
			t.Errorf("packet was routed to Bus, but should have been captured by Job: %+v", ev)
		case <-time.After(50 * time.Millisecond):
			// OK
		}
	})
}

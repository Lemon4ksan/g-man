// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package tf2

import (
	"context"
	"sync/atomic"
	"time"

	"github.com/lemon4ksan/g-man/pkg/jobs"
	"github.com/lemon4ksan/g-man/pkg/log"
	"github.com/lemon4ksan/g-man/pkg/modules"
	"github.com/lemon4ksan/g-man/pkg/modules/coordinator"
	gc "github.com/lemon4ksan/g-man/pkg/modules/coordinator/protocol"
	"github.com/lemon4ksan/g-man/pkg/steam"
	"github.com/lemon4ksan/g-man/pkg/steam/bus"

	pb "github.com/lemon4ksan/g-man/pkg/tf2/protobuf"
	"google.golang.org/protobuf/proto"
)

const (
	AppID             = 440
	ModuleName string = "tf2"
)

// TF2State reflects the GC session status.
type TF2State int32

const (
	GCDisconnected TF2State = iota
	GCConnecting
	GCConnected
)

// CoordinatorProvider defines what TF2 needs from the generic GC module.
type CoordinatorProvider interface {
	Send(ctx context.Context, appID uint32, msgType uint32, msg proto.Message) error
	SendRaw(ctx context.Context, appID uint32, msgType uint32, payload []byte) error
	Call(ctx context.Context, appID uint32, msgType uint32, msg proto.Message, cb jobs.Callback[*gc.Packet]) error
	CallRaw(ctx context.Context, appID uint32, msgType uint32, payload []byte, cb jobs.Callback[*gc.Packet]) error
}

type AppsProvider interface {
	PlayGames(ctx context.Context, appIDs []uint32, forceKick bool) error
}

func WithModule() steam.Option {
    return func(c *steam.Client) {
        c.RegisterModule(New())
    }
}

type TF2 struct {
	modules.BaseModule

	gc   CoordinatorProvider
	apps AppsProvider

	state atomic.Int32
	cache *SOCache
}

func New() *TF2 {
	return &TF2{
		BaseModule: modules.NewBase(ModuleName),
	}
}

func (t *TF2) Name() string { return ModuleName }

func (t *TF2) Init(init modules.InitContext) error {
	if err := t.BaseModule.Init(init); err != nil {
		return err
	}

	t.gc = init.Module("gc").(CoordinatorProvider)
	t.apps = init.Module("apps").(AppsProvider)
	t.cache = NewSOCache(t.gc)

	sub := t.Bus.Subscribe(&coordinator.GCMessageEvent{})
	t.Go(func(ctx context.Context) {
		t.messageLoop(ctx, sub)
	})

	return nil
}

// StartAuthed occurs when Steam logs in.
// We need to "start" TF2 so that GC can start talking to us.
func (t *TF2) StartAuthed(ctx context.Context, authCtx modules.AuthContext) error {
	if err := t.apps.PlayGames(ctx, []uint32{AppID}, false); err != nil {
		return err
	}

	t.state.Store(int32(GCConnecting))
	t.Go(func(ctx context.Context) {
		t.helloLoop(ctx)
	})

	return nil
}

func (t *TF2) Close() error {
	t.state.Store(int32(GCDisconnected))
	return t.BaseModule.Close()
}

func (t *TF2) helloLoop(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	t.sendHello(ctx)

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if t.state.Load() == int32(GCConnected) {
				continue
			}
			t.sendHello(ctx)
		}
	}
}

func (t *TF2) sendHello(ctx context.Context) {
	msg := &pb.CMsgClientHello{}

	err := t.gc.Send(ctx, AppID, 4006, msg)
	if err != nil {
		t.Logger.Error("Failed to send ClientHello to GC", log.Err(err))
	} else {
		t.Logger.Debug("Sent ClientHello to TF2 GC")
	}
}

func (t *TF2) messageLoop(ctx context.Context, sub *bus.Subscription) {
	defer sub.Unsubscribe()

	for {
		select {
		case <-ctx.Done():
			return
		case ev, ok := <-sub.C():
			if !ok {
				return
			}
			if msg, ok := ev.(*coordinator.GCMessageEvent); ok {
				if msg.Packet.AppID == AppID {
					t.routePacket(msg.Packet)
				}
			}
		}
	}
}

func (t *TF2) routePacket(pkt *gc.Packet) {
	switch pb.EGCBaseClientMsg(pkt.MsgType) {
	case pb.EGCBaseClientMsg_k_EMsgGCClientWelcome:
		t.handleWelcome(pkt)
	case pb.EGCBaseClientMsg_k_EMsgGCClientGoodbye:
		t.handleGoodbye(pkt)
	}

	switch pb.EGCItemMsg(pkt.MsgType) {
	case pb.EGCItemMsg_k_EMsgGCUpdateItemSchema:
		// Handle schema update
	case pb.EGCItemMsg_k_EMsgGCCraftResponse:
		t.handleCraftResponse(pkt)
	}
	// Shared Object (Inventory) Messages
	switch pb.ESOMsg(pkt.MsgType) {
	case pb.ESOMsg_k_ESOMsg_CacheSubscribed:
		t.cache.HandleSubscribed(pkt, t.Logger, t.Bus)
	case pb.ESOMsg_k_ESOMsg_Create, pb.ESOMsg_k_ESOMsg_Update, pb.ESOMsg_k_ESOMsg_Destroy, pb.ESOMsg_k_ESOMsg_UpdateMultiple:
		t.cache.HandleSOUpdate(pkt, t.Logger, t.Bus)
	}
}

func (t *TF2) handleWelcome(pkt *gc.Packet) {
	msg := &pb.CMsgClientWelcome{}
	if err := proto.Unmarshal(pkt.Payload, msg); err != nil {
		t.Logger.Error("Failed to unmarshal Welcome", log.Err(err))
		return
	}

	if t.state.CompareAndSwap(int32(GCConnecting), int32(GCConnected)) {
		t.Logger.Info("Connected to TF2 Game Coordinator", log.Uint32("version", msg.GetVersion()))
		t.Bus.Publish(&GCConnectedEvent{Version: msg.GetVersion()})
	}
}

func (t *TF2) handleGoodbye(pkt *gc.Packet) {
	t.Logger.Warn("Disconnected from TF2 Game Coordinator (Server Goodbye)")

	if t.state.CompareAndSwap(int32(GCConnected), int32(GCConnecting)) {
		t.Bus.Publish(&GCDisconnectedEvent{})
	}
}

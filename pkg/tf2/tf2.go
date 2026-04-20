// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package tf2

import (
	"context"
	"encoding/binary"
	"errors"
	"sync/atomic"
	"time"

	"google.golang.org/protobuf/proto"

	"github.com/lemon4ksan/g-man/pkg/bus"
	"github.com/lemon4ksan/g-man/pkg/jobs"
	"github.com/lemon4ksan/g-man/pkg/log"
	pb "github.com/lemon4ksan/g-man/pkg/protobuf/tf2"
	"github.com/lemon4ksan/g-man/pkg/steam"
	"github.com/lemon4ksan/g-man/pkg/steam/module"
	"github.com/lemon4ksan/g-man/pkg/steam/protocol"
	"github.com/lemon4ksan/g-man/pkg/steam/sys/apps"
	"github.com/lemon4ksan/g-man/pkg/steam/sys/gc"
	"github.com/lemon4ksan/g-man/pkg/tf2/schema/manager"
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
	Send(ctx context.Context, appID, msgType uint32, msg proto.Message) error
	SendRaw(ctx context.Context, appID, msgType uint32, payload []byte) error
	Call(ctx context.Context, appID, msgType uint32, msg proto.Message, cb jobs.Callback[*protocol.GCPacket]) error
	CallRaw(ctx context.Context, appID, msgType uint32, payload []byte, cb jobs.Callback[*protocol.GCPacket]) error
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
	module.Base

	gc   CoordinatorProvider
	apps AppsProvider

	state  atomic.Int32
	cache  *SOCache
	schema *manager.Manager
}

func New() *TF2 {
	return &TF2{
		Base: module.New(ModuleName),
	}
}

func (t *TF2) Name() string { return ModuleName }

func (t *TF2) Init(init module.InitContext) error {
	if err := t.Base.Init(init); err != nil {
		return err
	}

	gcMod, ok := init.Module(gc.ModuleName).(CoordinatorProvider)
	if !ok || gcMod == nil {
		return errors.New("gc module not registered or invalid")
	}

	t.gc = gcMod

	apps, ok := init.Module(apps.ModuleName).(AppsProvider)
	if !ok || apps == nil {
		return errors.New("apps module not registered or invalid")
	}

	t.apps = apps

	schema, ok := init.Module(manager.ModuleName).(*manager.Manager)
	if !ok || schema == nil {
		return errors.New("schema module not registered or invalid")
	}

	t.schema = schema

	t.cache = NewSOCache(t.gc, WithBus(t.Bus), WithLogger(t.Logger), WithSchema(t.schema.Get()))

	sub := t.Bus.Subscribe(&gc.GCMessageEvent{})
	t.Go(func(ctx context.Context) {
		t.messageLoop(ctx, sub)
	})

	return nil
}

// StartAuthed occurs when Steam logs in.
// We need to "start" TF2 so that GC can start talking to us.
func (t *TF2) StartAuthed(ctx context.Context, authCtx module.AuthContext) error {
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
	return t.Base.Close()
}

func (t *TF2) Cache() *SOCache {
	return t.cache
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
	msg := &pb.CMsgClientHello{
		Version: proto.Uint32(65580),
	}

	err := t.gc.Send(ctx, AppID, uint32(pb.EGCBaseClientMsg_k_EMsgGCClientHello), msg)
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

			if msg, ok := ev.(*gc.GCMessageEvent); ok {
				if msg.Packet.AppID == AppID {
					t.routePacket(ctx, msg.Packet)
				}
			}
		}
	}
}

func (t *TF2) routePacket(ctx context.Context, pkt *protocol.GCPacket) {
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
		t.cache.HandleSubscribed(pkt)
	case pb.ESOMsg_k_ESOMsg_Create,
		pb.ESOMsg_k_ESOMsg_Update,
		pb.ESOMsg_k_ESOMsg_Destroy,
		pb.ESOMsg_k_ESOMsg_UpdateMultiple:
		t.cache.HandleSOUpdate(pkt)
	case pb.ESOMsg_k_ESOMsg_CacheSubscriptionCheck:
		t.cache.HandleSOCacheCheck(ctx, pkt)
	case pb.ESOMsg_k_ESOMsg_CacheSubscribedUpToDate:
		t.cache.HandleUpToDate(pkt)
	}
}

func (t *TF2) handleWelcome(pkt *protocol.GCPacket) {
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

func (t *TF2) handleGoodbye(_ *protocol.GCPacket) {
	t.Logger.Warn("Disconnected from TF2 Game Coordinator (Server Goodbye)")

	if t.state.CompareAndSwap(int32(GCConnected), int32(GCConnecting)) {
		t.Bus.Publish(&GCDisconnectedEvent{})
	}
}

func parseCraftResponse(payload []byte) []uint64 {
	// [BlueprintID(int16)] [Unknown(uint32)] [Count(uint16)] [IDs(uint64...)]
	if len(payload) < 8 {
		return nil
	}

	count := int(binary.LittleEndian.Uint16(payload[6:]))
	items := make([]uint64, 0, count)

	for i := range count {
		offset := 8 + (i * 8)
		if len(payload) < offset+8 {
			break
		}

		items = append(items, binary.LittleEndian.Uint64(payload[offset:]))
	}

	return items
}

func (t *TF2) handleCraftResponse(pkt *protocol.GCPacket) {
	// Broadcast event for listeners (logs, analytics)
	// The specific job callback handles the logic flow.
	items := parseCraftResponse(pkt.Payload)
	if len(items) > 0 || len(pkt.Payload) >= 2 {
		blueprint := binary.LittleEndian.Uint16(pkt.Payload[0:])
		t.Bus.Publish(&CraftResponseEvent{
			BlueprintID:  blueprint,
			CreatedItems: items,
		})
	}
}

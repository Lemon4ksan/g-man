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

	"github.com/lemon4ksan/g-man/pkg/log"
	"github.com/lemon4ksan/g-man/pkg/modules/coordinator"
	"github.com/lemon4ksan/g-man/pkg/steam"
	"github.com/lemon4ksan/g-man/pkg/steam/bus"
	"github.com/lemon4ksan/g-man/pkg/steam/protocol/gc"

	pb "github.com/lemon4ksan/g-man/pkg/tf2/protobuf"
	"google.golang.org/protobuf/proto"
)

const (
	AppID             = 440
	ModuleName string = "tf2"
)

// ConnectionState reflects the GC session status.
type ConnectionState int32

const (
	GCDisconnected ConnectionState = iota
	GCConnecting
	GCConnected
)

type TF2 struct {
	client *steam.Client
	gc     *coordinator.Coordinator
	logger log.Logger
	bus    *bus.Bus

	state    atomic.Int32
	backpack *Backpack

	ctx    context.Context
	cancel context.CancelFunc
}

func New(logger log.Logger) *TF2 {
	return &TF2{
		logger: logger,
	}
}

func (t *TF2) Name() string { return ModuleName }

func (t *TF2) Init(c *steam.Client) error {
	t.client = c

	gcModule := c.GetModule(coordinator.ModuleName)
	if gcModule == nil {
		return errors.New("tf2: coordinator not initialized")
	}
	t.gc = gcModule.(*coordinator.Coordinator)

	sub := c.Bus().Subscribe(&coordinator.GCMessageEvent{})
	go t.loop(sub)

	t.backpack = NewBackpack(t.logger, c.Bus())
	return nil
}

func (t *TF2) Start(ctx context.Context) error {
	t.ctx, t.cancel = context.WithCancel(ctx)

	// Start message loop
	sub := t.bus.Subscribe(&coordinator.GCMessageEvent{})
	go t.loop(sub)

	return nil
}

// PlayGame simulates launching TF2. This is required to receive GC messages.
func (t *TF2) PlayGame(ctx context.Context) {
	t.connect() // Trigger GC handshake
}

func (t *TF2) connect() {
	if !t.state.CompareAndSwap(int32(GCDisconnected), int32(GCConnecting)) {
		return
	}

	t.logger.Info("Connecting to TF2 GC...")

	// Send ClientHello
	// Note: TF2 uses a legacy-style ClientHello usually, or an empty proto
	hello := proto.Message(&pb.CMsgClientHello{})
	t.gc.Send(t.ctx, AppID, uint32(pb.EGCBaseClientMsg_k_EMsgGCClientHello), hello)

	// Start hello ticker until we get Welcome
	go t.helloTicker()
}

func (t *TF2) helloTicker() {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-t.ctx.Done():
			return
		case <-ticker.C:
			if t.state.Load() == int32(GCConnected) {
				return
			}
			t.logger.Debug("Retrying ClientHello...")
			hello := proto.Message(&pb.CMsgClientHello{})
			t.gc.Send(t.ctx, AppID, uint32(pb.EGCBaseClientMsg_k_EMsgGCClientHello), hello)
		}
	}
}

func (t *TF2) loop(sub *bus.Subscription) {
	defer sub.Unsubscribe()

	for {
		select {
		case <-t.ctx.Done():
			return
		case ev := <-sub.C():
			switch e := ev.(type) {
			case *coordinator.GCMessageEvent:
				if e.Packet.AppID == AppID {
					t.handlePacket(e.Packet)
				}
			}
		}
	}
}

func (t *TF2) handlePacket(pkt *gc.Packet) {
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

	case pb.EGCItemMsg_k_EMsgGCTrading_InitiateTradeRequest:
		t.handleTradeRequest(pkt)
	}

	switch pb.ESOMsg(pkt.MsgType) {
	// Shared Object (Inventory) Messages
	case pb.ESOMsg_k_ESOMsg_CacheSubscriptionCheck:
		t.backpack.HandleCacheCheck(t.gc, t.client.SteamID())
	case pb.ESOMsg_k_ESOMsg_CacheSubscribed:
		t.backpack.HandleSubscribed(pkt)
	case pb.ESOMsg_k_ESOMsg_Create:
		t.backpack.HandleCreate(pkt)
	case pb.ESOMsg_k_ESOMsg_Update:
		t.backpack.HandleUpdate(pkt)
	case pb.ESOMsg_k_ESOMsg_Destroy:
		t.backpack.HandleDestroy(pkt)
	}
}

func (t *TF2) handleWelcome(_ *gc.Packet) {
	t.state.Store(int32(GCConnected))
	t.logger.Info("Connected to TF2 Game Coordinator")
	t.bus.Publish(&GCConnectedEvent{Version: 1}) // Extract version from packet if needed
}

func (t *TF2) handleGoodbye(_ *gc.Packet) {
	t.state.Store(int32(GCDisconnected))
	t.logger.Warn("Disconnected from TF2 GC")
	t.bus.Publish(&GCDisconnectedEvent{})

	// Auto-reconnect logic
	time.AfterFunc(2*time.Second, t.connect)
}

func (t *TF2) handleTradeRequest(pkt *gc.Packet) {
	if len(pkt.Payload) < 12 {
		return
	}

	// Skip 4 bytes (unknown), then read SteamID
	tradeID := binary.LittleEndian.Uint32(pkt.Payload[0:4])
	steamID := binary.LittleEndian.Uint64(pkt.Payload[4:12])

	t.bus.Publish(&TradeRequestEvent{
		SteamID: steamID,
		TradeID: tradeID,
	})
}

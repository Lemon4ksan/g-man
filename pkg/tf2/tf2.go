// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package tf2

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"sync/atomic"
	"time"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/protoadapt"

	"github.com/lemon4ksan/g-man/pkg/behavior/achievements"
	"github.com/lemon4ksan/g-man/pkg/bus"
	"github.com/lemon4ksan/g-man/pkg/jobs"
	"github.com/lemon4ksan/g-man/pkg/log"
	"github.com/lemon4ksan/g-man/pkg/protobuf/custom"
	pb_steam "github.com/lemon4ksan/g-man/pkg/protobuf/steam"
	pb "github.com/lemon4ksan/g-man/pkg/protobuf/tf2"
	"github.com/lemon4ksan/g-man/pkg/steam"
	"github.com/lemon4ksan/g-man/pkg/steam/id"
	"github.com/lemon4ksan/g-man/pkg/steam/module"
	"github.com/lemon4ksan/g-man/pkg/steam/protocol"
	"github.com/lemon4ksan/g-man/pkg/steam/protocol/enums"
	"github.com/lemon4ksan/g-man/pkg/steam/service"
	"github.com/lemon4ksan/g-man/pkg/steam/sys/apps"
	"github.com/lemon4ksan/g-man/pkg/steam/sys/gc"
	"github.com/lemon4ksan/g-man/pkg/tf2/schema"
)

const (
	// AppID is the application ID for Team Fortress 2.
	AppID = 440
	// ModuleName is the name of the TF2 module.
	ModuleName string = "tf2"
)

// WithModule returns an option that registers the TF2 module with the steam client.
func WithModule() steam.Option {
	return func(c *steam.Client) {
		c.RegisterModule(New())
	}
}

// From returns the TF2 module from the client.
func From(c *steam.Client) *TF2 {
	return steam.GetModule[*TF2](c)
}

// AchievementConfig returns the standard strategy config for TF2 achievements for achievements manager.
func AchievementConfig() achievements.Config {
	return achievements.Config{
		AppID:            AppID,
		TotalCount:       520,
		MinTargetPercent: 0.70,
		MaxTargetPercent: 0.82,
		UnlockChance:     0.40,
		BreakChance:      0.10,
		InitialDelay:     5 * time.Second,
		AchievementPool: [][]uint32{
			{1001, 1041}, // Scout
			{1101, 1142}, // Sniper
			{1201, 1240}, // Soldier
			{1301, 1340}, // Demoman
			{1401, 1440}, // Medic
			{1501, 1540}, // Heavy
			{1601, 1640}, // Pyro
			{1701, 1740}, // Spy
			{1801, 1840}, // Engy
			{1901, 1921}, // Halloween
			{2201, 2212}, // Foundry
			{2301, 2352}, // MvM
			{2401, 2412}, // Doomsday
			{2701, 2705}, // Snakewater
			{2801, 2805}, // Powerhouse
		},
	}
}

// State reflects the GC session status.
type State int32

// TF2 Game Coordinator connection states.
const (
	Disconnected State = iota
	Connecting
	Connected
)

// CoordinatorProvider defines what TF2 needs from the generic GC module.
type CoordinatorProvider interface {
	Send(ctx context.Context, appID, msgType uint32, msg proto.Message) error
	SendRaw(ctx context.Context, appID, msgType uint32, payload []byte) error
	Call(ctx context.Context, appID, msgType uint32, msg proto.Message, cb jobs.Callback[*protocol.GCPacket]) error
	CallRaw(ctx context.Context, appID, msgType uint32, payload []byte, cb jobs.Callback[*protocol.GCPacket]) error
}

// AppsProvider defines what TF2 needs from the generic Apps module.
type AppsProvider interface {
	PlayGames(ctx context.Context, appIDs []uint32, forceKick bool) error
}

// SchemaProvider defines what TF2 needs from the schema manager.
type SchemaProvider interface {
	Get() *schema.Schema
}

// TF2 provides the core logic for interacting with the Team Fortress 2 Game Coordinator.
// It manages the GC session, handles SO (Shared Object) cache updates, and provides
// a high-level API for inventory management and schema access.
//
// The module follows a reactive architecture where the SOCache is the single source of truth
// for all inventory data. Other modules, like Backpack or MetalManager, should act as
// lightweight views or controllers over this cache.
type TF2 struct {
	module.Base

	steamID id.ID
	gc      CoordinatorProvider
	service service.Doer
	apps    AppsProvider

	state  atomic.Int32
	cache  *SOCache
	schema SchemaProvider
}

// New creates a new TF2 module.
func New() *TF2 {
	return &TF2{
		Base: module.New(ModuleName),
	}
}

// Name returns the name of the module.
func (t *TF2) Name() string { return ModuleName }

// Init initializes the module.
func (t *TF2) Init(init module.InitContext) error {
	if err := t.Base.Init(init); err != nil {
		return err
	}

	gcMod, ok := init.Module(gc.ModuleName).(CoordinatorProvider)
	if !ok || gcMod == nil {
		return errors.New("gc module not registered or invalid")
	}

	t.gc = gcMod
	t.service = init.Service()

	appsMod, ok := init.Module(apps.ModuleName).(AppsProvider)
	if !ok || appsMod == nil {
		return errors.New("apps module not registered or invalid")
	}

	t.apps = appsMod

	schemaMod, ok := init.Module(schema.ModuleName).(SchemaProvider)
	if !ok || schemaMod == nil {
		return errors.New("schema module not registered or invalid")
	}

	t.schema = schemaMod

	t.cache = NewSOCache(t.gc, WithBus(t.Bus), WithLogger(t.Logger), WithSchema(t.schema.Get()))

	sub := t.Bus.Subscribe(&gc.MessageEvent{})
	t.Go(func(ctx context.Context) {
		t.messageLoop(ctx, sub)
	})

	return nil
}

// StartAuthed occurs when Steam logs in.
// We need to "start" TF2 so that GC can start talking to us.
func (t *TF2) StartAuthed(ctx context.Context, authCtx module.AuthContext) error {
	if authCtx != nil {
		t.steamID = authCtx.SteamID()
	}

	if err := t.apps.PlayGames(ctx, []uint32{AppID}, false); err != nil {
		return err
	}

	t.state.Store(int32(Connecting))
	t.Go(func(ctx context.Context) {
		t.helloLoop(ctx)
	})

	return nil
}

// Close occurs when steam closes the connection.
func (t *TF2) Close() error {
	t.state.Store(int32(Disconnected))
	return t.Base.Close()
}

// Cache returns the item cache.
func (t *TF2) Cache() *SOCache {
	return t.cache
}

// AwardAchievement unlocks the specified achievement in TF2.
func (t *TF2) AwardAchievement(ctx context.Context, achievementID uint32) error {
	req := &custom.CMsgClientStoreUserStats{
		GameId: proto.Uint64(AppID),
		Achievements: []*custom.CMsgClientStoreUserStats_Achievement{
			{
				AchievementId: proto.Uint32(achievementID),
				UnlockTime:    []uint32{0xFFFFFFFF}, // Signals immediate unlock
			},
		},
	}

	_, err := service.LegacyProto[service.NoResponse](
		ctx,
		t.service,
		enums.EMsg_ClientStoreUserStats,
		protoadapt.MessageV2Of(req),
		service.WithRoutingAppID(AppID),
	)

	return err
}

// SetStat sets the specified statistic in TF2.
func (t *TF2) SetStat(ctx context.Context, statID, value uint32) error {
	req := &custom.CMsgClientStoreUserStats{
		GameId: proto.Uint64(AppID),
		Stats: []*custom.CMsgClientStoreUserStats_Stat{
			{
				StatId:    proto.Uint32(statID),
				StatValue: proto.Uint32(value),
			},
		},
	}

	_, err := service.LegacyProto[service.NoResponse](
		ctx,
		t.service,
		enums.EMsg_ClientStoreUserStats,
		protoadapt.MessageV2Of(req),
		service.WithRoutingAppID(AppID),
	)

	return err
}

// GetCurrentAchievements returns a map of achievements that have already been unlocked.
func (t *TF2) GetCurrentAchievements(ctx context.Context) (map[uint32]bool, error) {
	t.Logger.Debug("Querying achievements progress", log.Uint64("steam_idForUser", t.steamID.Uint64()))

	req := &pb_steam.CMsgClientGetUserStats{
		GameId: proto.Uint64(AppID),
	}

	resp, err := service.LegacyProto[pb_steam.CMsgClientGetUserStatsResponse](
		ctx,
		t.service,
		enums.EMsg_ClientGetUserStats,
		req,
		service.WithRoutingAppID(AppID),
	)
	if err != nil {
		return nil, err
	}

	unlocked := make(map[uint32]bool)
	for _, block := range resp.GetAchievementBlocks() {
		baseID := block.GetAchievementId()

		for idx, unlockTime := range block.GetUnlockTime() {
			if unlockTime > 0 {
				achievementID := baseID + uint32(idx)
				unlocked[achievementID] = true
			}
		}
	}

	return unlocked, nil
}

// PlayGames launches a game in TF2 (or stops it if the list is empty).
func (t *TF2) PlayGames(ctx context.Context, appIDs []uint32) error {
	return t.apps.PlayGames(ctx, appIDs, false)
}

// Craft sends a crafting request to the Game Coordinator.
func (t *TF2) Craft(ctx context.Context, items []uint64, recipe int16) ([]uint64, error) {
	// Format (SDK MsgGCCraft_t): [Recipe(int16)] [Count(uint16)] [ItemID(uint64)]...
	buf := new(bytes.Buffer)
	_ = binary.Write(buf, binary.LittleEndian, recipe)
	_ = binary.Write(buf, binary.LittleEndian, uint16(len(items)))

	for _, id := range items {
		_ = binary.Write(buf, binary.LittleEndian, id)
	}

	resCh := make(chan []uint64, 1)
	errCh := make(chan error, 1)

	err := t.gc.CallRaw(
		ctx,
		AppID,
		uint32(pb.EGCItemMsg_k_EMsgGCCraft),
		buf.Bytes(),
		func(pkt *protocol.GCPacket, err error) {
			if err != nil {
				errCh <- err
				return
			}

			newItems := parseCraftResponse(pkt.Payload)
			resCh <- newItems
		},
	)
	if err != nil {
		return nil, err
	}

	select {
	case items := <-resCh:
		return items, nil
	case err := <-errCh:
		return nil, err
	case <-ctx.Done():
		return nil, ctx.Err()
	}
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
			if t.state.Load() == int32(Connected) {
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

			if msg, ok := ev.(*gc.MessageEvent); ok {
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
		t.handleSchemaUpdate(pkt)
	case pb.EGCItemMsg_k_EMsgGCCraftResponse:
		t.handleCraftResponse(pkt)
	}

	// Shared Object (Inventory) Messages
	switch pb.ESOMsg(pkt.MsgType) {
	case pb.ESOMsg_k_ESOMsg_CacheSubscribed:
		t.cache.handleSubscribed(pkt)
	case pb.ESOMsg_k_ESOMsg_Create,
		pb.ESOMsg_k_ESOMsg_Update,
		pb.ESOMsg_k_ESOMsg_Destroy,
		pb.ESOMsg_k_ESOMsg_UpdateMultiple:
		t.cache.handleSOUpdate(pkt)
	case pb.ESOMsg_k_ESOMsg_CacheSubscriptionCheck:
		t.cache.handleSOCacheCheck(ctx, pkt)
	case pb.ESOMsg_k_ESOMsg_CacheSubscribedUpToDate:
		t.cache.handleUpToDate(pkt)
	}
}

func (t *TF2) handleWelcome(pkt *protocol.GCPacket) {
	msg := &pb.CMsgClientWelcome{}
	if err := proto.Unmarshal(pkt.Payload, msg); err != nil {
		t.Logger.Error("Failed to unmarshal Welcome", log.Err(err))
		return
	}

	if t.state.CompareAndSwap(int32(Connecting), int32(Connected)) {
		t.Logger.Info("Connected to TF2 Game Coordinator", log.Uint32("version", msg.GetVersion()))
		t.Bus.Publish(&ConnectedEvent{Version: msg.GetVersion()})
	}
}

func (t *TF2) handleGoodbye(_ *protocol.GCPacket) {
	t.Logger.Warn("Disconnected from TF2 Game Coordinator (Server Goodbye)")

	if t.state.CompareAndSwap(int32(Connected), int32(Connecting)) {
		t.Bus.Publish(&DisconnectedEvent{})
	}
}

func (t *TF2) handleSchemaUpdate(pkt *protocol.GCPacket) {
	msg := &pb.CMsgUpdateItemSchema{}
	if err := proto.Unmarshal(pkt.Payload, msg); err != nil {
		t.Logger.Error("Failed to unmarshal UpdateItemSchema", log.Err(err))
		return
	}

	t.Logger.Info("Received item schema update notification from GC",
		log.Uint32("version", msg.GetItemSchemaVersion()),
	)

	t.Bus.Publish(&schema.UpdateRequestedEvent{
		Version:      msg.GetItemSchemaVersion(),
		ItemsGameURL: msg.GetItemsGameUrl(),
	})
}

func (t *TF2) handleCraftResponse(pkt *protocol.GCPacket) {
	items := parseCraftResponse(pkt.Payload)
	if len(items) > 0 || len(pkt.Payload) >= 2 {
		blueprint := binary.LittleEndian.Uint16(pkt.Payload[0:])
		t.Bus.Publish(&CraftResponseEvent{
			BlueprintID:  blueprint,
			CreatedItems: items,
		})
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

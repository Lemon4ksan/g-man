// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package apps manages "In-Game" playing statuses, license lists, and game connect tokens.
package apps

import (
	"context"
	"fmt"
	"slices"
	"sync"
	"time"

	"github.com/lemon4ksan/miyako/log"
	"google.golang.org/protobuf/proto"

	pb "github.com/lemon4ksan/g-man/pkg/protobuf/steam"
	"github.com/lemon4ksan/g-man/pkg/steam"
	"github.com/lemon4ksan/g-man/pkg/steam/module"
	"github.com/lemon4ksan/g-man/pkg/steam/protocol"
	"github.com/lemon4ksan/g-man/pkg/steam/protocol/enums"
	"github.com/lemon4ksan/g-man/pkg/steam/service"
)

const ModuleName string = "apps"

const NonSteamGameID uint64 = 15190414816125648896

// WithModule registers the Apps module in the client.
func WithModule() steam.Option {
	return steam.WithModule(New())
}

// From retrieves the Apps module instance from the client.
func From(c *steam.Client) *Apps {
	return steam.GetModule[*Apps](c)
}

// Apps tracks active playing states and license holdings.
//
// Thread Safety:
//   - Safe for concurrent use across all methods.
type Apps struct {
	module.Base

	client service.Doer

	mu             sync.RWMutex
	playingAppIDs  []uint32
	playingBlocked bool
	licenses       []*pb.CMsgClientLicenseList_License
	connectTokens  [][]byte

	unregFuncs []func()
}

// New constructs an Apps module instance.
func New() *Apps {
	return &Apps{
		Base:          module.New(ModuleName),
		playingAppIDs: make([]uint32, 0),
		connectTokens: make([][]byte, 0),
	}
}

func (a *Apps) Init(init module.InitContext) error {
	if err := a.Base.Init(init); err != nil {
		return err
	}

	a.client = init.Service()

	init.RegisterPacketHandler(enums.EMsg_ClientPlayingSessionState, a.handlePlayingSessionState)
	init.RegisterPacketHandler(enums.EMsg_ClientLicenseList, a.handleLicenseList)
	init.RegisterPacketHandler(enums.EMsg_ClientGameConnectTokens, a.handleGameConnectTokens)

	a.unregFuncs = append(a.unregFuncs, func() {
		init.UnregisterPacketHandler(enums.EMsg_ClientPlayingSessionState)
		init.UnregisterPacketHandler(enums.EMsg_ClientLicenseList)
		init.UnregisterPacketHandler(enums.EMsg_ClientGameConnectTokens)
	})

	return nil
}

func (a *Apps) Close() error {
	a.mu.Lock()
	for _, unreg := range a.unregFuncs {
		unreg()
	}

	a.unregFuncs = nil
	a.mu.Unlock()

	return a.Base.Close()
}

// GetPlayerCount queries current online player count for an appID via Steam Data Publisher.
func (a *Apps) GetPlayerCount(ctx context.Context, appID uint32) (int32, error) {
	req := &pb.CMsgDPGetNumberOfCurrentPlayers{
		Appid: proto.Uint32(appID),
	}

	resp, err := service.LegacyProto[pb.CMsgDPGetNumberOfCurrentPlayersResponse](
		ctx,
		a.client,
		enums.EMsg_ClientGetNumberOfCurrentPlayersDP,
		req,
	)
	if err != nil {
		return 0, fmt.Errorf("apps: failed to get player count: %w", err)
	}

	eResult := enums.EResult(resp.GetEresult())
	if eResult != enums.EResult_OK {
		return 0, fmt.Errorf("apps: steam error: %s", eResult.String())
	}

	return resp.GetPlayerCount(), nil
}

// PlayGames sets account presence to "In-Game" for specified AppIDs.
func (a *Apps) PlayGames(ctx context.Context, appIDs []uint32, forceKick bool) error {
	a.mu.RLock()
	blocked := a.playingBlocked
	a.mu.RUnlock()

	if blocked && forceKick {
		a.Logger.Info("Playing session is blocked by another client. Attempting to kick...")

		if err := a.KickPlayingSession(ctx); err != nil {
			a.Logger.Error("Failed to kick other playing session", log.Err(err))
		}

		time.Sleep(500 * time.Millisecond)
	}

	games := make([]*pb.CMsgClientGamesPlayed_GamePlayed, 0, len(appIDs))
	for _, id := range appIDs {
		games = append(games, &pb.CMsgClientGamesPlayed_GamePlayed{
			GameId: proto.Uint64(uint64(id)),
		})
	}

	return a.sendGamesPlayed(ctx, games, appIDs)
}

// PlayCustomGames sets "In-Game" status for non-Steam shortcuts with custom display names.
func (a *Apps) PlayCustomGames(ctx context.Context, names []string) error {
	games := make([]*pb.CMsgClientGamesPlayed_GamePlayed, 0, len(names))
	for _, name := range names {
		games = append(games, &pb.CMsgClientGamesPlayed_GamePlayed{
			GameId:        proto.Uint64(NonSteamGameID),
			GameExtraInfo: proto.String(name),
		})
	}

	return a.sendGamesPlayed(ctx, games, nil)
}

// StopPlaying clears active "In-Game" presence.
func (a *Apps) StopPlaying(ctx context.Context) error {
	return a.PlayGames(ctx, nil, false)
}

// KickPlayingSession disconnects active playing sessions on other devices.
func (a *Apps) KickPlayingSession(ctx context.Context) error {
	_, err := service.LegacyProto[service.NoResponse](
		ctx,
		a.client,
		enums.EMsg_ClientKickPlayingSession,
		&pb.CMsgClientKickPlayingSession{},
	)

	return err
}

func (a *Apps) sendGamesPlayed(
	ctx context.Context,
	games []*pb.CMsgClientGamesPlayed_GamePlayed,
	newAppIDs []uint32,
) error {
	req := &pb.CMsgClientGamesPlayed{
		GamesPlayed: games,
	}

	_, err := service.LegacyProto[service.NoResponse](ctx, a.client, enums.EMsg_ClientGamesPlayedWithDataBlob, req)
	if err != nil {
		return fmt.Errorf("apps: failed to update playing status: %w", err)
	}

	a.mu.Lock()
	defer a.mu.Unlock()

	for _, newID := range newAppIDs {
		if !slices.Contains(a.playingAppIDs, newID) {
			a.Logger.Debug("App launched", log.Uint32("appid", newID))
			a.Bus.Publish(&AppLaunchedEvent{AppID: newID})
		}
	}

	for _, oldID := range a.playingAppIDs {
		if !slices.Contains(newAppIDs, oldID) {
			a.Logger.Debug("App quit", log.Uint32("appid", oldID))
			a.Bus.Publish(&AppQuitEvent{AppID: oldID})
		}
	}

	a.playingAppIDs = newAppIDs

	return nil
}

func (a *Apps) handlePlayingSessionState(packet *protocol.Packet) {
	msg := &pb.CMsgClientPlayingSessionState{}
	if err := protocol.UnmarshalProto(packet.Payload, msg); err != nil {
		a.Logger.Error("Failed to unmarshal playing session state", log.Err(err))
		return
	}

	blocked := msg.GetPlayingBlocked()
	playingApp := msg.GetPlayingApp()

	a.mu.Lock()
	a.playingBlocked = blocked
	a.mu.Unlock()

	if blocked {
		a.Logger.Warn("In-game status blocked by another session", log.Uint32("active_app", playingApp))
	}

	a.Bus.Publish(&PlayingStateEvent{
		Blocked:    blocked,
		PlayingApp: playingApp,
	})
}

// GetLicenses returns cached user license records.
func (a *Apps) GetLicenses() []*pb.CMsgClientLicenseList_License {
	a.mu.RLock()
	defer a.mu.RUnlock()

	return a.licenses
}

// GetConnectTokens returns all cached game connect tokens.
func (a *Apps) GetConnectTokens() [][]byte {
	a.mu.RLock()
	defer a.mu.RUnlock()

	tokens := make([][]byte, len(a.connectTokens))
	copy(tokens, a.connectTokens)

	return tokens
}

// PopConnectToken retrieves and pops the first available game connect token.
func (a *Apps) PopConnectToken() []byte {
	a.mu.Lock()
	defer a.mu.Unlock()

	if len(a.connectTokens) == 0 {
		return nil
	}

	token := a.connectTokens[0]
	a.connectTokens = a.connectTokens[1:]

	return token
}

func (a *Apps) handleLicenseList(packet *protocol.Packet) {
	msg := &pb.CMsgClientLicenseList{}
	if err := protocol.UnmarshalProto(packet.Payload, msg); err != nil {
		a.Logger.Error("Failed to unmarshal license list", log.Err(err))
		return
	}

	a.mu.Lock()
	a.licenses = msg.GetLicenses()
	a.mu.Unlock()

	a.Bus.Publish(&LicensesEvent{
		Licenses: msg.GetLicenses(),
	})
}

func (a *Apps) handleGameConnectTokens(packet *protocol.Packet) {
	msg := &pb.CMsgClientGameConnectTokens{}
	if err := protocol.UnmarshalProto(packet.Payload, msg); err != nil {
		a.Logger.Error("Failed to unmarshal game connect tokens", log.Err(err))
		return
	}

	a.mu.Lock()
	a.connectTokens = append(a.connectTokens, msg.GetTokens()...)

	maxKeep := int(msg.GetMaxTokensToKeep())
	if maxKeep > 0 && len(a.connectTokens) > maxKeep {
		a.connectTokens = a.connectTokens[len(a.connectTokens)-maxKeep:]
	}

	a.mu.Unlock()

	a.Bus.Publish(&GameConnectTokensEvent{
		Tokens: msg.GetTokens(),
	})
}

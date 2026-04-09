// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package apps

import (
	"context"
	"fmt"
	"slices"
	"sync"
	"time"

	"github.com/lemon4ksan/g-man/pkg/log"
	pb "github.com/lemon4ksan/g-man/pkg/protobuf/steam"
	"github.com/lemon4ksan/g-man/pkg/steam"
	"github.com/lemon4ksan/g-man/pkg/steam/module"
	"github.com/lemon4ksan/g-man/pkg/steam/service"
	"github.com/lemon4ksan/g-man/pkg/steam/socket/protocol"
	"google.golang.org/protobuf/proto"
)

const ModuleName string = "apps"

// nonSteamGameID is the special ID used by Steam to represent a "Non-Steam Game" shortcut.
const nonSteamGameID uint64 = 15190414816125648896

func WithModule() steam.Option {
	return func(c *steam.Client) {
		c.RegisterModule(New())
	}
}

// Apps manages the "In-Game" status and interacts with Steam's app services.
type Apps struct {
	module.Base

	// Dependencies
	client service.Doer

	// Internal State
	mu             sync.RWMutex
	playingAppIDs  []uint32
	playingBlocked bool

	unregFuncs []func()
}

// New creates a new instance of the Apps module.
func New() *Apps {
	return &Apps{
		Base:          module.New(ModuleName),
		playingAppIDs: make([]uint32, 0),
	}
}

// Init registers handlers for tracking the state of playing sessions.
func (a *Apps) Init(init module.InitContext) error {
	if err := a.Base.Init(init); err != nil {
		return err
	}

	a.client = init.Service()

	init.RegisterPacketHandler(protocol.EMsg_ClientPlayingSessionState, a.handlePlayingSessionState)

	a.unregFuncs = append(a.unregFuncs, func() {
		init.UnregisterPacketHandler(protocol.EMsg_ClientPlayingSessionState)
	})

	return nil
}

// Close ensures all packet handlers are removed and background tasks are stopped.
func (a *Apps) Close() error {
	a.mu.Lock()
	for _, unreg := range a.unregFuncs {
		unreg()
	}
	a.unregFuncs = nil
	a.mu.Unlock()

	return a.Base.Close()
}

// GetPlayerCount requests the current number of online players for a specific AppID.
// Set appID to 0 to get the total number of users currently connected to Steam.
func (a *Apps) GetPlayerCount(ctx context.Context, appID uint32) (int32, error) {
	req := &pb.CMsgDPGetNumberOfCurrentPlayers{
		Appid: proto.Uint32(appID),
	}

	resp, err := service.Legacy[pb.CMsgDPGetNumberOfCurrentPlayersResponse](ctx, a.client, protocol.EMsg_ClientGetNumberOfCurrentPlayersDP, req)
	if err != nil {
		return 0, fmt.Errorf("apps: failed to get player count: %w", err)
	}

	eResult := protocol.EResult(resp.GetEresult())
	if eResult != protocol.EResult_OK {
		return 0, fmt.Errorf("apps: steam error: %s", eResult.String())
	}

	return resp.GetPlayerCount(), nil
}

// PlayGames updates the account's status to "In-Game" for the specified AppIDs.
// Pass an empty slice to stop playing.
// If forceKick is true, it will attempt to disconnect any other session currently playing games.
func (a *Apps) PlayGames(ctx context.Context, appIDs []uint32, forceKick bool) error {
	a.mu.RLock()
	blocked := a.playingBlocked
	a.mu.RUnlock()

	if blocked && forceKick {
		a.Logger.Info("Playing session is blocked by another client. Attempting to kick...")
		if err := a.KickPlayingSession(ctx); err != nil {
			a.Logger.Error("Failed to kick other playing session", log.Err(err))
		}
		// Give Steam a moment to invalidate the other session
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

// PlayCustomGames sets the "In-Game" status to one or more non-Steam games with custom names.
func (a *Apps) PlayCustomGames(ctx context.Context, names []string) error {
	games := make([]*pb.CMsgClientGamesPlayed_GamePlayed, 0, len(names))
	for _, name := range names {
		games = append(games, &pb.CMsgClientGamesPlayed_GamePlayed{
			GameId:        proto.Uint64(nonSteamGameID),
			GameExtraInfo: proto.String(name),
		})
	}
	return a.sendGamesPlayed(ctx, games, nil)
}

// StopPlaying clears the "In-Game" status for the account.
func (a *Apps) StopPlaying(ctx context.Context) error {
	return a.PlayGames(ctx, nil, false)
}

// KickPlayingSession sends a request to Steam to terminate any other active
// game-playing sessions on this account (e.g., on another PC).
func (a *Apps) KickPlayingSession(ctx context.Context) error {
	_, err := service.Legacy[service.NoResponse](ctx, a.client, protocol.EMsg_ClientKickPlayingSession, &pb.CMsgClientKickPlayingSession{})
	return err
}

// --- Internal Logic ---

func (a *Apps) sendGamesPlayed(ctx context.Context, games []*pb.CMsgClientGamesPlayed_GamePlayed, newAppIDs []uint32) error {
	req := &pb.CMsgClientGamesPlayed{
		GamesPlayed: games,
	}

	_, err := service.Legacy[service.NoResponse](ctx, a.client, protocol.EMsg_ClientGamesPlayedWithDataBlob, req)
	if err != nil {
		return fmt.Errorf("apps: failed to update playing status: %w", err)
	}

	a.mu.Lock()
	defer a.mu.Unlock()

	// Emit events for newly launched apps
	for _, newID := range newAppIDs {
		if !slices.Contains(a.playingAppIDs, newID) {
			a.Logger.Debug("App launched", log.Uint32("appid", newID))
			a.Bus.Publish(&AppLaunchedEvent{AppID: newID})
		}
	}

	// Emit events for quit apps
	for _, oldID := range a.playingAppIDs {
		if !slices.Contains(newAppIDs, oldID) {
			a.Logger.Debug("App quit", log.Uint32("appid", oldID))
			a.Bus.Publish(&AppQuitEvent{AppID: oldID})
		}
	}

	a.playingAppIDs = newAppIDs
	return nil
}

// --- Handlers ---

func (a *Apps) handlePlayingSessionState(packet *protocol.Packet) {
	msg := &pb.CMsgClientPlayingSessionState{}
	if err := proto.Unmarshal(packet.Payload, msg); err != nil {
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

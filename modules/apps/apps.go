// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package apps provides application management (launching, kicking sessions, PICS).
package apps

import (
	"context"
	"fmt"
	"slices"
	"sync"
	"time"

	"github.com/lemon4ksan/g-man/log"
	"github.com/lemon4ksan/g-man/steam"
	"github.com/lemon4ksan/g-man/steam/protocol"
	pb "github.com/lemon4ksan/g-man/steam/protocol/protobuf"
	"google.golang.org/protobuf/proto"
)

const ModuleName string = "apps"

// Magic AppID used by Steam for non-Steam games
const nonSteamGameID uint64 = 15190414816125648896

type Apps struct {
	client *steam.Client
	logger log.Logger

	mu             sync.RWMutex
	playingAppIDs  []uint32
	playingBlocked bool
}

// New creates a new Apps module.
func New(logger log.Logger) *Apps {
	return &Apps{
		logger:        logger,
		playingAppIDs: make([]uint32, 0),
	}
}

func (a *Apps) Name() string { return ModuleName }

func (a *Apps) Init(c *steam.Client) error {
	a.client = c

	// Handle notifications about our account playing state
	c.RegisterPacketHandler(protocol.EMsg_ClientPlayingSessionState, a.handlePlayingSessionState)
	return nil
}

func (a *Apps) Start(ctx context.Context) error {
	return nil
}

// PlayGames tells Steam that you are currently playing the specified AppIDs.
// Pass an empty slice to stop playing all games.
// If forceKick is true, it will attempt to kick any other session playing games on this account.
func (a *Apps) PlayGames(ctx context.Context, appIDs []uint32, forceKick bool) error {
	a.mu.RLock()
	blocked := a.playingBlocked
	a.mu.RUnlock()

	if blocked && forceKick {
		a.logger.Info("Playing is blocked by another session. Forcing kick...")
		if err := a.KickPlayingSession(ctx); err != nil {
			a.logger.Error("Failed to kick playing session", log.Err(err))
		}
		// Give Steam a moment to process the kick
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

// PlayCustomGame sets your status to playing a non-Steam game with a custom name.
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

// StopPlaying tells Steam to clear your "In-Game" status.
func (a *Apps) StopPlaying(ctx context.Context) error {
	return a.PlayGames(ctx, nil, false)
}

// KickPlayingSession asks Steam to disconnect any other client logged into this
// account that is currently playing a game.
func (a *Apps) KickPlayingSession(ctx context.Context) error {
	req := &pb.CMsgClientKickPlayingSession{}
	return a.client.Proto().CallLegacy(ctx, protocol.EMsg_ClientKickPlayingSession, req, nil)
}

func (a *Apps) sendGamesPlayed(ctx context.Context, games []*pb.CMsgClientGamesPlayed_GamePlayed, newAppIDs []uint32) error {
	req := &pb.CMsgClientGamesPlayed{
		GamesPlayed: games,
	}

	if err := a.client.Proto().CallLegacy(ctx, protocol.EMsg_ClientGamesPlayedWithDataBlob, req, nil); err != nil {
		return fmt.Errorf("apps: failed to send games played: %w", err)
	}

	a.mu.Lock()
	defer a.mu.Unlock()

	for _, newID := range newAppIDs {
		if !slices.Contains(a.playingAppIDs, newID) {
			a.logger.Debug("App launched", log.Uint32("appid", newID))
			a.client.Bus().Publish(&AppLaunchedEvent{AppID: newID})
		}
	}

	for _, oldID := range a.playingAppIDs {
		if !slices.Contains(newAppIDs, oldID) {
			a.logger.Debug("App quit", log.Uint32("appid", oldID))
			a.client.Bus().Publish(&AppQuitEvent{AppID: oldID})
		}
	}

	a.playingAppIDs = newAppIDs
	return nil
}

func (a *Apps) handlePlayingSessionState(packet *protocol.Packet) {
	msg := &pb.CMsgClientPlayingSessionState{}
	if err := proto.Unmarshal(packet.Payload, msg); err != nil {
		a.logger.Error("Failed to unmarshal ClientPlayingSessionState", log.Err(err))
		return
	}

	blocked := msg.GetPlayingBlocked()
	playingApp := msg.GetPlayingApp()

	a.mu.Lock()
	a.playingBlocked = blocked
	a.mu.Unlock()

	if blocked {
		a.logger.Warn("Playing state blocked by another session", log.Uint32("playing_app", playingApp))
	}

	a.client.Bus().Publish(&PlayingStateEvent{
		Blocked:    blocked,
		PlayingApp: playingApp,
	})
}

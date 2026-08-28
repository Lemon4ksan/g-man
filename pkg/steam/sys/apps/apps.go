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
	"sync/atomic"
	"time"

	log "github.com/lemon4ksan/foundation/async/logkit"
	"google.golang.org/protobuf/proto"

	"github.com/lemon4ksan/g-man/pkg/steam/client"
	"github.com/lemon4ksan/g-man/pkg/steam/module"
	"github.com/lemon4ksan/g-man/pkg/steam/protocol/enums"
	pb "github.com/lemon4ksan/g-man/protobuf/steam"
)

const ModuleName string = "apps"

const NonSteamGameID uint64 = 15190414816125648896

// WithModule registers the Apps module in the client.
func WithModule() client.Option {
	return client.WithModule(New())
}

// From retrieves the Apps module instance from the client.
func From(c *client.Client) *Apps {
	return client.GetModule[*Apps](c)
}

// Snapshot is an immutable point-in-time state of apps and game sessions.
type Snapshot struct {
	PlayingAppIDs  []uint32
	PlayingBlocked bool
	Licenses       []*pb.CMsgClientLicenseList_License
	ConnectTokens  [][]byte
}

// Apps tracks active playing states and license holdings via lock-free atomic snapshots.
//
// Thread Safety:
//   - Safe for concurrent use across all methods.
type Apps struct {
	module.Base

	events Events
	state  atomic.Pointer[Snapshot]
	mu     sync.Mutex
}

// New constructs an Apps module instance.
func New() *Apps {
	a := &Apps{
		Base: module.New(ModuleName),
	}
	a.state.Store(&Snapshot{
		PlayingAppIDs: make([]uint32, 0),
		ConnectTokens: make([][]byte, 0),
	})

	return a
}

func (a *Apps) Init(init module.InitContext) error {
	if err := a.Base.Init(init); err != nil {
		return err
	}

	a.events = NewEvents(init)
	a.events.OnPlayingSessionState(a.handlePlayingSessionState)
	a.events.OnLicenseList(a.handleLicenseList)
	a.events.OnGameConnectTokens(a.handleGameConnectTokens)

	return nil
}

func (a *Apps) Close() error {
	if a.events != nil {
		_ = a.events.Close()
	}

	return a.Base.Close()
}

// --- Lock-Free State Readers (1 CPU instruction) ---

// Snapshot returns an immutable point-in-time state snapshot.
func (a *Apps) Snapshot() Snapshot {
	return *a.state.Load()
}

// Licenses returns cached user license records.
func (a *Apps) Licenses() []*pb.CMsgClientLicenseList_License {
	return a.state.Load().Licenses
}

// GetLicenses returns cached user license records.
func (a *Apps) GetLicenses() []*pb.CMsgClientLicenseList_License {
	return a.Licenses()
}

// ConnectTokens returns all cached game connect tokens.
func (a *Apps) ConnectTokens() [][]byte {
	return slices.Clone(a.state.Load().ConnectTokens)
}

// GetConnectTokens returns all cached game connect tokens.
func (a *Apps) GetConnectTokens() [][]byte {
	return a.ConnectTokens()
}

// PlayingAppIDs returns the list of currently playing AppIDs.
func (a *Apps) PlayingAppIDs() []uint32 {
	return slices.Clone(a.state.Load().PlayingAppIDs)
}

// IsPlayingBlocked reports whether playing is blocked by another session.
func (a *Apps) IsPlayingBlocked() bool {
	return a.state.Load().PlayingBlocked
}

// PopConnectToken retrieves and pops the first available game connect token.
func (a *Apps) PopConnectToken() []byte {
	for {
		current := a.state.Load()
		if len(current.ConnectTokens) == 0 {
			return nil
		}

		token := current.ConnectTokens[0]
		next := *current
		next.ConnectTokens = next.ConnectTokens[1:]

		if a.state.CompareAndSwap(current, &next) {
			return token
		}
	}
}

// --- Outbound RPC & Playing Methods ---

// GetPlayerCount queries current online player count for an appID via Steam Data Publisher.
func (a *Apps) GetPlayerCount(ctx context.Context, appID uint32) (int32, error) {
	req := &pb.CMsgDPGetNumberOfCurrentPlayers{
		Appid: proto.Uint32(appID),
	}

	resp, err := a.events.GetPlayerCount(ctx, req)
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
	if a.state.Load().PlayingBlocked && forceKick {
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
	return a.events.KickPlayingSession(ctx, &pb.CMsgClientKickPlayingSession{})
}

func (a *Apps) sendGamesPlayed(
	ctx context.Context,
	games []*pb.CMsgClientGamesPlayed_GamePlayed,
	newAppIDs []uint32,
) error {
	req := &pb.CMsgClientGamesPlayed{
		GamesPlayed: games,
	}

	if err := a.events.GamesPlayed(ctx, req); err != nil {
		return fmt.Errorf("apps: failed to update playing status: %w", err)
	}

	a.mu.Lock()
	defer a.mu.Unlock()

	oldAppIDs := a.state.Load().PlayingAppIDs

	for _, newID := range newAppIDs {
		if !slices.Contains(oldAppIDs, newID) {
			a.Logger.Debug("App launched", log.Uint32("appid", newID))
			a.Bus.Publish(&AppLaunchedEvent{AppID: newID})
		}
	}

	for _, oldID := range oldAppIDs {
		if !slices.Contains(newAppIDs, oldID) {
			a.Logger.Debug("App quit", log.Uint32("appid", oldID))
			a.Bus.Publish(&AppQuitEvent{AppID: oldID})
		}
	}

	a.updateState(func(s *Snapshot) {
		s.PlayingAppIDs = newAppIDs
	})

	return nil
}

// --- Copy-On-Write State Updates ---

func (a *Apps) updateState(fn func(next *Snapshot)) {
	for {
		current := a.state.Load()
		next := *current
		fn(&next)

		if a.state.CompareAndSwap(current, &next) {
			return
		}
	}
}

func (a *Apps) handlePlayingSessionState(msg *pb.CMsgClientPlayingSessionState) {
	blocked := msg.GetPlayingBlocked()
	playingApp := msg.GetPlayingApp()

	a.updateState(func(s *Snapshot) {
		s.PlayingBlocked = blocked
	})

	if blocked {
		a.Logger.Warn("In-game status blocked by another session", log.Uint32("active_app", playingApp))
	}

	a.Bus.Publish(&PlayingStateEvent{
		Blocked:    blocked,
		PlayingApp: playingApp,
	})
}

func (a *Apps) handleLicenseList(msg *pb.CMsgClientLicenseList) {
	licenses := msg.GetLicenses()

	a.updateState(func(s *Snapshot) {
		s.Licenses = licenses
	})

	a.Bus.Publish(&LicensesEvent{
		Licenses: licenses,
	})
}

func (a *Apps) handleGameConnectTokens(msg *pb.CMsgClientGameConnectTokens) {
	maxKeep := int(msg.GetMaxTokensToKeep())
	newTokens := msg.GetTokens()

	a.updateState(func(s *Snapshot) {
		s.ConnectTokens = append(s.ConnectTokens, newTokens...)
		if maxKeep > 0 && len(s.ConnectTokens) > maxKeep {
			s.ConnectTokens = s.ConnectTokens[len(s.ConnectTokens)-maxKeep:]
		}
	})

	a.Bus.Publish(&GameConnectTokensEvent{
		Tokens: newTokens,
	})
}

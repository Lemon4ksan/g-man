// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package apps

import (
	"errors"
	"testing"
	"time"

	"github.com/lemon4ksan/miyako/bus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"

	pb "github.com/lemon4ksan/g-man/pkg/protobuf/steam"
	"github.com/lemon4ksan/g-man/pkg/steam/protocol"
	"github.com/lemon4ksan/g-man/pkg/steam/protocol/enums"
	module "github.com/lemon4ksan/g-man/test/mock"
)

const (
	AppidTf2 = 440
	AppidCs2 = 730
)

func setup(t *testing.T) (*Apps, *module.InitContext) {
	t.Helper()

	a := New()
	ictx := module.NewInitContext()

	require.NoError(t, a.Init(ictx), "failed to init apps module")

	t.Cleanup(func() {
		_ = a.Close()
	})

	return a, ictx
}

func TestApps_InitAndClose(t *testing.T) {
	t.Parallel()

	t.Run("success_lifecycle", func(t *testing.T) {
		t.Parallel()

		a := New()
		ictx := module.NewInitContext()

		assert.Equal(t, ModuleName, a.Name())

		err := a.Init(ictx)
		require.NoError(t, err)

		ictx.AssertPacketHandlerRegistered(t, enums.EMsg_ClientPlayingSessionState)

		err = a.Close()
		require.NoError(t, err)

		ictx.AssertPacketHandlerUnregistered(t, enums.EMsg_ClientPlayingSessionState)
	})
}

func TestApps_GetPlayerCount(t *testing.T) {
	t.Parallel()

	t.Run("success", func(t *testing.T) {
		t.Parallel()
		a, ictx := setup(t)
		ctx := t.Context()

		ictx.MockService().SetLegacyResponse(
			enums.EMsg_ClientGetNumberOfCurrentPlayersDP,
			&pb.CMsgDPGetNumberOfCurrentPlayersResponse{
				Eresult:     proto.Int32(int32(enums.EResult_OK)),
				PlayerCount: proto.Int32(100500),
			},
		)

		count, err := a.GetPlayerCount(ctx, AppidTf2)
		require.NoError(t, err)
		assert.Equal(t, int32(100500), count)
	})

	t.Run("eresult_error", func(t *testing.T) {
		t.Parallel()
		a, ictx := setup(t)
		ctx := t.Context()

		ictx.MockService().SetLegacyResponse(
			enums.EMsg_ClientGetNumberOfCurrentPlayersDP,
			&pb.CMsgDPGetNumberOfCurrentPlayersResponse{
				Eresult: proto.Int32(int32(enums.EResult_AccessDenied)),
			},
		)

		_, err := a.GetPlayerCount(ctx, AppidTf2)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "steam error: AccessDenied")
	})

	t.Run("network_error", func(t *testing.T) {
		t.Parallel()
		a, ictx := setup(t)
		ctx := t.Context()

		ictx.MockService().ResponseErrs[enums.EMsg_ClientGetNumberOfCurrentPlayersDP.String()] = errors.New(
			"network timeout",
		)

		_, err := a.GetPlayerCount(ctx, AppidTf2)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to get player count")
		assert.Contains(t, err.Error(), "network timeout")
	})
}

func TestApps_HandlePlayingSessionState(t *testing.T) {
	t.Parallel()

	t.Run("valid_packet", func(t *testing.T) {
		t.Parallel()
		a, ictx := setup(t)

		subState := ictx.Bus().Subscribe(&PlayingStateEvent{})
		defer subState.Unsubscribe()

		ictx.EmitPacket(t, enums.EMsg_ClientPlayingSessionState, &pb.CMsgClientPlayingSessionState{
			PlayingBlocked: proto.Bool(true),
			PlayingApp:     proto.Uint32(AppidCs2),
		})

		a.mu.RLock()
		isBlocked := a.playingBlocked
		a.mu.RUnlock()

		assert.True(t, isBlocked)

		select {
		case ev := <-subState.C():
			event := ev.(*PlayingStateEvent)
			assert.True(t, event.Blocked)
			assert.Equal(t, uint32(AppidCs2), event.PlayingApp)
		case <-time.After(time.Second):
			t.Fatal("timed out waiting for PlayingStateEvent")
		}
	})

	t.Run("invalid_packet", func(t *testing.T) {
		t.Parallel()
		a, _ := setup(t)

		a.handlePlayingSessionState(&protocol.Packet{
			EMsg:    enums.EMsg_ClientPlayingSessionState,
			Payload: []byte{0xFF, 0xFF, 0xFF}, // Invalid protobuf
		})
	})
}

func TestApps_PlayGames_Sequence(t *testing.T) {
	t.Parallel()

	a, ictx := setup(t)
	ctx := t.Context()

	subL := ictx.Bus().Subscribe(&AppLaunchedEvent{})
	defer subL.Unsubscribe()

	subQ := ictx.Bus().Subscribe(&AppQuitEvent{})
	defer subQ.Unsubscribe()

	collectIDs := func(ch <-chan bus.Event, count int) []uint32 {
		ids := make([]uint32, 0, count)
		for i := range count {
			select {
			case ev := <-ch:
				if l, ok := ev.(*AppLaunchedEvent); ok {
					ids = append(ids, l.AppID)
				} else if q, ok := ev.(*AppQuitEvent); ok {
					ids = append(ids, q.AppID)
				}

			case <-time.After(500 * time.Millisecond):
				t.Fatalf("expected %d events, but timed out at %d", count, i)
			}
		}

		return ids
	}

	require.NoError(t, a.PlayGames(ctx, []uint32{AppidTf2}, false))
	require.NoError(t, a.PlayGames(ctx, []uint32{AppidTf2, AppidCs2}, false))
	require.NoError(t, a.PlayGames(ctx, []uint32{AppidCs2}, false))
	require.NoError(t, a.StopPlaying(ctx))

	launched := collectIDs(subL.C(), 2)
	quit := collectIDs(subQ.C(), 2)

	expected := []uint32{AppidTf2, AppidCs2}
	assert.ElementsMatch(t, expected, launched, "launched apps mismatch")
	assert.ElementsMatch(t, expected, quit, "quit apps mismatch")
}

func TestApps_PlayGames_BlockedAndForceKick(t *testing.T) {
	t.Parallel()

	t.Run("no_force_kick", func(t *testing.T) {
		t.Parallel()
		a, ictx := setup(t)
		ctx := t.Context()

		a.mu.Lock()
		a.playingBlocked = true
		a.mu.Unlock()

		err := a.PlayGames(ctx, []uint32{AppidTf2}, false)
		require.NoError(t, err)

		req := &pb.CMsgClientKickPlayingSession{}
		lastCall := ictx.MockService().GetLastCall(req)

		if lastCall != nil {
			assert.NotEqual(t, enums.EMsg_ClientKickPlayingSession.String(), lastCall.Target().String())
		}
	})

	t.Run("force_kick_success", func(t *testing.T) {
		t.Parallel()
		a, ictx := setup(t)
		ctx := t.Context()

		a.mu.Lock()
		a.playingBlocked = true
		a.mu.Unlock()

		err := a.PlayGames(ctx, []uint32{AppidTf2}, true)
		require.NoError(t, err)

		calls := ictx.MockService().Calls

		foundKick := false
		for _, c := range calls {
			if c.Target().String() == enums.EMsg_ClientKickPlayingSession.String() {
				foundKick = true
				break
			}
		}

		assert.True(t, foundKick, "expected ClientKickPlayingSession to be called")
	})

	t.Run("force_kick_error_fallback", func(t *testing.T) {
		t.Parallel()
		a, ictx := setup(t)
		ctx := t.Context()

		a.mu.Lock()
		a.playingBlocked = true
		a.mu.Unlock()

		ictx.MockService().ResponseErrs[enums.EMsg_ClientKickPlayingSession.String()] = errors.New("kick failed")
		err := a.PlayGames(ctx, []uint32{AppidTf2}, true)
		require.NoError(t, err, "PlayGames should succeed even if KickPlayingSession logs an error")
	})
}

func TestApps_PlayCustomGames(t *testing.T) {
	t.Parallel()

	a, ictx := setup(t)
	ctx := t.Context()
	gameNames := []string{"G-man Bot", "Trading"}

	err := a.PlayCustomGames(ctx, gameNames)
	require.NoError(t, err)

	req := &pb.CMsgClientGamesPlayed{}
	ictx.MockService().GetLastCall(req)

	require.Len(t, req.GetGamesPlayed(), len(gameNames))

	for i, name := range gameNames {
		game := req.GetGamesPlayed()[i]
		assert.Equal(t, uint64(NonSteamGameID), game.GetGameId())
		assert.Equal(t, name, game.GetGameExtraInfo())
	}
}

func TestApps_Errors(t *testing.T) {
	t.Parallel()

	t.Run("play_games_send_error", func(t *testing.T) {
		t.Parallel()
		a, ictx := setup(t)
		ctx := t.Context()

		ictx.MockService().ResponseErrs[enums.EMsg_ClientGamesPlayedWithDataBlob.String()] = errors.New(
			"socket disconnected",
		)

		err := a.PlayGames(ctx, []uint32{AppidTf2}, false)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to update playing status")
		assert.Contains(t, err.Error(), "socket disconnected")
	})

	t.Run("kick_playing_session_send_error", func(t *testing.T) {
		t.Parallel()
		a, ictx := setup(t)
		ctx := t.Context()

		ictx.MockService().ResponseErrs[enums.EMsg_ClientKickPlayingSession.String()] = errors.New("socket timeout")

		err := a.KickPlayingSession(ctx)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "socket timeout")
	})
}

func TestApps_HandleLicenseList(t *testing.T) {
	t.Parallel()

	t.Run("success", func(t *testing.T) {
		t.Parallel()
		a, ictx := setup(t)

		sub := ictx.Bus().Subscribe(&LicensesEvent{})
		defer sub.Unsubscribe()

		ictx.EmitPacket(t, enums.EMsg_ClientLicenseList, &pb.CMsgClientLicenseList{
			Eresult: proto.Int32(int32(enums.EResult_OK)),
			Licenses: []*pb.CMsgClientLicenseList_License{
				{
					PackageId:   proto.Uint32(1001),
					TimeCreated: proto.Uint32(123456),
				},
			},
		})

		licenses := a.GetLicenses()
		require.Len(t, licenses, 1)
		assert.Equal(t, uint32(1001), licenses[0].GetPackageId())

		select {
		case ev := <-sub.C():
			e := ev.(*LicensesEvent)
			require.Len(t, e.Licenses, 1)
			assert.Equal(t, uint32(1001), e.Licenses[0].GetPackageId())
		case <-time.After(100 * time.Millisecond):
			t.Fatal("Event not received")
		}
	})

	t.Run("error_unmarshal", func(t *testing.T) {
		t.Parallel()
		a, _ := setup(t)
		a.handleLicenseList(&protocol.Packet{
			EMsg:    enums.EMsg_ClientLicenseList,
			Payload: []byte{0xFF}, // invalid proto
		})
	})
}

func TestApps_HandleGameConnectTokens(t *testing.T) {
	t.Parallel()

	t.Run("success", func(t *testing.T) {
		t.Parallel()
		a, ictx := setup(t)

		sub := ictx.Bus().Subscribe(&GameConnectTokensEvent{})
		defer sub.Unsubscribe()

		ictx.EmitPacket(t, enums.EMsg_ClientGameConnectTokens, &pb.CMsgClientGameConnectTokens{
			MaxTokensToKeep: proto.Uint32(2),
			Tokens: [][]byte{
				[]byte("token1"),
				[]byte("token2"),
				[]byte("token3"),
			},
		})

		tokens := a.GetConnectTokens()
		assert.Len(t, tokens, 2)
		assert.Equal(t, []byte("token2"), tokens[0])
		assert.Equal(t, []byte("token3"), tokens[1])

		assert.Equal(t, []byte("token2"), a.PopConnectToken())
		assert.Equal(t, []byte("token3"), a.PopConnectToken())
		assert.Nil(t, a.PopConnectToken())

		select {
		case ev := <-sub.C():
			e := ev.(*GameConnectTokensEvent)
			assert.Len(t, e.Tokens, 3)
		case <-time.After(100 * time.Millisecond):
			t.Fatal("Event not received")
		}
	})

	t.Run("no_max_keep_limit", func(t *testing.T) {
		t.Parallel()
		a, ictx := setup(t)
		ictx.EmitPacket(t, enums.EMsg_ClientGameConnectTokens, &pb.CMsgClientGameConnectTokens{
			MaxTokensToKeep: proto.Uint32(0), // no limit
			Tokens: [][]byte{
				[]byte("token1"),
			},
		})
		assert.Len(t, a.GetConnectTokens(), 1)
	})

	t.Run("error_unmarshal", func(t *testing.T) {
		t.Parallel()
		a, _ := setup(t)
		a.handleGameConnectTokens(&protocol.Packet{
			EMsg:    enums.EMsg_ClientGameConnectTokens,
			Payload: []byte{0xFF}, // invalid proto
		})
	})
}

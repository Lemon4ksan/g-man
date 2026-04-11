// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package apps

import (
	"reflect"
	"testing"
	"time"

	"google.golang.org/protobuf/proto"

	"github.com/lemon4ksan/g-man/pkg/bus"
	pb "github.com/lemon4ksan/g-man/pkg/protobuf/steam"
	"github.com/lemon4ksan/g-man/pkg/steam/protocol/enums"
	"github.com/lemon4ksan/g-man/test/module"
)

const (
	AppID_TF2 = 440
	AppID_CS2 = 730
)

func setup(t *testing.T) (*Apps, *module.InitContext) {
	t.Helper()

	a := New()
	ictx := module.NewInitContext()

	if err := a.Init(ictx); err != nil {
		t.Fatalf("failed to init apps module: %v", err)
	}

	t.Cleanup(func() {
		_ = a.Close()
	})

	return a, ictx
}

func TestApps_InitAndClose(t *testing.T) {
	a := New()
	ictx := module.NewInitContext()

	if a.Name() != ModuleName {
		t.Errorf("expected module name %s, got %s", ModuleName, a.Name())
	}

	if err := a.Init(ictx); err != nil {
		t.Fatalf("Init failed: %v", err)
	}

	ictx.AssertPacketHandlerRegistered(t, enums.EMsg_ClientPlayingSessionState)

	if err := a.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}

	ictx.AssertPacketHandlerUnregistered(t, enums.EMsg_ClientPlayingSessionState)
}

func TestApps_GetPlayerCount(t *testing.T) {
	a, ictx := setup(t)

	tests := []struct {
		name      string
		eresult   enums.EResult
		mockCount int32
		wantErr   bool
		wantCount int32
	}{
		{
			name:      "Success",
			eresult:   enums.EResult_OK,
			mockCount: 100500,
			wantCount: 100500,
		},
		{
			name:    "Access Denied",
			eresult: enums.EResult_AccessDenied,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ictx.MockServiceAccessor().SetLegacyResponse(
				enums.EMsg_ClientGetNumberOfCurrentPlayersDP,
				&pb.CMsgDPGetNumberOfCurrentPlayersResponse{
					Eresult:     proto.Int32(int32(tt.eresult)),
					PlayerCount: proto.Int32(tt.mockCount),
				},
			)

			count, err := a.GetPlayerCount(t.Context(), AppID_TF2)
			if (err != nil) != tt.wantErr {
				t.Fatalf("GetPlayerCount() error = %v, wantErr %v", err, tt.wantErr)
			}

			if count != tt.wantCount {
				t.Errorf("expected count %d, got %d", tt.wantCount, count)
			}
		})
	}
}

func TestApps_HandlePlayingSessionState(t *testing.T) {
	a, ictx := setup(t)
	subState := ictx.Bus().Subscribe(&PlayingStateEvent{})

	ictx.EmitPacket(t, enums.EMsg_ClientPlayingSessionState, &pb.CMsgClientPlayingSessionState{
		PlayingBlocked: proto.Bool(true),
		PlayingApp:     proto.Uint32(AppID_CS2),
	})

	a.mu.RLock()
	isBlocked := a.playingBlocked
	a.mu.RUnlock()

	if !isBlocked {
		t.Error("module state should be 'blocked' after receiving packet")
	}

	select {
	case ev := <-subState.C():
		event := ev.(*PlayingStateEvent)
		if !event.Blocked || event.PlayingApp != AppID_CS2 {
			t.Errorf("unexpected event data: %+v", event)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for PlayingStateEvent")
	}
}

func TestApps_PlayGames_Sequence(t *testing.T) {
	a, ictx := setup(t)

	subL := ictx.Bus().Subscribe(&AppLaunchedEvent{})
	subQ := ictx.Bus().Subscribe(&AppQuitEvent{})

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

	_ = a.PlayGames(t.Context(), []uint32{AppID_TF2}, false)
	_ = a.PlayGames(t.Context(), []uint32{AppID_TF2, AppID_CS2}, false)
	_ = a.PlayGames(t.Context(), []uint32{AppID_CS2}, false)
	_ = a.StopPlaying(t.Context())

	launched := collectIDs(subL.C(), 2)
	quit := collectIDs(subQ.C(), 2)

	expected := []uint32{AppID_TF2, AppID_CS2}
	if !reflect.DeepEqual(launched, expected) {
		t.Errorf("launched apps mismatch: want %v, got %v", expected, launched)
	}

	if !reflect.DeepEqual(quit, expected) {
		t.Errorf("quit apps mismatch: want %v, got %v", expected, quit)
	}
}

func TestApps_PlayCustomGames(t *testing.T) {
	a, ictx := setup(t)
	gameNames := []string{"G-man Bot", "Trading"}

	err := a.PlayCustomGames(t.Context(), gameNames)
	if err != nil {
		t.Fatalf("PlayCustomGames failed: %v", err)
	}

	req := &pb.CMsgClientGamesPlayed{}
	ictx.MockServiceAccessor().GetLastCall(req)

	if len(req.GetGamesPlayed()) != len(gameNames) {
		t.Fatalf("expected %d games in request, got %d", len(gameNames), len(req.GetGamesPlayed()))
	}

	for i, name := range gameNames {
		extraInfo := req.GetGamesPlayed()[i].GetGameExtraInfo()
		if extraInfo != name {
			t.Errorf("game %d: expected name %q, got %q", i, name, extraInfo)
		}
	}
}

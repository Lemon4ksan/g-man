// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package friends

import (
	"fmt"
	"io"
	"net/url"
	"testing"
	"time"

	pb "github.com/lemon4ksan/g-man/pkg/protobuf/steam"
	"github.com/lemon4ksan/g-man/pkg/steam/community"
	"github.com/lemon4ksan/g-man/pkg/steam/id"
	"github.com/lemon4ksan/g-man/pkg/steam/protocol/enums"
	"github.com/lemon4ksan/g-man/test/module"
	"google.golang.org/protobuf/proto"
)

const (
	FriendID_1 = 101
	FriendID_2 = 102
	BotSteamID = 76561198000000000
)

func setupFriends(t *testing.T) (*Manager, *module.InitContext) {
	t.Helper()
	m := New()
	ictx := module.NewInitContext()

	if err := m.Init(ictx); err != nil {
		t.Fatalf("failed to init friends: %v", err)
	}

	_ = m.StartAuthed(t.Context(), module.NewAuthContext(BotSteamID))

	t.Cleanup(func() {
		_ = m.Close()
	})

	return m, ictx
}

func TestManager_InitAndClose(t *testing.T) {
	m := New()
	ictx := module.NewInitContext()

	t.Run("Register", func(t *testing.T) {
		_ = m.Init(ictx)
		ictx.AssertPacketHandlerRegistered(t, enums.EMsg_ClientFriendsList)
		ictx.AssertPacketHandlerRegistered(t, enums.EMsg_ClientPersonaState)
	})

	t.Run("Unregister", func(t *testing.T) {
		_ = m.Close()
		ictx.AssertPacketHandlerUnregistered(t, enums.EMsg_ClientFriendsList)
	})
}

func TestManager_FriendCache(t *testing.T) {
	m, _ := setupFriends(t)

	m.relationships[FriendID_1] = enums.EFriendRelationship_Friend
	m.relationships[FriendID_2] = enums.EFriendRelationship_RequestRecipient
	m.users[FriendID_1] = &PersonaState{PlayerName: "G-man"}

	t.Run("Status Checks", func(t *testing.T) {
		if !m.IsFriend(FriendID_1) {
			t.Error("expected IsFriend(FriendID_1) to be true")
		}
		if m.IsFriend(FriendID_2) {
			t.Error("RequestRecipient should not be considered a 'Friend'")
		}
	})

	t.Run("Getters", func(t *testing.T) {
		p := m.GetFriend(FriendID_1)
		if p == nil || p.PlayerName != "G-man" {
			t.Errorf("expected G-man, got %+v", p)
		}

		friends := m.GetFriends()
		if len(friends) != 1 || friends[0] != FriendID_1 {
			t.Errorf("expected [101], got %v", friends)
		}
	})
}

func TestManager_GetMaxFriends(t *testing.T) {
	m, ictx := setupFriends(t)

	ictx.MockService().SetJSONResponse("IPlayerService", "GetBadges", map[string]any{
		"response": map[string]any{
			"player_level": 100,
		},
	})

	max, err := m.GetMaxFriends(t.Context())
	if err != nil {
		t.Fatalf("failed: %v", err)
	}

	if max != 750 {
		t.Errorf("expected 750, got %d", max)
	}

	ictx.MockService().ClearCalls()
	maxCached, _ := m.GetMaxFriends(t.Context())

	if maxCached != 750 {
		t.Error("cache value mismatch")
	}
}

func TestManager_AddAndRemoveFriend(t *testing.T) {
	m, ictx := setupFriends(t)

	t.Run("Add", func(t *testing.T) {
		_ = m.AddFriend(t.Context(), FriendID_1)
		req := &pb.CMsgClientAddFriend{}
		ictx.MockService().GetLastCall(req)
		if req.GetSteamidToAdd() != FriendID_1 {
			t.Errorf("wrong steamid in AddFriend request: %d", req.GetSteamidToAdd())
		}
	})

	t.Run("Remove", func(t *testing.T) {
		_ = m.RemoveFriend(t.Context(), FriendID_1)
		req := &pb.CMsgClientRemoveFriend{}
		ictx.MockService().GetLastCall(req)
		if req.GetFriendid() != FriendID_1 {
			t.Errorf("wrong steamid in RemoveFriend request: %d", req.GetFriendid())
		}
	})
}

func TestManager_InviteToGroups(t *testing.T) {
	m, _ := setupFriends(t)

	authCtx := module.NewAuthContext(BotSteamID)
	comm := authCtx.MockCommunity()

	_ = m.StartAuthed(t.Context(), authCtx)

	inviteURL := community.BaseURL + "actions/GroupInvite"
	comm.SetJSONResponse(inviteURL, 200, map[string]any{"success": true})

	t.Run("Invite Friend", func(t *testing.T) {
		m.relationships[FriendID_1] = enums.EFriendRelationship_Friend

		m.InviteToGroups(t.Context(), FriendID_1, []uint64{999})

		last := comm.GetLastCall()
		if last == nil {
			t.Fatal("no HTTP call recorded")
		}

		data, _ := io.ReadAll(last.Body)
		body, _ := url.ParseQuery(string(data))
		if body.Get("invitee") != fmt.Sprint(FriendID_1) {
			t.Errorf("expected invitee=%d, got %s", FriendID_1, body.Get("invitee"))
		}
	})

	t.Run("Skip Non-Friend", func(t *testing.T) {
		comm.ClearCalls()

		m.InviteToGroups(t.Context(), 666, []uint64{999})

		if comm.CallsCount() > 0 {
			t.Error("should not send invite to someone who is not a friend")
		}
	})
}

func TestManager_HandleFriendsList(t *testing.T) {
	m, ictx := setupFriends(t)
	sub := ictx.Bus().Subscribe(&RelationshipChangedEvent{})

	m.relationships[FriendID_1] = enums.EFriendRelationship_Friend

	ictx.EmitPacket(t, enums.EMsg_ClientFriendsList, &pb.CMsgClientFriendsList{
		Friends: []*pb.CMsgClientFriendsList_Friend{
			{Ulfriendid: proto.Uint64(FriendID_1), Efriendrelationship: proto.Uint32(uint32(enums.EFriendRelationship_None))},
			{Ulfriendid: proto.Uint64(FriendID_2), Efriendrelationship: proto.Uint32(uint32(enums.EFriendRelationship_Friend))},
		},
	})

	events := make(map[id.ID]*RelationshipChangedEvent)
	for i := 0; i < 2; i++ {
		select {
		case ev := <-sub.C():
			e := ev.(*RelationshipChangedEvent)
			events[e.SteamID] = e
		case <-time.After(500 * time.Millisecond):
			t.Fatal("timed out waiting for RelationshipChangedEvent")
		}
	}

	if ev, ok := events[FriendID_1]; !ok || ev.New != enums.EFriendRelationship_None {
		t.Errorf("removal event failed: %+v", ev)
	}
	if ev, ok := events[FriendID_2]; !ok || ev.New != enums.EFriendRelationship_Friend {
		t.Errorf("addition event failed: %+v", ev)
	}

	if m.IsFriend(FriendID_1) || !m.IsFriend(FriendID_2) {
		t.Error("internal relationships map state is incorrect")
	}
}

func TestManager_HandlePersonaState(t *testing.T) {
	m, ictx := setupFriends(t)
	sub := ictx.Bus().Subscribe(&PersonaStateUpdatedEvent{})

	ictx.EmitPacket(t, enums.EMsg_ClientPersonaState, &pb.CMsgClientPersonaState{
		Friends: []*pb.CMsgClientPersonaState_Friend{
			{Friendid: proto.Uint64(FriendID_1), PlayerName: proto.String("Lemon")},
		},
	})

	select {
	case ev := <-sub.C():
		pev := ev.(*PersonaStateUpdatedEvent)
		if pev.SteamID != FriendID_1 || pev.State.PlayerName != "Lemon" {
			t.Errorf("invalid event: %+v", pev)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("PersonaStateUpdatedEvent not received")
	}

	if p := m.GetFriend(FriendID_1); p == nil || p.PlayerName != "Lemon" {
		t.Error("persona state was not cached")
	}
}

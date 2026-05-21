// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package friends

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"

	pb "github.com/lemon4ksan/g-man/pkg/protobuf/steam"
	"github.com/lemon4ksan/g-man/pkg/rest"
	"github.com/lemon4ksan/g-man/pkg/steam/community"
	"github.com/lemon4ksan/g-man/pkg/steam/id"
	"github.com/lemon4ksan/g-man/pkg/steam/protocol"
	"github.com/lemon4ksan/g-man/pkg/steam/protocol/enums"
	tc "github.com/lemon4ksan/g-man/test/community"
	"github.com/lemon4ksan/g-man/test/module"
)

const (
	FriendID1  = id.ID(101)
	FriendID2  = id.ID(102)
	BotSteamID = id.ID(76561198000000000)
)

func setupFriends(t *testing.T) (*Manager, *module.InitContext) {
	t.Helper()

	m := New()
	ictx := module.NewInitContext()
	require.NoError(t, m.Init(ictx))

	auth := module.NewAuthContext(BotSteamID)
	require.NoError(t, m.StartAuthed(context.Background(), auth))

	t.Cleanup(func() { _ = m.Close() })

	return m, ictx
}

func TestManager_InitAndClose(t *testing.T) {
	m := New()
	ictx := module.NewInitContext()

	assert.Equal(t, ModuleName, m.Name())

	err := m.Init(ictx)
	require.NoError(t, err)
	ictx.AssertPacketHandlerRegistered(t, enums.EMsg_ClientFriendsList)

	err = m.Close()
	require.NoError(t, err)
	ictx.AssertPacketHandlerUnregistered(t, enums.EMsg_ClientFriendsList)
}

func TestManager_FriendCache(t *testing.T) {
	m, _ := setupFriends(t)

	m.mu.Lock()
	m.relationships[FriendID1] = enums.EFriendRelationship_Friend
	m.relationships[FriendID2] = enums.EFriendRelationship_RequestRecipient
	m.users[FriendID1] = &PersonaState{PlayerName: "G-man"}
	m.mu.Unlock()

	t.Run("Status Checks", func(t *testing.T) {
		assert.True(t, m.IsFriend(FriendID1))
		assert.False(t, m.IsFriend(FriendID2))
	})

	t.Run("Getters", func(t *testing.T) {
		p := m.GetFriend(FriendID1)
		assert.NotNil(t, p)
		assert.Equal(t, "G-man", p.PlayerName)

		friends := m.GetFriends()
		assert.ElementsMatch(t, []id.ID{FriendID1}, friends)
	})
}

func TestManager_GetMaxFriends(t *testing.T) {
	m, ictx := setupFriends(t)
	ctx := context.Background()

	t.Run("API Success", func(t *testing.T) {
		ictx.MockService().SetJSONResponse("IPlayerService", "GetBadges", map[string]any{
			"response": map[string]any{"player_level": 10},
		})

		max, err := m.GetMaxFriends(ctx)
		require.NoError(t, err)
		assert.Equal(t, 300, max)
	})

	t.Run("API Error", func(t *testing.T) {
		m.mu.Lock()
		m.maxFriends = 0 // Clear cache to force API call
		m.mu.Unlock()

		// Key must be Interface/Method for service calls in the mock
		ictx.MockService().ResponseErrs["IPlayerService/GetBadges"] = errors.New("api down")

		_, err := m.GetMaxFriends(ctx)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "api down")
	})
}

func TestManager_AddAndRemoveFriend(t *testing.T) {
	m, ictx := setupFriends(t)
	ctx := context.Background()

	t.Run("Add", func(t *testing.T) {
		err := m.AddFriend(ctx, uint64(FriendID1))
		assert.NoError(t, err)

		req := &pb.CMsgClientAddFriend{}
		ictx.MockService().GetLastCall(req)
		assert.Equal(t, uint64(FriendID1), req.GetSteamidToAdd())
	})

	t.Run("Remove", func(t *testing.T) {
		err := m.RemoveFriend(ctx, uint64(FriendID1))
		assert.NoError(t, err)

		req := &pb.CMsgClientRemoveFriend{}
		ictx.MockService().GetLastCall(req)
		assert.Equal(t, uint64(FriendID1), req.GetFriendid())
	})
}

func TestManager_InviteToGroups(t *testing.T) {
	m, _ := setupFriends(t)
	ctx := context.Background()
	// Ensure we use the community mock instance linked to the manager's state
	comm := tc.New()
	m.community = comm
	path := "actions/GroupInvite"

	t.Run("Skip Non-Friend", func(t *testing.T) {
		comm.ClearCalls()
		m.InviteToGroups(ctx, FriendID2, []uint64{999})
		assert.Equal(t, 0, comm.CallsCount())
	})

	t.Run("Success and Ignore 400", func(t *testing.T) {
		m.mu.Lock()
		m.relationships[FriendID1] = enums.EFriendRelationship_Friend
		m.mu.Unlock()

		comm.ClearCalls()
		comm.SetJSONResponse(community.BaseURL+path, 200, map[string]any{"success": true})

		comm.ResponseErrs[path] = &rest.APIError{StatusCode: 400, Body: []byte("already in group")}

		m.InviteToGroups(ctx, FriendID1, []uint64{1001, 1002})

		assert.Equal(t, 2, comm.CallsCount())
	})
}

func TestManager_HandleFriendsList(t *testing.T) {
	m, ictx := setupFriends(t)

	t.Run("Unmarshal Error", func(t *testing.T) {
		m.handleFriendsList(&protocol.Packet{Payload: []byte{0xFF, 0xEE}})
		// Should log and return
	})

	t.Run("Relationship Changes", func(t *testing.T) {
		sub := ictx.Bus().Subscribe(&RelationshipChangedEvent{})

		ictx.EmitPacket(t, enums.EMsg_ClientFriendsList, &pb.CMsgClientFriendsList{
			Friends: []*pb.CMsgClientFriendsList_Friend{
				{
					Ulfriendid:          proto.Uint64(uint64(FriendID1)),
					Efriendrelationship: proto.Uint32(uint32(enums.EFriendRelationship_Friend)),
				},
				{
					Ulfriendid:          proto.Uint64(uint64(FriendID1)), // No change
					Efriendrelationship: proto.Uint32(uint32(enums.EFriendRelationship_Friend)),
				},
			},
		})

		select {
		case ev := <-sub.C():
			e := ev.(*RelationshipChangedEvent)
			assert.Equal(t, FriendID1, e.SteamID)
			assert.Equal(t, enums.EFriendRelationship_Friend, e.New)
		case <-time.After(100 * time.Millisecond):
			t.Fatal("Event not received")
		}

		// Ensure only one event was fired (since second friend update had no change)
		assert.Empty(t, sub.C())
	})
}

func TestManager_HandlePersonaState(t *testing.T) {
	m, ictx := setupFriends(t)

	t.Run("Unmarshal Error", func(t *testing.T) {
		m.handlePersonaState(&protocol.Packet{Payload: []byte{0xFF}})
	})

	t.Run("State Updates", func(t *testing.T) {
		sub := ictx.Bus().Subscribe(&PersonaStateUpdatedEvent{})

		ictx.EmitPacket(t, enums.EMsg_ClientPersonaState, &pb.CMsgClientPersonaState{
			Friends: []*pb.CMsgClientPersonaState_Friend{
				{
					Friendid:   proto.Uint64(uint64(FriendID1)),
					PlayerName: proto.String("New Name"),
					AvatarHash: []byte("abc"),
				},
				{
					Friendid:   proto.Uint64(uint64(FriendID1)), // Update existing
					PlayerName: proto.String("Updated Name"),
				},
				{
					Friendid: proto.Uint64(uint64(FriendID2)), // New user, missing fields
				},
			},
		})

		// Check Friend 1
		p1 := m.GetFriend(FriendID1)
		assert.Equal(t, "Updated Name", p1.PlayerName)
		assert.Equal(t, []byte("abc"), p1.AvatarHash)

		// Check Friend 2
		p2 := m.GetFriend(FriendID2)
		assert.NotNil(t, p2)
		assert.Empty(t, p2.PlayerName)

		// Verify events were fired
		assert.Len(t, sub.C(), 3)
	})
}

// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package friends

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/lemon4ksan/aoni"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"

	pb "github.com/lemon4ksan/g-man/pkg/protobuf/steam"
	"github.com/lemon4ksan/g-man/pkg/steam/community"
	"github.com/lemon4ksan/g-man/pkg/steam/id"
	"github.com/lemon4ksan/g-man/pkg/steam/protocol"
	"github.com/lemon4ksan/g-man/pkg/steam/protocol/enums"
	"github.com/lemon4ksan/g-man/test/mock"
)

const (
	FriendID1  = id.ID(101)
	FriendID2  = id.ID(102)
	BotSteamID = id.ID(76561198000000000)
)

func setupFriends(t *testing.T) (*Manager, *mock.InitContext) {
	t.Helper()

	m := New()
	ictx := mock.NewInitContext()
	require.NoError(t, m.Init(ictx))

	auth := mock.NewAuthContext(BotSteamID)
	require.NoError(t, m.StartAuthed(context.Background(), auth))

	t.Cleanup(func() { _ = m.Close() })

	return m, ictx
}

func TestManager_InitAndClose(t *testing.T) {
	m := New()
	ictx := mock.NewInitContext()

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
	comm := mock.NewHTTPStub()
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

		comm.ResponseErrs[path] = &aoni.APIError{StatusCode: 400, Body: []byte("already in group")}

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

func TestManager_AcceptFriendRequestWeb(t *testing.T) {
	m, _ := setupFriends(t)
	ctx := context.Background()
	comm := mock.NewHTTPStub()
	m.community = comm

	path := "actions/AddFriendAjax"

	t.Run("Success", func(t *testing.T) {
		comm.ClearCalls()
		comm.SetJSONResponse(community.BaseURL+path, 200, map[string]any{"success": true})

		err := m.AcceptFriendRequestWeb(ctx, FriendID1)
		require.NoError(t, err)

		params := comm.GetLastCallParams()
		assert.Equal(t, "1", params.Get("accept_invite"))
		assert.Equal(t, comm.MockSessionID, params.Get("sessionid"))
		assert.Equal(t, FriendID1.String(), params.Get("steamid"))
	})

	t.Run("Unsuccessful", func(t *testing.T) {
		comm.ClearCalls()
		comm.SetJSONResponse(community.BaseURL+path, 200, map[string]any{"success": false})

		err := m.AcceptFriendRequestWeb(ctx, FriendID1)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "web accept request unsuccessful")
	})

	t.Run("HTTP Error", func(t *testing.T) {
		comm.ClearCalls()
		comm.ResponseErrs[community.BaseURL+path] = errors.New("network down")

		err := m.AcceptFriendRequestWeb(ctx, FriendID1)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "network down")
	})

	t.Run("Uninitialized community", func(t *testing.T) {
		m2 := New()
		err := m2.AcceptFriendRequestWeb(ctx, FriendID1)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "community requester is not initialized")
	})
}

func TestManager_BlockCommunication(t *testing.T) {
	m, _ := setupFriends(t)
	ctx := context.Background()
	comm := mock.NewHTTPStub()
	m.community = comm

	path := "actions/BlockUserAjax"

	t.Run("Success", func(t *testing.T) {
		comm.ClearCalls()
		comm.SetJSONResponse(community.BaseURL+path, 200, map[string]any{"success": true})

		err := m.BlockCommunication(ctx, FriendID1)
		require.NoError(t, err)

		params := comm.GetLastCallParams()
		assert.Equal(t, comm.MockSessionID, params.Get("sessionid"))
		assert.Equal(t, FriendID1.String(), params.Get("steamid"))
	})

	t.Run("Unsuccessful", func(t *testing.T) {
		comm.ClearCalls()
		comm.SetJSONResponse(community.BaseURL+path, 200, map[string]any{"success": false})

		err := m.BlockCommunication(ctx, FriendID1)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "block user request unsuccessful")
	})

	t.Run("HTTP Error", func(t *testing.T) {
		comm.ClearCalls()
		comm.ResponseErrs[community.BaseURL+path] = errors.New("http block error")

		err := m.BlockCommunication(ctx, FriendID1)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "http block error")
	})

	t.Run("Uninitialized community", func(t *testing.T) {
		m2 := New()
		err := m2.BlockCommunication(ctx, FriendID1)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "community requester is not initialized")
	})
}

func TestManager_UnblockCommunication(t *testing.T) {
	m, _ := setupFriends(t)
	ctx := context.Background()
	comm := mock.NewHTTPStub()
	m.community = comm

	path := fmt.Sprintf("profiles/%s/friends/blocked", m.mySteamID)

	t.Run("Success", func(t *testing.T) {
		comm.ClearCalls()
		comm.SetRawResponse(community.BaseURL+path, 200, []byte("OK"))

		err := m.UnblockCommunication(ctx, FriendID1)
		require.NoError(t, err)

		params := comm.GetLastCallParams()
		assert.Equal(t, "unignore", params.Get("action"))
		assert.Equal(t, "1", params.Get("friends["+FriendID1.String()+"]"))
		assert.Equal(t, comm.MockSessionID, params.Get("sessionid"))
	})

	t.Run("HTTP Status Error", func(t *testing.T) {
		comm.ClearCalls()
		comm.SetRawResponse(community.BaseURL+path, 400, []byte("Bad Request"))

		err := m.UnblockCommunication(ctx, FriendID1)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "unblock failed with HTTP status: 400")
	})

	t.Run("HTTP Error", func(t *testing.T) {
		comm.ClearCalls()
		comm.ResponseErrs[community.BaseURL+path] = errors.New("http unblock error")

		err := m.UnblockCommunication(ctx, FriendID1)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "http unblock error")
	})

	t.Run("Uninitialized community", func(t *testing.T) {
		m2 := New()
		err := m2.UnblockCommunication(ctx, FriendID1)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "community requester is not initialized")
	})
}

func TestManager_PostUserComment(t *testing.T) {
	m, _ := setupFriends(t)
	ctx := context.Background()
	comm := mock.NewHTTPStub()
	m.community = comm

	path := "comment/Profile/post/101/-1"

	t.Run("Success", func(t *testing.T) {
		comm.ClearCalls()
		comm.SetJSONResponse(community.BaseURL+path, 200, map[string]any{
			"success":       true,
			"comments_html": `<div class="commentthread_comments"><div class="commentthread_comment" id="comment_987654321"></div></div>`,
		})

		commentID, err := m.PostUserComment(ctx, FriendID1, "cool profile!")
		require.NoError(t, err)
		assert.Equal(t, "987654321", commentID)

		params := comm.GetLastCallParams()
		assert.Equal(t, "cool profile!", params.Get("comment"))
		assert.Equal(t, "1", params.Get("count"))
		assert.Equal(t, comm.MockSessionID, params.Get("sessionid"))
	})

	t.Run("Unsuccessful with error msg", func(t *testing.T) {
		comm.ClearCalls()
		comm.SetJSONResponse(community.BaseURL+path, 200, map[string]any{
			"success": false,
			"error":   "Rate limit exceeded",
		})

		_, err := m.PostUserComment(ctx, FriendID1, "cool profile!")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "post comment failed: Rate limit exceeded")
	})

	t.Run("Unsuccessful unknown error", func(t *testing.T) {
		comm.ClearCalls()
		comm.SetJSONResponse(community.BaseURL+path, 200, map[string]any{
			"success": false,
		})

		_, err := m.PostUserComment(ctx, FriendID1, "cool profile!")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "post comment failed: unknown error")
	})

	t.Run("HTML Missing Comment Element", func(t *testing.T) {
		comm.ClearCalls()
		comm.SetJSONResponse(community.BaseURL+path, 200, map[string]any{
			"success":       true,
			"comments_html": `<div>No comment here</div>`,
		})

		_, err := m.PostUserComment(ctx, FriendID1, "cool profile!")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "new comment not found in returned HTML")
	})

	t.Run("HTML Comment ID Missing Attribute", func(t *testing.T) {
		comm.ClearCalls()
		comm.SetJSONResponse(community.BaseURL+path, 200, map[string]any{
			"success":       true,
			"comments_html": `<div class="commentthread_comment"></div>`,
		})

		_, err := m.PostUserComment(ctx, FriendID1, "cool profile!")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "new comment missing id attribute")
	})

	t.Run("HTML Comment ID Bad Format", func(t *testing.T) {
		comm.ClearCalls()
		comm.SetJSONResponse(community.BaseURL+path, 200, map[string]any{
			"success":       true,
			"comments_html": `<div class="commentthread_comment" id="comment"></div>`,
		})

		_, err := m.PostUserComment(ctx, FriendID1, "cool profile!")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "invalid comment element id format")
	})

	t.Run("HTTP Error", func(t *testing.T) {
		comm.ClearCalls()
		comm.ResponseErrs[community.BaseURL+path] = errors.New("post error")

		_, err := m.PostUserComment(ctx, FriendID1, "cool profile!")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "post error")
	})

	t.Run("Uninitialized community", func(t *testing.T) {
		m2 := New()
		_, err := m2.PostUserComment(ctx, FriendID1, "cool profile!")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "community requester is not initialized")
	})
}

func TestManager_DeleteUserComment(t *testing.T) {
	m, _ := setupFriends(t)
	ctx := context.Background()
	comm := mock.NewHTTPStub()
	m.community = comm

	path := "comment/Profile/delete/101/-1"

	t.Run("Success", func(t *testing.T) {
		comm.ClearCalls()
		comm.SetJSONResponse(community.BaseURL+path, 200, map[string]any{
			"success":       true,
			"comments_html": `<div>Comment deleted successfully</div>`,
		})

		err := m.DeleteUserComment(ctx, FriendID1, "987654321")
		require.NoError(t, err)

		params := comm.GetLastCallParams()
		assert.Equal(t, "987654321", params.Get("gidcomment"))
		assert.Equal(t, "0", params.Get("start"))
		assert.Equal(t, "1", params.Get("count"))
		assert.Equal(t, comm.MockSessionID, params.Get("sessionid"))
		assert.Equal(t, "-1", params.Get("feature2"))
	})

	t.Run("Unsuccessful with comment ID still in HTML", func(t *testing.T) {
		comm.ClearCalls()
		comm.SetJSONResponse(community.BaseURL+path, 200, map[string]any{
			"success":       true,
			"comments_html": `<div id="comment_987654321">Failed to delete</div>`,
		})

		err := m.DeleteUserComment(ctx, FriendID1, "987654321")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to delete comment (comment still in HTML)")
	})

	t.Run("Unsuccessful response", func(t *testing.T) {
		comm.ClearCalls()
		comm.SetJSONResponse(community.BaseURL+path, 200, map[string]any{
			"success": false,
			"error":   "Not authorized",
		})

		err := m.DeleteUserComment(ctx, FriendID1, "987654321")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "delete comment failed: Not authorized")
	})

	t.Run("HTTP Error", func(t *testing.T) {
		comm.ClearCalls()
		comm.ResponseErrs[community.BaseURL+path] = errors.New("delete error")

		err := m.DeleteUserComment(ctx, FriendID1, "987654321")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "delete error")
	})

	t.Run("Uninitialized community", func(t *testing.T) {
		m2 := New()
		err := m2.DeleteUserComment(ctx, FriendID1, "987654321")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "community requester is not initialized")
	})
}

func TestManager_GetUserComments(t *testing.T) {
	m, _ := setupFriends(t)
	ctx := context.Background()
	comm := mock.NewHTTPStub()
	m.community = comm

	path := "comment/Profile/render/101/-1"

	t.Run("Success", func(t *testing.T) {
		comm.ClearCalls()

		html := `<div class="commentthread_comments">
			<div class="commentthread_comment responsive_body_text" id="comment_555">
				<div class="playerAvatar">
					<img src="https://avatar.url/avatar5.jpg">
				</div>
				<div class="commentthread_comment_avatar">
					<a href="https://steamcommunity.com/profiles/76561198000000005" data-miniprofile="5"></a>
				</div>
				<bdi>User 5</bdi>
				<span class="commentthread_comment_timestamp" data-timestamp="1621234567"></span>
				<div class="commentthread_comment_text">
					Excellent trader!
				</div>
			</div>
			<div class="commentthread_comment responsive_body_text" id="invalid">
				<div class="playerAvatar">
					<img src="https://avatar.url/avatar6.jpg">
				</div>
				<bdi>User 6</bdi>
				<div class="commentthread_comment_text">
					Bad ID format
				</div>
			</div>
		</div>`

		comm.SetJSONResponse(community.BaseURL+path, 200, map[string]any{
			"success":       true,
			"total_count":   15,
			"comments_html": html,
		})

		comments, totalCount, err := m.GetUserComments(ctx, FriendID1, 0, 5)
		require.NoError(t, err)
		assert.Equal(t, 15, totalCount)
		require.Len(t, comments, 1)

		c := comments[0]
		assert.Equal(t, "555", c.ID)
		assert.Equal(t, id.ID(76561197960265728+5), c.AuthorSteamID)
		assert.Equal(t, "User 5", c.AuthorName)
		assert.Equal(t, "https://avatar.url/avatar5.jpg", c.AuthorAvatar)
		assert.Equal(t, time.Unix(1621234567, 0).UTC(), c.Date)
		assert.Equal(t, "Excellent trader!", c.Text)

		params := comm.GetLastCallParams()
		assert.Equal(t, "0", params.Get("start"))
		assert.Equal(t, "5", params.Get("count"))
		assert.Equal(t, "-1", params.Get("feature2"))
		assert.Equal(t, comm.MockSessionID, params.Get("sessionid"))
	})

	t.Run("Unsuccessful response", func(t *testing.T) {
		comm.ClearCalls()
		comm.SetJSONResponse(community.BaseURL+path, 200, map[string]any{
			"success": false,
			"error":   "Internal error",
		})

		_, _, err := m.GetUserComments(ctx, FriendID1, 0, 5)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "render comments failed: Internal error")
	})

	t.Run("HTTP Error", func(t *testing.T) {
		comm.ClearCalls()
		comm.ResponseErrs[community.BaseURL+path] = errors.New("render error")

		_, _, err := m.GetUserComments(ctx, FriendID1, 0, 5)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "render error")
	})

	t.Run("Uninitialized community", func(t *testing.T) {
		m2 := New()
		_, _, err := m2.GetUserComments(ctx, FriendID1, 0, 5)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "community requester is not initialized")
	})
}

func TestManager_UploadRichPresence(t *testing.T) {
	m, ictx := setupFriends(t)
	ctx := context.Background()

	err := m.UploadRichPresence(ctx, 440, map[string]string{
		"steam_display": "#Status_AtMainMenu",
	})
	assert.NoError(t, err)

	req := &pb.CMsgClientRichPresenceUpload{}
	ictx.MockService().GetLastCall(req)

	// Verify that the payload contains Section "RP" and Key "steam_display"
	kv := req.GetRichPresenceKv()
	assert.NotEmpty(t, kv)
	assert.Contains(t, string(kv), "RP")
	assert.Contains(t, string(kv), "steam_display")
	assert.Contains(t, string(kv), "#Status_AtMainMenu")
}

func TestManager_SetUIMode(t *testing.T) {
	m, ictx := setupFriends(t)
	ctx := context.Background()

	err := m.SetUIMode(ctx, UIModeMobile)
	assert.NoError(t, err)

	req := &pb.CMsgClientUIMode{}
	ictx.MockService().GetLastCall(req)
	assert.Equal(t, UIModeMobile, req.GetUimode())
}

func TestManager_FriendInviteTokens(t *testing.T) {
	m, ictx := setupFriends(t)
	ctx := context.Background()

	t.Run("CreateFriendInviteToken Success", func(t *testing.T) {
		ictx.MockService().
			SetJSONResponse("UserAccount", "CreateFriendInviteToken", map[string]any{
				"response": map[string]any{
					"invite_token": "TOKEN_123",
				},
			})

		token, err := m.CreateFriendInviteToken(ctx, 5, 3600)
		require.NoError(t, err)
		assert.Equal(t, "TOKEN_123", token)

		req := ictx.MockService().GetLastRequest()
		assert.Equal(t, "5", req.Params().Get("invite_limit"))
		assert.Equal(t, "3600", req.Params().Get("invite_duration"))
	})

	t.Run("GetFriendInviteTokens Success", func(t *testing.T) {
		ictx.MockService().
			SetJSONResponse("UserAccount", "GetFriendInviteTokens", map[string]any{
				"response": map[string]any{
					"tokens": []map[string]any{
						{
							"invite_token": "TOKEN_ABC",
						},
					},
				},
			})

		tokens, err := m.GetFriendInviteTokens(ctx)
		require.NoError(t, err)
		require.Len(t, tokens, 1)
		assert.Equal(t, "TOKEN_ABC", tokens[0].GetInviteToken())
	})

	t.Run("RevokeFriendInviteToken Success", func(t *testing.T) {
		ictx.MockService().
			SetJSONResponse("UserAccount", "RevokeFriendInviteToken", map[string]any{
				"response": map[string]any{},
			})

		err := m.RevokeFriendInviteToken(ctx, "TOKEN_XYZ")
		assert.NoError(t, err)

		req := ictx.MockService().GetLastRequest()
		assert.Equal(t, "TOKEN_XYZ", req.Params().Get("invite_token"))
	})

	t.Run("ViewFriendInviteToken Success", func(t *testing.T) {
		ictx.MockService().
			SetJSONResponse("UserAccount", "ViewFriendInviteToken", map[string]any{
				"response": map[string]any{
					"valid": true,
				},
			})

		resp, err := m.ViewFriendInviteToken(ctx, 76561198000000001, "TOKEN_VIEW")
		require.NoError(t, err)
		assert.True(t, resp.GetValid())

		req := ictx.MockService().GetLastRequest()
		assert.Equal(t, "76561198000000001", req.Params().Get("steamid"))
		assert.Equal(t, "TOKEN_VIEW", req.Params().Get("invite_token"))
	})
}

func TestManager_HandleFriendsGroupsList(t *testing.T) {
	m, ictx := setupFriends(t)

	sub := ictx.Bus().Subscribe(&GroupListEvent{})

	ictx.EmitPacket(t, enums.EMsg_ClientFriendsGroupsList, &pb.CMsgClientFriendsGroupsList{
		Bremoval:     proto.Bool(false),
		Bincremental: proto.Bool(false),
		FriendGroups: []*pb.CMsgClientFriendsGroupsList_FriendGroup{
			{
				NGroupID:     proto.Int32(1),
				StrGroupName: proto.String("Co-workers"),
			},
		},
		Memberships: []*pb.CMsgClientFriendsGroupsList_FriendGroupsMembership{
			{
				UlSteamID: proto.Uint64(uint64(FriendID1)),
				NGroupID:  proto.Int32(1),
			},
		},
	})

	groups := m.GetFriendGroups()
	assert.Len(t, groups, 1)
	assert.Equal(t, "Co-workers", groups[1].Name)
	assert.Contains(t, groups[1].Members, FriendID1)

	select {
	case ev := <-sub.C():
		e := ev.(*GroupListEvent)
		assert.Len(t, e.Groups, 1)
		assert.Equal(t, "Co-workers", e.Groups[1].Name)
	case <-time.After(100 * time.Millisecond):
		t.Fatal("Event not received")
	}
}

func TestManager_HandlePlayerNicknameList(t *testing.T) {
	m, ictx := setupFriends(t)

	sub := ictx.Bus().Subscribe(&NicknameListEvent{})

	ictx.EmitPacket(t, enums.EMsg_ClientPlayerNicknameList, &pb.CMsgClientPlayerNicknameList{
		Removal:     proto.Bool(false),
		Incremental: proto.Bool(false),
		Nicknames: []*pb.CMsgClientPlayerNicknameList_PlayerNickname{
			{
				Steamid:  proto.Uint64(uint64(FriendID1)),
				Nickname: proto.String("Bob"),
			},
		},
	})

	assert.Equal(t, "Bob", m.GetNickname(FriendID1))
	assert.Len(t, m.GetNicknames(), 1)

	select {
	case ev := <-sub.C():
		e := ev.(*NicknameListEvent)
		assert.Equal(t, "Bob", e.Nicknames[FriendID1])
	case <-time.After(100 * time.Millisecond):
		t.Fatal("Event not received")
	}
}

func TestManager_HandleNotifyFriendNicknameChanged(t *testing.T) {
	m, ictx := setupFriends(t)
	m.relationships[FriendID1] = enums.EFriendRelationship_Friend

	sub := ictx.Bus().Subscribe(&NicknameChangedEvent{})

	handler, ok := ictx.GetServiceHandler("PlayerClient.NotifyFriendNicknameChanged#1")
	require.True(t, ok)

	payload, err := proto.Marshal(&pb.CPlayer_FriendNicknameChanged_Notification{
		Accountid: proto.Uint32(FriendID1.AccountID()),
		Nickname:  proto.String("Alice"),
	})
	require.NoError(t, err)

	handler(&protocol.Packet{
		Payload: payload,
	})

	assert.Equal(t, "Alice", m.GetNickname(FriendID1))

	select {
	case ev := <-sub.C():
		e := ev.(*NicknameChangedEvent)
		assert.Equal(t, FriendID1, e.SteamID)
		assert.Equal(t, "Alice", e.Nickname)
	case <-time.After(100 * time.Millisecond):
		t.Fatal("Event not received")
	}
}

func TestManager_SetFriendNickname(t *testing.T) {
	m, ictx := setupFriends(t)
	ctx := context.Background()

	t.Run("SetFriendNickname Success", func(t *testing.T) {
		ictx.MockService().SetLegacyResponse(
			enums.EMsg_AMClientSetPlayerNickname,
			&pb.CMsgClientSetPlayerNicknameResponse{
				Eresult: proto.Uint32(uint32(enums.EResult_OK)),
			},
		)

		err := m.SetFriendNickname(ctx, uint64(FriendID1), "Best Friend")
		assert.NoError(t, err)

		req := &pb.CMsgClientSetPlayerNickname{}
		ictx.MockService().GetLastCall(req)
		assert.Equal(t, uint64(FriendID1), req.GetSteamid())
		assert.Equal(t, "Best Friend", req.GetNickname())
	})

	t.Run("SetFriendNickname Steam Error", func(t *testing.T) {
		ictx.MockService().SetLegacyResponse(
			enums.EMsg_AMClientSetPlayerNickname,
			&pb.CMsgClientSetPlayerNicknameResponse{
				Eresult: proto.Uint32(uint32(enums.EResult_Fail)),
			},
		)

		err := m.SetFriendNickname(ctx, uint64(FriendID1), "Best Friend")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "steam error EResult 2")
	})
}

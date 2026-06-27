// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package friends

import (
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
	require.NoError(t, m.StartAuthed(t.Context(), auth))

	t.Cleanup(func() { _ = m.Close() })

	return m, ictx
}

func TestManager_InitAndClose(t *testing.T) {
	t.Parallel()

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
	t.Parallel()

	t.Run("status_checks", func(t *testing.T) {
		t.Parallel()
		m, _ := setupFriends(t)

		m.mu.Lock()
		m.relationships[FriendID1] = enums.EFriendRelationship_Friend
		m.relationships[FriendID2] = enums.EFriendRelationship_RequestRecipient
		m.mu.Unlock()

		assert.True(t, m.IsFriend(FriendID1))
		assert.False(t, m.IsFriend(FriendID2))
	})

	t.Run("getters", func(t *testing.T) {
		t.Parallel()
		m, _ := setupFriends(t)

		m.mu.Lock()
		m.relationships[FriendID1] = enums.EFriendRelationship_Friend
		m.users[FriendID1] = &PersonaState{PlayerName: "G-man"}
		m.mu.Unlock()

		p := m.GetFriend(FriendID1)
		assert.NotNil(t, p)
		assert.Equal(t, "G-man", p.PlayerName)

		frs := m.GetFriends()
		assert.ElementsMatch(t, []id.ID{FriendID1}, frs)
	})
}

func TestManager_GetMaxFriends(t *testing.T) {
	t.Parallel()

	t.Run("api_success", func(t *testing.T) {
		t.Parallel()
		m, ictx := setupFriends(t)

		ictx.MockService().SetJSONResponse("IPlayerService", "GetBadges", map[string]any{
			"response": map[string]any{"player_level": 10},
		})

		max, err := m.GetMaxFriends(t.Context())
		require.NoError(t, err)
		assert.Equal(t, 300, max)

		max2, err := m.GetMaxFriends(t.Context())
		require.NoError(t, err)
		assert.Equal(t, 300, max2)
	})

	t.Run("api_error", func(t *testing.T) {
		t.Parallel()
		m, ictx := setupFriends(t)
		m.mu.Lock()
		m.maxFriends = 0
		m.mu.Unlock()

		ictx.MockService().ResponseErrs["IPlayerService/GetBadges"] = errors.New("api down")

		_, err := m.GetMaxFriends(t.Context())
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "api down")
	})
}

func TestManager_AddAndRemoveFriend(t *testing.T) {
	t.Parallel()

	t.Run("add", func(t *testing.T) {
		t.Parallel()
		m, ictx := setupFriends(t)

		err := m.AddFriend(t.Context(), uint64(FriendID1))
		assert.NoError(t, err)

		req := &pb.CMsgClientAddFriend{}
		ictx.MockService().GetLastCall(req)
		assert.Equal(t, uint64(FriendID1), req.GetSteamidToAdd())
	})

	t.Run("remove", func(t *testing.T) {
		t.Parallel()
		m, ictx := setupFriends(t)

		err := m.RemoveFriend(t.Context(), uint64(FriendID1))
		assert.NoError(t, err)

		req := &pb.CMsgClientRemoveFriend{}
		ictx.MockService().GetLastCall(req)
		assert.Equal(t, uint64(FriendID1), req.GetFriendid())
	})

	t.Run("set_persona_with_name", func(t *testing.T) {
		t.Parallel()
		m, ictx := setupFriends(t)

		err := m.SetPersona(t.Context(), enums.EPersonaState_Online, "New Bot Name")
		assert.NoError(t, err)

		req := &pb.CMsgClientChangeStatus{}
		ictx.MockService().GetLastCall(req)
		assert.Equal(t, uint32(enums.EPersonaState_Online), req.GetPersonaState())
		assert.Equal(t, "New Bot Name", req.GetPlayerName())
	})

	t.Run("set_persona_empty_name", func(t *testing.T) {
		t.Parallel()
		m, ictx := setupFriends(t)

		err := m.SetPersona(t.Context(), enums.EPersonaState_Busy, "")
		assert.NoError(t, err)

		req := &pb.CMsgClientChangeStatus{}
		ictx.MockService().GetLastCall(req)
		assert.Equal(t, uint32(enums.EPersonaState_Busy), req.GetPersonaState())
		assert.Nil(t, req.PlayerName)
	})
}

func TestManager_InviteToGroups(t *testing.T) {
	t.Parallel()

	t.Run("skip_non_friend", func(t *testing.T) {
		t.Parallel()
		m, _ := setupFriends(t)
		comm := mock.NewHTTPStub()
		m.community = comm

		comm.ClearCalls()
		m.InviteToGroups(t.Context(), FriendID2, []uint64{999})
		assert.Equal(t, 0, comm.CallsCount())
	})

	t.Run("success_and_ignore_400", func(t *testing.T) {
		t.Parallel()
		m, _ := setupFriends(t)
		comm := mock.NewHTTPStub()
		m.community = comm
		path := "actions/GroupInvite"

		m.mu.Lock()
		m.relationships[FriendID1] = enums.EFriendRelationship_Friend
		m.mu.Unlock()

		comm.ClearCalls()
		comm.SetJSONResponse(community.BaseURL+path, 200, map[string]any{"success": true})
		comm.ResponseErrs[path] = &aoni.APIError{StatusCode: 400, Body: []byte("already in group")}

		m.InviteToGroups(t.Context(), FriendID1, []uint64{1001, 1002})

		assert.Equal(t, 2, comm.CallsCount())
	})

	t.Run("non_400_error_logged", func(t *testing.T) {
		t.Parallel()
		m, _ := setupFriends(t)
		comm := mock.NewHTTPStub()
		m.community = comm
		path := "actions/GroupInvite"

		m.mu.Lock()
		m.relationships[FriendID1] = enums.EFriendRelationship_Friend
		m.mu.Unlock()

		comm.ClearCalls()
		comm.ResponseErrs[community.BaseURL+path] = &aoni.APIError{StatusCode: 500, Body: []byte("internal error")}

		m.InviteToGroups(t.Context(), FriendID1, []uint64{1001})

		assert.Equal(t, 1, comm.CallsCount())
	})
}

func TestManager_HandleFriendsList(t *testing.T) {
	t.Parallel()

	t.Run("unmarshal_error", func(t *testing.T) {
		t.Parallel()
		m, _ := setupFriends(t)
		m.handleFriendsList(&protocol.Packet{Payload: []byte{0xFF, 0xEE}})
	})

	t.Run("relationship_changes", func(t *testing.T) {
		t.Parallel()
		_, ictx := setupFriends(t)

		sub := ictx.Bus().Subscribe(&RelationshipChangedEvent{})
		defer sub.Unsubscribe()

		ictx.EmitPacket(t, enums.EMsg_ClientFriendsList, &pb.CMsgClientFriendsList{
			Friends: []*pb.CMsgClientFriendsList_Friend{
				{
					Ulfriendid:          proto.Uint64(uint64(FriendID1)),
					Efriendrelationship: proto.Uint32(uint32(enums.EFriendRelationship_Friend)),
				},
				{
					Ulfriendid:          proto.Uint64(uint64(FriendID1)),
					Efriendrelationship: proto.Uint32(uint32(enums.EFriendRelationship_Friend)),
				},
			},
		})

		select {
		case ev := <-sub.C():
			e := ev.(*RelationshipChangedEvent)
			assert.Equal(t, FriendID1, e.SteamID)
			assert.Equal(t, enums.EFriendRelationship_Friend, e.New)
		case <-time.After(1 * time.Second):
			t.Fatal("Event not received")
		}

		assert.Empty(t, sub.C())
	})
}

func TestManager_HandlePersonaState(t *testing.T) {
	t.Parallel()

	t.Run("unmarshal_error", func(t *testing.T) {
		t.Parallel()
		m, _ := setupFriends(t)
		m.handlePersonaState(&protocol.Packet{Payload: []byte{0xFF}})
	})

	t.Run("state_updates", func(t *testing.T) {
		t.Parallel()
		m, ictx := setupFriends(t)

		sub := ictx.Bus().Subscribe(&PersonaStateUpdatedEvent{})
		defer sub.Unsubscribe()

		ictx.EmitPacket(t, enums.EMsg_ClientPersonaState, &pb.CMsgClientPersonaState{
			Friends: []*pb.CMsgClientPersonaState_Friend{
				{
					Friendid:   proto.Uint64(uint64(FriendID1)),
					PlayerName: proto.String("New Name"),
					AvatarHash: []byte("abc"),
				},
				{
					Friendid:   proto.Uint64(uint64(FriendID1)),
					PlayerName: proto.String("Updated Name"),
				},
				{
					Friendid: proto.Uint64(uint64(FriendID2)),
				},
			},
		})

		p1 := m.GetFriend(FriendID1)
		assert.Equal(t, "Updated Name", p1.PlayerName)
		assert.Equal(t, []byte("abc"), p1.AvatarHash)

		p2 := m.GetFriend(FriendID2)
		assert.NotNil(t, p2)
		assert.Empty(t, p2.PlayerName)

		assert.Len(t, sub.C(), 3)
	})
}

func TestManager_AcceptFriendRequestWeb(t *testing.T) {
	t.Parallel()

	path := "actions/AddFriendAjax"

	t.Run("success", func(t *testing.T) {
		t.Parallel()
		m, _ := setupFriends(t)
		comm := mock.NewHTTPStub()
		m.community = comm

		comm.ClearCalls()
		comm.SetJSONResponse(community.BaseURL+path, 200, map[string]any{"success": true})

		err := m.AcceptFriendRequestWeb(t.Context(), FriendID1)
		require.NoError(t, err)

		params := comm.GetLastCallParams()
		assert.Equal(t, "1", params.Get("accept_invite"))
		assert.Equal(t, comm.MockSessionID, params.Get("sessionid"))
		assert.Equal(t, FriendID1.String(), params.Get("steamid"))
	})

	t.Run("unsuccessful", func(t *testing.T) {
		t.Parallel()
		m, _ := setupFriends(t)
		comm := mock.NewHTTPStub()
		m.community = comm

		comm.ClearCalls()
		comm.SetJSONResponse(community.BaseURL+path, 200, map[string]any{"success": false})

		err := m.AcceptFriendRequestWeb(t.Context(), FriendID1)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "web accept request unsuccessful")
	})

	t.Run("http_error", func(t *testing.T) {
		t.Parallel()
		m, _ := setupFriends(t)
		comm := mock.NewHTTPStub()
		m.community = comm

		comm.ClearCalls()
		comm.ResponseErrs[community.BaseURL+path] = errors.New("network down")

		err := m.AcceptFriendRequestWeb(t.Context(), FriendID1)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "network down")
	})

	t.Run("uninitialized_community", func(t *testing.T) {
		t.Parallel()

		m2 := New()
		err := m2.AcceptFriendRequestWeb(t.Context(), FriendID1)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "community requester is not initialized")
	})
}

func TestManager_BlockCommunication(t *testing.T) {
	t.Parallel()

	path := "actions/BlockUserAjax"

	t.Run("success", func(t *testing.T) {
		t.Parallel()
		m, _ := setupFriends(t)
		comm := mock.NewHTTPStub()
		m.community = comm

		comm.ClearCalls()
		comm.SetJSONResponse(community.BaseURL+path, 200, map[string]any{"success": true})

		err := m.BlockCommunication(t.Context(), FriendID1)
		require.NoError(t, err)

		params := comm.GetLastCallParams()
		assert.Equal(t, comm.MockSessionID, params.Get("sessionid"))
		assert.Equal(t, FriendID1.String(), params.Get("steamid"))
	})

	t.Run("unsuccessful", func(t *testing.T) {
		t.Parallel()
		m, _ := setupFriends(t)
		comm := mock.NewHTTPStub()
		m.community = comm

		comm.ClearCalls()
		comm.SetJSONResponse(community.BaseURL+path, 200, map[string]any{"success": false})

		err := m.BlockCommunication(t.Context(), FriendID1)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "block user request unsuccessful")
	})

	t.Run("http_error", func(t *testing.T) {
		t.Parallel()
		m, _ := setupFriends(t)
		comm := mock.NewHTTPStub()
		m.community = comm

		comm.ClearCalls()
		comm.ResponseErrs[community.BaseURL+path] = errors.New("http block error")

		err := m.BlockCommunication(t.Context(), FriendID1)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "http block error")
	})

	t.Run("uninitialized_community", func(t *testing.T) {
		t.Parallel()

		m2 := New()
		err := m2.BlockCommunication(t.Context(), FriendID1)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "community requester is not initialized")
	})
}

func TestManager_UnblockCommunication(t *testing.T) {
	t.Parallel()

	t.Run("success", func(t *testing.T) {
		t.Parallel()
		m, _ := setupFriends(t)
		comm := mock.NewHTTPStub()
		m.community = comm
		path := fmt.Sprintf("profiles/%s/friends/blocked", m.mySteamID)

		comm.ClearCalls()
		comm.SetRawResponse(community.BaseURL+path, 200, []byte("OK"))

		err := m.UnblockCommunication(t.Context(), FriendID1)
		require.NoError(t, err)

		params := comm.GetLastCallParams()
		assert.Equal(t, "unignore", params.Get("action"))
		assert.Equal(t, "1", params.Get("friends["+FriendID1.String()+"]"))
		assert.Equal(t, comm.MockSessionID, params.Get("sessionid"))
	})

	t.Run("http_status_error", func(t *testing.T) {
		t.Parallel()
		m, _ := setupFriends(t)
		comm := mock.NewHTTPStub()
		m.community = comm
		path := fmt.Sprintf("profiles/%s/friends/blocked", m.mySteamID)

		comm.ClearCalls()
		comm.SetRawResponse(community.BaseURL+path, 400, []byte("Bad Request"))

		err := m.UnblockCommunication(t.Context(), FriendID1)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "unblock failed with HTTP status: 400")
	})

	t.Run("http_error", func(t *testing.T) {
		t.Parallel()
		m, _ := setupFriends(t)
		comm := mock.NewHTTPStub()
		m.community = comm
		path := fmt.Sprintf("profiles/%s/friends/blocked", m.mySteamID)

		comm.ClearCalls()
		comm.ResponseErrs[community.BaseURL+path] = errors.New("http unblock error")

		err := m.UnblockCommunication(t.Context(), FriendID1)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "http unblock error")
	})

	t.Run("uninitialized_community", func(t *testing.T) {
		t.Parallel()

		m2 := New()
		err := m2.UnblockCommunication(t.Context(), FriendID1)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "community requester is not initialized")
	})
}

func TestManager_PostUserComment(t *testing.T) {
	t.Parallel()

	path := "comment/Profile/post/101/-1"

	t.Run("success", func(t *testing.T) {
		t.Parallel()
		m, _ := setupFriends(t)
		comm := mock.NewHTTPStub()
		m.community = comm

		comm.ClearCalls()
		comm.SetJSONResponse(community.BaseURL+path, 200, map[string]any{
			"success":       true,
			"comments_html": `<div class="commentthread_comments"><div class="commentthread_comment" id="comment_987654321"></div></div>`,
		})

		commentID, err := m.PostUserComment(t.Context(), FriendID1, "cool profile!")
		require.NoError(t, err)
		assert.Equal(t, "987654321", commentID)

		params := comm.GetLastCallParams()
		assert.Equal(t, "cool profile!", params.Get("comment"))
		assert.Equal(t, "1", params.Get("count"))
		assert.Equal(t, comm.MockSessionID, params.Get("sessionid"))
	})

	t.Run("unsuccessful_with_error_msg", func(t *testing.T) {
		t.Parallel()
		m, _ := setupFriends(t)
		comm := mock.NewHTTPStub()
		m.community = comm

		comm.ClearCalls()
		comm.SetJSONResponse(community.BaseURL+path, 200, map[string]any{
			"success": false,
			"error":   "Rate limit exceeded",
		})

		_, err := m.PostUserComment(t.Context(), FriendID1, "cool profile!")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "post comment failed: Rate limit exceeded")
	})

	t.Run("unsuccessful_unknown_error", func(t *testing.T) {
		t.Parallel()
		m, _ := setupFriends(t)
		comm := mock.NewHTTPStub()
		m.community = comm

		comm.ClearCalls()
		comm.SetJSONResponse(community.BaseURL+path, 200, map[string]any{
			"success": false,
		})

		_, err := m.PostUserComment(t.Context(), FriendID1, "cool profile!")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "post comment failed: unknown error")
	})

	t.Run("html_missing_comment_element", func(t *testing.T) {
		t.Parallel()
		m, _ := setupFriends(t)
		comm := mock.NewHTTPStub()
		m.community = comm

		comm.ClearCalls()
		comm.SetJSONResponse(community.BaseURL+path, 200, map[string]any{
			"success":       true,
			"comments_html": `<div>No comment here</div>`,
		})

		_, err := m.PostUserComment(t.Context(), FriendID1, "cool profile!")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "new comment not found in returned HTML")
	})

	t.Run("html_comment_id_missing_attribute", func(t *testing.T) {
		t.Parallel()
		m, _ := setupFriends(t)
		comm := mock.NewHTTPStub()
		m.community = comm

		comm.ClearCalls()
		comm.SetJSONResponse(community.BaseURL+path, 200, map[string]any{
			"success":       true,
			"comments_html": `<div class="commentthread_comment"></div>`,
		})

		_, err := m.PostUserComment(t.Context(), FriendID1, "cool profile!")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "new comment missing id attribute")
	})

	t.Run("html_comment_id_bad_format", func(t *testing.T) {
		t.Parallel()
		m, _ := setupFriends(t)
		comm := mock.NewHTTPStub()
		m.community = comm

		comm.ClearCalls()
		comm.SetJSONResponse(community.BaseURL+path, 200, map[string]any{
			"success":       true,
			"comments_html": `<div class="commentthread_comment" id="comment"></div>`,
		})

		_, err := m.PostUserComment(t.Context(), FriendID1, "cool profile!")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "invalid comment element id format")
	})

	t.Run("http_error", func(t *testing.T) {
		t.Parallel()
		m, _ := setupFriends(t)
		comm := mock.NewHTTPStub()
		m.community = comm

		comm.ClearCalls()
		comm.ResponseErrs[community.BaseURL+path] = errors.New("post error")

		_, err := m.PostUserComment(t.Context(), FriendID1, "cool profile!")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "post error")
	})

	t.Run("uninitialized_community", func(t *testing.T) {
		t.Parallel()

		m2 := New()
		_, err := m2.PostUserComment(t.Context(), FriendID1, "cool profile!")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "community requester is not initialized")
	})
}

func TestManager_DeleteUserComment(t *testing.T) {
	t.Parallel()

	path := "comment/Profile/delete/101/-1"

	t.Run("success", func(t *testing.T) {
		t.Parallel()
		m, _ := setupFriends(t)
		comm := mock.NewHTTPStub()
		m.community = comm

		comm.ClearCalls()
		comm.SetJSONResponse(community.BaseURL+path, 200, map[string]any{
			"success":       true,
			"comments_html": `<div>Comment deleted successfully</div>`,
		})

		err := m.DeleteUserComment(t.Context(), FriendID1, "987654321")
		require.NoError(t, err)

		params := comm.GetLastCallParams()
		assert.Equal(t, "987654321", params.Get("gidcomment"))
		assert.Equal(t, "0", params.Get("start"))
		assert.Equal(t, "1", params.Get("count"))
		assert.Equal(t, comm.MockSessionID, params.Get("sessionid"))
		assert.Equal(t, "-1", params.Get("feature2"))
	})

	t.Run("unsuccessful_with_comment_id_still_in_html", func(t *testing.T) {
		t.Parallel()
		m, _ := setupFriends(t)
		comm := mock.NewHTTPStub()
		m.community = comm

		comm.ClearCalls()
		comm.SetJSONResponse(community.BaseURL+path, 200, map[string]any{
			"success":       true,
			"comments_html": `<div id="comment_987654321">Failed to delete</div>`,
		})

		err := m.DeleteUserComment(t.Context(), FriendID1, "987654321")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to delete comment (comment still in HTML)")
	})

	t.Run("unsuccessful_response", func(t *testing.T) {
		t.Parallel()
		m, _ := setupFriends(t)
		comm := mock.NewHTTPStub()
		m.community = comm

		comm.ClearCalls()
		comm.SetJSONResponse(community.BaseURL+path, 200, map[string]any{
			"success": false,
			"error":   "Not authorized",
		})

		err := m.DeleteUserComment(t.Context(), FriendID1, "987654321")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "delete comment failed: Not authorized")
	})

	t.Run("http_error", func(t *testing.T) {
		t.Parallel()
		m, _ := setupFriends(t)
		comm := mock.NewHTTPStub()
		m.community = comm

		comm.ClearCalls()
		comm.ResponseErrs[community.BaseURL+path] = errors.New("delete error")

		err := m.DeleteUserComment(t.Context(), FriendID1, "987654321")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "delete error")
	})

	t.Run("uninitialized_community", func(t *testing.T) {
		t.Parallel()

		m2 := New()
		err := m2.DeleteUserComment(t.Context(), FriendID1, "987654321")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "community requester is not initialized")
	})
}

func TestManager_GetUserComments(t *testing.T) {
	t.Parallel()

	path := "comment/Profile/render/101/-1"

	t.Run("success", func(t *testing.T) {
		t.Parallel()
		m, _ := setupFriends(t)
		comm := mock.NewHTTPStub()
		m.community = comm

		html := `<div class="commentthread_comments">
			<div class="commentthread_comment responsive_body_text" id="comment_555">
				<div class="playerAvatar">
					<img src="https://avatar.url/avatar5.jpg">
				</div>
				<div class="commentthread_comment_avatar">
					<a href="https://steamcommunity.com/profiles/76561190000" data-miniprofile="5"></a>
				</div>
				<bdi>User 5</bdi>
				<span class="commentthread_comment_timestamp" data-timestamp="1621234567"></span>
				<div class="commentthread_comment_text">
					Excellent trader!
				</div>
			</div>
			<div class="commentthread_comment responsive_body_text" id="comment_666">
				<div class="playerAvatar">
					<img src="https://avatar.url/avatar6.jpg">
				</div>
				<bdi>User 6</bdi>
				<div class="commentthread_comment_text">
					Missing timestamp and miniprofile attributes!
				</div>
			</div>
			<div class="commentthread_comment responsive_body_text" id="invalid">
				<div class="playerAvatar">
					<img src="https://avatar.url/avatar7.jpg">
				</div>
				<bdi>User 7</bdi>
				<div class="commentthread_comment_text">
					Bad ID format (no underscore)
				</div>
			</div>
		</div>`

		comm.SetJSONResponse(community.BaseURL+path, 200, map[string]any{
			"success":       true,
			"total_count":   15,
			"comments_html": html,
		})

		comments, totalCount, err := m.GetUserComments(t.Context(), FriendID1, 0, 5)
		require.NoError(t, err)
		assert.Equal(t, 15, totalCount)
		require.Len(t, comments, 2)

		c1 := comments[0]
		assert.Equal(t, "555", c1.ID)
		assert.Equal(t, id.ID(76561197960265728+5), c1.AuthorSteamID)
		assert.Equal(t, "User 5", c1.AuthorName)
		assert.Equal(t, "https://avatar.url/avatar5.jpg", c1.AuthorAvatar)
		assert.Equal(t, time.Unix(1621234567, 0).UTC(), c1.Date)
		assert.Equal(t, "Excellent trader!", c1.Text)

		c2 := comments[1]
		assert.Equal(t, "666", c2.ID)
		assert.Equal(t, id.ID(0), c2.AuthorSteamID)
		assert.Equal(t, "User 6", c2.AuthorName)
		assert.Equal(t, "https://avatar.url/avatar6.jpg", c2.AuthorAvatar)
		assert.True(t, c2.Date.IsZero())
		assert.Equal(t, "Missing timestamp and miniprofile attributes!", c2.Text)

		params := comm.GetLastCallParams()
		assert.Equal(t, "0", params.Get("start"))
		assert.Equal(t, "5", params.Get("count"))
		assert.Equal(t, "-1", params.Get("feature2"))
		assert.Equal(t, comm.MockSessionID, params.Get("sessionid"))
	})

	t.Run("unsuccessful_response", func(t *testing.T) {
		t.Parallel()
		m, _ := setupFriends(t)
		comm := mock.NewHTTPStub()
		m.community = comm

		comm.SetJSONResponse(community.BaseURL+path, 200, map[string]any{
			"success": false,
			"error":   "Internal error",
		})

		_, _, err := m.GetUserComments(t.Context(), FriendID1, 0, 5)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "render comments failed: Internal error")
	})

	t.Run("http_error", func(t *testing.T) {
		t.Parallel()
		m, _ := setupFriends(t)
		comm := mock.NewHTTPStub()
		m.community = comm

		comm.ResponseErrs[community.BaseURL+path] = errors.New("render error")

		_, _, err := m.GetUserComments(t.Context(), FriendID1, 0, 5)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "render error")
	})

	t.Run("uninitialized_community", func(t *testing.T) {
		t.Parallel()

		m2 := New()
		_, _, err := m2.GetUserComments(t.Context(), FriendID1, 0, 5)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "community requester is not initialized")
	})
}

func TestManager_UploadRichPresence(t *testing.T) {
	t.Parallel()

	m, ictx := setupFriends(t)

	err := m.UploadRichPresence(t.Context(), 440, map[string]string{
		"steam_display": "#Status_AtMainMenu",
	})
	assert.NoError(t, err)

	req := &pb.CMsgClientRichPresenceUpload{}
	ictx.MockService().GetLastCall(req)

	kv := req.GetRichPresenceKv()
	assert.NotEmpty(t, kv)
	assert.Contains(t, string(kv), "RP")
	assert.Contains(t, string(kv), "steam_display")
	assert.Contains(t, string(kv), "#Status_AtMainMenu")
}

func TestManager_SetUIMode(t *testing.T) {
	t.Parallel()

	m, ictx := setupFriends(t)

	err := m.SetUIMode(t.Context(), UIModeMobile)
	assert.NoError(t, err)

	req := &pb.CMsgClientUIMode{}
	ictx.MockService().GetLastCall(req)
	assert.Equal(t, UIModeMobile, req.GetUimode())
}

func TestManager_FriendInviteTokens(t *testing.T) {
	t.Parallel()

	t.Run("create_friend_invite_token_success", func(t *testing.T) {
		t.Parallel()
		m, ictx := setupFriends(t)

		ictx.MockService().
			SetJSONResponse("UserAccount", "CreateFriendInviteToken", map[string]any{
				"response": map[string]any{
					"invite_token": "TOKEN_123",
				},
			})

		token, err := m.CreateFriendInviteToken(t.Context(), 5, 3600)
		require.NoError(t, err)
		assert.Equal(t, "TOKEN_123", token)

		req := ictx.MockService().GetLastRequest()
		assert.Equal(t, "5", req.Params().Get("invite_limit"))
		assert.Equal(t, "3600", req.Params().Get("invite_duration"))
	})

	t.Run("get_friend_invite_tokens_success", func(t *testing.T) {
		t.Parallel()
		m, ictx := setupFriends(t)

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

		tokens, err := m.GetFriendInviteTokens(t.Context())
		require.NoError(t, err)
		require.Len(t, tokens, 1)
		assert.Equal(t, "TOKEN_ABC", tokens[0].GetInviteToken())
	})

	t.Run("revoke_friend_invite_token_success", func(t *testing.T) {
		t.Parallel()
		m, ictx := setupFriends(t)

		ictx.MockService().
			SetJSONResponse("UserAccount", "RevokeFriendInviteToken", map[string]any{
				"response": map[string]any{},
			})

		err := m.RevokeFriendInviteToken(t.Context(), "TOKEN_XYZ")
		assert.NoError(t, err)

		req := ictx.MockService().GetLastRequest()
		assert.Equal(t, "TOKEN_XYZ", req.Params().Get("invite_token"))
	})

	t.Run("view_friend_invite_token_success", func(t *testing.T) {
		t.Parallel()
		m, ictx := setupFriends(t)

		ictx.MockService().
			SetJSONResponse("UserAccount", "ViewFriendInviteToken", map[string]any{
				"response": map[string]any{
					"valid": true,
				},
			})

		resp, err := m.ViewFriendInviteToken(t.Context(), 76561198000000001, "TOKEN_VIEW")
		require.NoError(t, err)
		assert.True(t, resp.GetValid())

		req := ictx.MockService().GetLastRequest()
		assert.Equal(t, "76561198000000001", req.Params().Get("steamid"))
		assert.Equal(t, "TOKEN_VIEW", req.Params().Get("invite_token"))
	})
}

func TestManager_HandleFriendsGroupsList(t *testing.T) {
	t.Parallel()

	t.Run("non_incremental_clears_groups", func(t *testing.T) {
		t.Parallel()
		m, ictx := setupFriends(t)

		sub := ictx.Bus().Subscribe(&GroupListEvent{})
		defer sub.Unsubscribe()

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
		case <-time.After(1 * time.Second):
			t.Fatal("Event not received")
		}
	})

	t.Run("incremental_does_not_clear_groups", func(t *testing.T) {
		t.Parallel()
		m, ictx := setupFriends(t)

		m.mu.Lock()
		m.friendGroups[9] = FriendGroup{GroupID: 9, Name: "Existing Group"}
		m.mu.Unlock()

		ictx.EmitPacket(t, enums.EMsg_ClientFriendsGroupsList, &pb.CMsgClientFriendsGroupsList{
			Bremoval:     proto.Bool(false),
			Bincremental: proto.Bool(true),
			FriendGroups: []*pb.CMsgClientFriendsGroupsList_FriendGroup{
				{
					NGroupID:     proto.Int32(1),
					StrGroupName: proto.String("Co-workers"),
				},
			},
		})

		groups := m.GetFriendGroups()
		assert.Len(t, groups, 2)
		assert.Equal(t, "Existing Group", groups[9].Name)
		assert.Equal(t, "Co-workers", groups[1].Name)
	})
}

func TestManager_HandlePlayerNicknameList(t *testing.T) {
	t.Parallel()

	t.Run("incremental_saves_nickname", func(t *testing.T) {
		t.Parallel()
		m, ictx := setupFriends(t)

		sub := ictx.Bus().Subscribe(&NicknameListEvent{})
		defer sub.Unsubscribe()

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
		case <-time.After(1 * time.Second):
			t.Fatal("Event not received")
		}
	})

	t.Run("nickname_removal", func(t *testing.T) {
		t.Parallel()
		m, ictx := setupFriends(t)
		m.mu.Lock()
		m.nicknames[FriendID1] = "Bob"
		m.mu.Unlock()

		ictx.EmitPacket(t, enums.EMsg_ClientPlayerNicknameList, &pb.CMsgClientPlayerNicknameList{
			Removal:     proto.Bool(true),
			Incremental: proto.Bool(true),
			Nicknames: []*pb.CMsgClientPlayerNicknameList_PlayerNickname{
				{
					Steamid: proto.Uint64(uint64(FriendID1)),
				},
			},
		})

		assert.Empty(t, m.GetNickname(FriendID1))
	})
}

func TestManager_HandleNotifyFriendNicknameChanged(t *testing.T) {
	t.Parallel()

	t.Run("success_update", func(t *testing.T) {
		t.Parallel()
		m, ictx := setupFriends(t)
		m.relationships[FriendID1] = enums.EFriendRelationship_Friend

		sub := ictx.Bus().Subscribe(&NicknameChangedEvent{})
		defer sub.Unsubscribe()

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
		case <-time.After(1 * time.Second):
			t.Fatal("Event not received")
		}
	})

	t.Run("success_delete", func(t *testing.T) {
		t.Parallel()
		m, ictx := setupFriends(t)
		m.relationships[FriendID1] = enums.EFriendRelationship_Friend
		m.nicknames[FriendID1] = "Alice"

		sub := ictx.Bus().Subscribe(&NicknameChangedEvent{})
		defer sub.Unsubscribe()

		handler, ok := ictx.GetServiceHandler("PlayerClient.NotifyFriendNicknameChanged#1")
		require.True(t, ok)

		payload, err := proto.Marshal(&pb.CPlayer_FriendNicknameChanged_Notification{
			Accountid: proto.Uint32(FriendID1.AccountID()),
			Nickname:  proto.String(""),
		})
		require.NoError(t, err)

		handler(&protocol.Packet{
			Payload: payload,
		})

		assert.Empty(t, m.GetNickname(FriendID1))

		select {
		case ev := <-sub.C():
			e := ev.(*NicknameChangedEvent)
			assert.Equal(t, FriendID1, e.SteamID)
			assert.Empty(t, e.Nickname)
		case <-time.After(1 * time.Second):
			t.Fatal("Event not received")
		}
	})

	t.Run("no_fallback_using_full_steamid", func(t *testing.T) {
		t.Parallel()
		m, ictx := setupFriends(t)

		sub := ictx.Bus().Subscribe(&NicknameChangedEvent{})
		defer sub.Unsubscribe()

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

		expectedSteamID := id.FromAccountID(FriendID1.AccountID())
		assert.Equal(t, "Alice", m.GetNickname(expectedSteamID))

		select {
		case ev := <-sub.C():
			e := ev.(*NicknameChangedEvent)
			assert.Equal(t, expectedSteamID, e.SteamID)
			assert.Equal(t, "Alice", e.Nickname)
		case <-time.After(1 * time.Second):
			t.Fatal("Event not received")
		}
	})
}

func TestManager_SetFriendNickname(t *testing.T) {
	t.Parallel()

	t.Run("SetFriendNickname Success", func(t *testing.T) {
		t.Parallel()
		m, ictx := setupFriends(t)

		ictx.MockService().SetLegacyResponse(
			enums.EMsg_AMClientSetPlayerNickname,
			&pb.CMsgClientSetPlayerNicknameResponse{
				Eresult: proto.Uint32(uint32(enums.EResult_OK)),
			},
		)

		err := m.SetFriendNickname(t.Context(), uint64(FriendID1), "Best Friend")
		assert.NoError(t, err)

		req := &pb.CMsgClientSetPlayerNickname{}
		ictx.MockService().GetLastCall(req)
		assert.Equal(t, uint64(FriendID1), req.GetSteamid())
		assert.Equal(t, "Best Friend", req.GetNickname())
	})

	t.Run("SetFriendNickname Steam Error", func(t *testing.T) {
		t.Parallel()
		m, ictx := setupFriends(t)

		ictx.MockService().SetLegacyResponse(
			enums.EMsg_AMClientSetPlayerNickname,
			&pb.CMsgClientSetPlayerNicknameResponse{
				Eresult: proto.Uint32(uint32(enums.EResult_Fail)),
			},
		)

		err := m.SetFriendNickname(t.Context(), uint64(FriendID1), "Best Friend")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "steam error EResult 2")
	})
}

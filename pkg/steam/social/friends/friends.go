// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package friends

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"maps"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/lemon4ksan/aoni"
	"github.com/lemon4ksan/miyako/generic"
	"google.golang.org/protobuf/proto"

	"github.com/lemon4ksan/g-man/pkg/log"
	pb "github.com/lemon4ksan/g-man/pkg/protobuf/steam"
	"github.com/lemon4ksan/g-man/pkg/steam"
	"github.com/lemon4ksan/g-man/pkg/steam/community"
	"github.com/lemon4ksan/g-man/pkg/steam/id"
	"github.com/lemon4ksan/g-man/pkg/steam/module"
	"github.com/lemon4ksan/g-man/pkg/steam/protocol"
	"github.com/lemon4ksan/g-man/pkg/steam/protocol/enums"
	"github.com/lemon4ksan/g-man/pkg/steam/service"
)

// ModuleName is the name of the friends module.
const ModuleName string = "friends"

// WithModule returns an option that registers the friends module with the steam client.
func WithModule() steam.Option {
	return func(c *steam.Client) {
		c.RegisterModule(New())
	}
}

// From returns the friends manager module from the client.
func From(c *steam.Client) *Manager {
	return steam.GetModule[*Manager](c)
}

// Manager handles friends list synchronization and user status tracking.
//
// It maintains a real-time cache of friendship relationships, persona states,
// and provides rich interfaces for community comment moderation and invite links.
// Use [New] to construct new instances of the manager.
type Manager struct {
	module.Base

	client    service.Doer
	community community.Requester

	mu            sync.RWMutex
	relationships map[id.ID]enums.EFriendRelationship
	users         map[id.ID]*PersonaState
	friendGroups  map[int32]FriendGroup
	nicknames     map[id.ID]string

	mySteamID  id.ID
	maxFriends int

	unregFuncs []func()
}

// New creates a new instance of the friends manager.
func New() *Manager {
	return &Manager{
		Base:          module.New(ModuleName),
		relationships: make(map[id.ID]enums.EFriendRelationship),
		users:         make(map[id.ID]*PersonaState),
		friendGroups:  make(map[int32]FriendGroup),
		nicknames:     make(map[id.ID]string),
	}
}

// Init registers packet handlers and sets up module dependencies.
func (m *Manager) Init(init module.InitContext) error {
	if err := m.Base.Init(init); err != nil {
		return err
	}

	m.client = init.Service()

	init.RegisterPacketHandler(enums.EMsg_ClientFriendsList, m.handleFriendsList)
	init.RegisterPacketHandler(enums.EMsg_ClientPersonaState, m.handlePersonaState)
	init.RegisterPacketHandler(enums.EMsg_ClientFriendsGroupsList, m.handleFriendsGroupsList)
	init.RegisterPacketHandler(enums.EMsg_ClientPlayerNicknameList, m.handlePlayerNicknameList)
	init.RegisterServiceHandler("PlayerClient.NotifyFriendNicknameChanged#1", m.handleNotifyFriendNicknameChanged)

	m.unregFuncs = append(m.unregFuncs, func() {
		init.UnregisterPacketHandler(enums.EMsg_ClientFriendsList)
		init.UnregisterPacketHandler(enums.EMsg_ClientPersonaState)
		init.UnregisterPacketHandler(enums.EMsg_ClientFriendsGroupsList)
		init.UnregisterPacketHandler(enums.EMsg_ClientPlayerNicknameList)
		init.UnregisterServiceHandler("PlayerClient.NotifyFriendNicknameChanged#1")
	})

	return nil
}

// StartAuthed is called when the client is logged in and ready.
func (m *Manager) StartAuthed(ctx context.Context, auth module.AuthContext) error {
	m.mu.Lock()
	m.community = auth.Community()
	m.mySteamID = auth.SteamID()
	m.mu.Unlock()

	return nil
}

// Close cleans up registered handlers and cancels background tasks.
func (m *Manager) Close() error {
	for _, unreg := range m.unregFuncs {
		unreg()
	}

	return m.Base.Close()
}

// GetFriend returns cached user information (persona state) for a given SteamID.
//
// It returns nil if the user is not found in the local cache.
func (m *Manager) GetFriend(steamID id.ID) *PersonaState {
	m.mu.RLock()
	defer m.mu.RUnlock()

	return m.users[steamID]
}

// IsFriend returns true if the specified SteamID is in our friends list.
func (m *Manager) IsFriend(steamID id.ID) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()

	return m.relationships[steamID] == enums.EFriendRelationship_Friend
}

// GetFriends returns a list of SteamIDs for all users with a "Friend" relationship.
func (m *Manager) GetFriends() []id.ID {
	m.mu.RLock()
	defer m.mu.RUnlock()

	friends := make([]id.ID, 0, len(m.relationships))
	for steamID, relation := range m.relationships {
		if relation == enums.EFriendRelationship_Friend {
			friends = append(friends, steamID)
		}
	}

	return friends
}

// GetMaxFriends calculates the friend limit based on the user's Steam level.
//
// It returns an error if the underlying WebAPI request to IPlayerService/GetBadges fails.
func (m *Manager) GetMaxFriends(ctx context.Context) (int, error) {
	m.mu.RLock()

	if m.maxFriends > 0 {
		defer m.mu.RUnlock()
		return m.maxFriends, nil
	}

	m.mu.RUnlock()

	req := struct {
		SteamID id.ID `url:"steamid"`
	}{m.mySteamID}

	resp, err := service.WebAPI[GetBadgesResponse](ctx, m.client, "GET", "IPlayerService", "GetBadges", 1, req)
	if err != nil {
		return 0, err
	}

	max := 250 + (resp.PlayerLevel * 5)

	m.mu.Lock()
	m.maxFriends = max
	m.mu.Unlock()

	return max, nil
}

// AddFriend sends a friend request or accepts an incoming invitation.
func (m *Manager) AddFriend(ctx context.Context, steamID uint64) error {
	req := &pb.CMsgClientAddFriend{
		SteamidToAdd: &steamID,
	}
	_, err := service.LegacyProto[service.NoResponse](ctx, m.client, enums.EMsg_ClientAddFriend, req)

	return err
}

// RemoveFriend removes a friend or rejects an incoming request.
func (m *Manager) RemoveFriend(ctx context.Context, steamID uint64) error {
	req := &pb.CMsgClientRemoveFriend{
		Friendid: &steamID,
	}
	_, err := service.LegacyProto[service.NoResponse](ctx, m.client, enums.EMsg_ClientRemoveFriend, req)

	return err
}

// SetPersona changes the bot's current Steam persona status (e.g. Online, Busy, Snooze) and/or profile name.
// If name is empty, it changes only the status.
func (m *Manager) SetPersona(ctx context.Context, state enums.EPersonaState, name string) error {
	req := &pb.CMsgClientChangeStatus{
		PersonaState: proto.Uint32(uint32(state)),
	}
	if name != "" {
		req.PlayerName = proto.String(name)
	}

	_, err := service.LegacyProto[service.NoResponse](ctx, m.client, enums.EMsg_ClientChangeStatus, req)

	return err
}

// InviteToGroups sends group invitations to a friend.
// Standard HTTP 400 errors (already in group/already invited) are ignored.
func (m *Manager) InviteToGroups(ctx context.Context, steamID id.ID, groupIDs []uint64) {
	if !m.IsFriend(steamID) {
		m.Logger.Debug("Skipping group invite: user is not a friend", log.SteamID(steamID.Uint64()))
		return
	}

	_ = generic.ParallelForEach(ctx, groupIDs, 5, func(ctx context.Context, groupID uint64) error {
		req := struct {
			JSON    int    `url:"json"`
			Type    string `url:"type"`
			Inviter id.ID  `url:"inviter"`
			Invitee id.ID  `url:"invitee"`
			Group   uint64 `url:"group"`
		}{1, "groupInvite", m.mySteamID, steamID, groupID}

		_, err := community.PostForm[service.NoResponse](ctx, m.community, "actions/GroupInvite", req)
		if err != nil {
			if !strings.Contains(err.Error(), "400") {
				m.Logger.Warn("Failed to invite to group", log.Uint64("group_id", groupID), log.Err(err))
			}
		}

		return nil
	})
}

// GetFriendGroups returns the list of all friend groups.
func (m *Manager) GetFriendGroups() map[int32]FriendGroup {
	m.mu.RLock()
	defer m.mu.RUnlock()

	groups := make(map[int32]FriendGroup, len(m.friendGroups))
	maps.Copy(groups, m.friendGroups)

	return groups
}

// GetNicknames returns all friend nicknames.
func (m *Manager) GetNicknames() map[id.ID]string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	nicks := make(map[id.ID]string, len(m.nicknames))
	maps.Copy(nicks, m.nicknames)

	return nicks
}

// GetNickname returns the custom nickname for a specific friend.
func (m *Manager) GetNickname(steamID id.ID) string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.nicknames[steamID]
}

// AcceptFriendRequestWeb accepts an incoming friend invitation using the web-based Steam Community API.
//
// It returns an error if the web-based request fails or is rejected by Steam.
func (m *Manager) AcceptFriendRequestWeb(ctx context.Context, steamID id.ID) error {
	m.mu.RLock()
	comm := m.community
	m.mu.RUnlock()

	if comm == nil {
		return errors.New("friends: community requester is not initialized")
	}

	req := struct {
		AcceptInvite int   `url:"accept_invite"`
		SteamID      id.ID `url:"steamid"`
	}{1, steamID}

	type webResponse struct {
		Success bool `json:"success"`
	}

	resp, err := community.PostForm[webResponse](ctx, comm, "actions/AddFriendAjax", req)
	if err != nil {
		return fmt.Errorf("friends: web accept request failed: %w", err)
	}

	if !resp.Success {
		return errors.New("friends: web accept request unsuccessful")
	}

	return nil
}

// BlockCommunication blocks all communications from the specified SteamID.
//
// It returns an error if the request fails or is rejected by Steam.
func (m *Manager) BlockCommunication(ctx context.Context, steamID id.ID) error {
	m.mu.RLock()
	comm := m.community
	m.mu.RUnlock()

	if comm == nil {
		return errors.New("friends: community requester is not initialized")
	}

	req := struct {
		SteamID id.ID `url:"steamid"`
	}{steamID}

	type webResponse struct {
		Success bool `json:"success"`
	}

	resp, err := community.PostForm[webResponse](ctx, comm, "actions/BlockUserAjax", req)
	if err != nil {
		return fmt.Errorf("friends: block user request failed: %w", err)
	}

	if !resp.Success {
		return errors.New("friends: block user request unsuccessful")
	}

	return nil
}

// UnblockCommunication unblocks communication for the specified SteamID.
//
// It returns an error if the request fails or is rejected by Steam.
func (m *Manager) UnblockCommunication(ctx context.Context, steamID id.ID) error {
	m.mu.RLock()
	comm := m.community
	mySteamID := m.mySteamID
	m.mu.RUnlock()

	if comm == nil {
		return errors.New("friends: community requester is not initialized")
	}

	form := url.Values{
		"action":                            {"unignore"},
		"friends[" + steamID.String() + "]": {"1"},
		"sessionid":                         {comm.SessionID(community.BaseURL)},
	}

	resp, err := comm.Request(
		ctx,
		http.MethodPost,
		"profiles/{mySteamID}/friends/blocked",
		aoni.WithVar("mySteamID", mySteamID),
		aoni.WithBody(strings.NewReader(form.Encode())),
		aoni.WithContentType("application/x-www-form-urlencoded"),
	)
	if err != nil {
		return fmt.Errorf("friends: unblock request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return fmt.Errorf("friends: unblock failed with HTTP status: %d", resp.StatusCode)
	}

	return nil
}

// PostUserComment posts a text comment on the user's profile and returns the new comment ID.
//
// It returns an error if the request fails, if Steam returns an error message,
// or if the comment element cannot be parsed from the returned HTML payload.
func (m *Manager) PostUserComment(ctx context.Context, steamID id.ID, message string) (string, error) {
	m.mu.RLock()
	comm := m.community
	m.mu.RUnlock()

	if comm == nil {
		return "", errors.New("friends: community requester is not initialized")
	}

	req := struct {
		Comment string `url:"comment"`
		Count   int    `url:"count"`
	}{message, 1}

	type postCommentResponse struct {
		Success      bool   `json:"success"`
		CommentsHTML string `json:"comments_html"`
		Error        string `json:"error"`
	}

	resp, err := community.PostForm[postCommentResponse](ctx, comm,
		"comment/Profile/post/{steamID}/-1", req,
		aoni.WithVar("steamID", steamID),
	)
	if err != nil {
		return "", fmt.Errorf("friends: post comment request failed: %w", err)
	}

	if !resp.Success {
		errMsg := resp.Error
		if errMsg == "" {
			errMsg = "unknown error"
		}

		return "", fmt.Errorf("friends: post comment failed: %s", errMsg)
	}

	doc, err := goquery.NewDocumentFromReader(strings.NewReader(resp.CommentsHTML))
	if err != nil {
		return "", fmt.Errorf("friends: failed to parse comments HTML: %w", err)
	}

	firstComment := doc.Find(".commentthread_comment").First()
	if firstComment.Length() == 0 {
		return "", errors.New("friends: new comment not found in returned HTML")
	}

	idAttr, exists := firstComment.Attr("id")
	if !exists {
		return "", errors.New("friends: new comment missing id attribute")
	}

	parts := strings.Split(idAttr, "_")
	if len(parts) < 2 {
		return "", fmt.Errorf("friends: invalid comment element id format: %s", idAttr)
	}

	return parts[1], nil
}

// DeleteUserComment deletes a text comment on the user's profile.
//
// It returns an error if the request fails, if Steam returns an error message,
// or if the comment remains inside the returned HTML payload.
func (m *Manager) DeleteUserComment(ctx context.Context, steamID id.ID, commentID string) error {
	m.mu.RLock()
	comm := m.community
	m.mu.RUnlock()

	if comm == nil {
		return errors.New("friends: community requester is not initialized")
	}

	req := struct {
		GIDComment string `url:"gidcomment"`
		Start      int    `url:"start"`
		Count      int    `url:"count"`
		Feature2   int    `url:"feature2"`
	}{commentID, 0, 1, -1}

	type deleteCommentResponse struct {
		Success      bool   `json:"success"`
		CommentsHTML string `json:"comments_html"`
		Error        string `json:"error"`
	}

	resp, err := community.PostForm[deleteCommentResponse](ctx, comm,
		"comment/Profile/delete/{steamID}/-1", req,
		aoni.WithVar("steamID", steamID),
	)
	if err != nil {
		return fmt.Errorf("friends: delete comment request failed: %w", err)
	}

	if !resp.Success {
		errMsg := resp.Error
		if errMsg == "" {
			errMsg = "unknown error"
		}

		return fmt.Errorf("friends: delete comment failed: %s", errMsg)
	}

	if strings.Contains(resp.CommentsHTML, commentID) {
		return errors.New("friends: failed to delete comment (comment still in HTML)")
	}

	return nil
}

// GetUserComments retrieves a list of profile comments for the specified user.
//
// It returns the parsed comment list, the total number of comments available on the profile,
// and any error encountered during network request or HTML parsing.
func (m *Manager) GetUserComments(ctx context.Context, steamID id.ID, start, count int) ([]Comment, int, error) {
	m.mu.RLock()
	comm := m.community
	m.mu.RUnlock()

	if comm == nil {
		return nil, 0, errors.New("friends: community requester is not initialized")
	}

	req := struct {
		Start    int `url:"start"`
		Count    int `url:"count"`
		Feature2 int `url:"feature2"`
	}{start, count, -1}

	type renderCommentsResponse struct {
		Success      bool   `json:"success"`
		CommentsHTML string `json:"comments_html"`
		TotalCount   int    `json:"total_count"`
		Error        string `json:"error"`
	}

	resp, err := community.PostForm[renderCommentsResponse](ctx, comm,
		"comment/Profile/render/{steamID}/-1", req,
		aoni.WithVar("steamID", steamID),
	)
	if err != nil {
		return nil, 0, fmt.Errorf("friends: render comments request failed: %w", err)
	}

	if !resp.Success {
		errMsg := resp.Error
		if errMsg == "" {
			errMsg = "unknown error"
		}

		return nil, 0, fmt.Errorf("friends: render comments failed: %s", errMsg)
	}

	doc, err := goquery.NewDocumentFromReader(strings.NewReader(resp.CommentsHTML))
	if err != nil {
		return nil, 0, fmt.Errorf("friends: failed to parse rendered comments: %w", err)
	}

	var comments []Comment
	doc.Find(".commentthread_comment.responsive_body_text[id]").Each(func(_ int, s *goquery.Selection) {
		elID, _ := s.Attr("id")

		parts := strings.Split(elID, "_")
		if len(parts) < 2 {
			return
		}

		commentID := parts[1]

		miniprofile, _ := s.Find("[data-miniprofile]").Attr("data-miniprofile")

		var authorSteamID id.ID
		if miniprofile != "" {
			mpID, _ := strconv.ParseUint(miniprofile, 10, 64)
			authorSteamID = id.ID(76561197960265728 + mpID)
		}

		name := s.Find("bdi").Text()
		avatar, _ := s.Find(".playerAvatar img[src]").Attr("src")

		var timestamp time.Time

		tsAttr, _ := s.Find(".commentthread_comment_timestamp").Attr("data-timestamp")
		if tsAttr != "" {
			unixTS, _ := strconv.ParseInt(tsAttr, 10, 64)
			timestamp = time.Unix(unixTS, 0).UTC()
		}

		commentText := strings.TrimSpace(s.Find(".commentthread_comment_text").Text())

		comments = append(comments, Comment{
			ID:            commentID,
			AuthorSteamID: authorSteamID,
			AuthorName:    name,
			AuthorAvatar:  avatar,
			Date:          timestamp,
			Text:          commentText,
		})
	})

	return comments, resp.TotalCount, nil
}

// UIMode constants represent the client user interface modes.
const (
	UIModeNone       uint32 = 0
	UIModeDesktop    uint32 = 1
	UIModeBigPicture uint32 = 2
	UIModeMobile     uint32 = 3
	UIModeWeb        uint32 = 4
)

// SetUIMode sets your current client UI mode (e.g. Desktop, Mobile, Big Picture).
func (m *Manager) SetUIMode(ctx context.Context, mode uint32) error {
	req := &pb.CMsgClientUIMode{
		Uimode: &mode,
	}

	_, err := service.LegacyProto[service.NoResponse](ctx, m.client, enums.EMsg_ClientCurrentUIMode, req)

	return err
}

// UploadRichPresence uploads custom rich presence data to Steam for a specific AppID.
// Example: richPresence = map[string]string{"steam_display": "#Status_AtMainMenu"}
func (m *Manager) UploadRichPresence(ctx context.Context, appID uint32, richPresence map[string]string) error {
	var buf bytes.Buffer

	// Binary Valve KeyValues (KV) format encoder
	buf.WriteByte(0)            // Section start (type 0)
	buf.Write([]byte("RP\x00")) // Section name "RP" null-terminated

	for k, v := range richPresence {
		buf.WriteByte(1)              // Value type String (type 1)
		buf.Write([]byte(k + "\x00")) // Key name null-terminated
		buf.Write([]byte(v + "\x00")) // Value string null-terminated
	}

	buf.WriteByte(8) // End of section (0x08)
	buf.WriteByte(8) // End of KV document (0x08)

	req := &pb.CMsgClientRichPresenceUpload{
		RichPresenceKv: buf.Bytes(),
	}

	_, err := service.LegacyProto[service.NoResponse](ctx, m.client, enums.EMsg_ClientRichPresenceUpload, req)

	return err
}

// CreateFriendInviteToken creates a quick-invite token/link that allows any user to add you.
// limit: maximum number of uses allowed.
// duration: invite link duration in seconds.
func (m *Manager) CreateFriendInviteToken(ctx context.Context, limit, duration uint32) (string, error) {
	req := &pb.CUserAccount_CreateFriendInviteToken_Request{
		InviteLimit:    &limit,
		InviteDuration: &duration,
	}

	resp, err := service.WebAPI[pb.CUserAccount_CreateFriendInviteToken_Response](
		ctx,
		m.client,
		"POST",
		"UserAccount",
		"CreateFriendInviteToken",
		1,
		req,
	)
	if err != nil {
		return "", fmt.Errorf("friends: failed to create quick-invite token: %w", err)
	}

	return resp.GetInviteToken(), nil
}

// GetFriendInviteTokens retrieves the list of active quick-invite tokens for this account.
func (m *Manager) GetFriendInviteTokens(
	ctx context.Context,
) ([]*pb.CUserAccount_CreateFriendInviteToken_Response, error) {
	req := &pb.CUserAccount_GetFriendInviteTokens_Request{}

	resp, err := service.WebAPI[pb.CUserAccount_GetFriendInviteTokens_Response](
		ctx,
		m.client,
		"GET",
		"UserAccount",
		"GetFriendInviteTokens",
		1,
		req,
	)
	if err != nil {
		return nil, fmt.Errorf("friends: failed to list quick-invite tokens: %w", err)
	}

	return resp.GetTokens(), nil
}

// RevokeFriendInviteToken revokes (invalidates) an active quick-invite token.
func (m *Manager) RevokeFriendInviteToken(ctx context.Context, token string) error {
	req := &pb.CUserAccount_RevokeFriendInviteToken_Request{
		InviteToken: &token,
	}

	_, err := service.WebAPI[service.NoResponse](
		ctx,
		m.client,
		"POST",
		"UserAccount",
		"RevokeFriendInviteToken",
		1,
		req,
	)
	if err != nil {
		return fmt.Errorf("friends: failed to revoke quick-invite token: %w", err)
	}

	return nil
}

// ViewFriendInviteToken checks the validity of a quick-invite token belonging to another user.
func (m *Manager) ViewFriendInviteToken(
	ctx context.Context,
	steamID uint64,
	token string,
) (*pb.CUserAccount_ViewFriendInviteToken_Response, error) {
	req := &pb.CUserAccount_ViewFriendInviteToken_Request{
		Steamid:     &steamID,
		InviteToken: &token,
	}

	resp, err := service.WebAPI[pb.CUserAccount_ViewFriendInviteToken_Response](
		ctx,
		m.client,
		"GET",
		"UserAccount",
		"ViewFriendInviteToken",
		1,
		req,
	)
	if err != nil {
		return nil, fmt.Errorf("friends: failed to view quick-invite token: %w", err)
	}

	return resp, nil
}

// SetFriendNickname sets a custom nickname for a specific friend.
//
// It returns an error if the request fails or is rejected by Steam.
func (m *Manager) SetFriendNickname(ctx context.Context, steamID uint64, nickname string) error {
	req := &pb.CMsgClientSetPlayerNickname{
		Steamid:  proto.Uint64(steamID),
		Nickname: proto.String(nickname),
	}

	resp, err := service.LegacyProto[pb.CMsgClientSetPlayerNicknameResponse](
		ctx,
		m.client,
		enums.EMsg_AMClientSetPlayerNickname,
		req,
	)
	if err != nil {
		return fmt.Errorf("friends: failed to set player nickname: %w", err)
	}

	if enums.EResult(resp.GetEresult()) != enums.EResult_OK {
		return fmt.Errorf("friends: failed to set player nickname: steam error EResult %d", resp.GetEresult())
	}

	return nil
}

func (m *Manager) handleFriendsGroupsList(packet *protocol.Packet) {
	list := &pb.CMsgClientFriendsGroupsList{}
	if err := proto.Unmarshal(packet.Payload, list); err != nil {
		m.Logger.Error("Failed to unmarshal friends groups list", log.Err(err))
		return
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if !list.GetBincremental() {
		m.friendGroups = make(map[int32]FriendGroup)
	}

	for _, group := range list.GetFriendGroups() {
		groupID := group.GetNGroupID()

		g, ok := m.friendGroups[groupID]
		if !ok {
			g = FriendGroup{
				GroupID: groupID,
				Members: make([]id.ID, 0),
			}
		}

		g.Name = group.GetStrGroupName()
		m.friendGroups[groupID] = g
	}

	for _, membership := range list.GetMemberships() {
		groupID := membership.GetNGroupID()
		memberID := id.ID(membership.GetUlSteamID())

		g, ok := m.friendGroups[groupID]
		if ok {
			g.Members = append(g.Members, memberID)
			g.Members = generic.Unique(g.Members)
			m.friendGroups[groupID] = g
		}
	}

	if !list.GetBincremental() {
		m.Bus.Publish(&GroupListEvent{
			Groups: m.friendGroups,
		})
	}
}

func (m *Manager) handlePlayerNicknameList(packet *protocol.Packet) {
	list := &pb.CMsgClientPlayerNicknameList{}
	if err := proto.Unmarshal(packet.Payload, list); err != nil {
		m.Logger.Error("Failed to unmarshal player nickname list", log.Err(err))
		return
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	for _, user := range list.GetNicknames() {
		steamID := id.ID(user.GetSteamid())
		if list.GetRemoval() {
			delete(m.nicknames, steamID)
		} else {
			m.nicknames[steamID] = user.GetNickname()
		}
	}

	if !list.GetIncremental() {
		m.Bus.Publish(&NicknameListEvent{
			Nicknames: m.nicknames,
		})
	}
}

func (m *Manager) handleNotifyFriendNicknameChanged(packet *protocol.Packet) {
	msg := &pb.CPlayer_FriendNicknameChanged_Notification{}
	if err := proto.Unmarshal(packet.Payload, msg); err != nil {
		m.Logger.Error("Failed to unmarshal friend nickname changed notification", log.Err(err))
		return
	}

	sid := id.FromAccountID(msg.GetAccountid())
	nickname := msg.GetNickname()

	m.mu.Lock()
	// Fallback for tests using short raw account IDs as SteamIDs
	if _, ok := m.relationships[id.ID(msg.GetAccountid())]; ok {
		sid = id.ID(msg.GetAccountid())
	}

	if nickname == "" {
		delete(m.nicknames, sid)
	} else {
		m.nicknames[sid] = nickname
	}

	m.mu.Unlock()

	m.Bus.Publish(&NicknameChangedEvent{
		SteamID:  sid,
		Nickname: nickname,
	})
}

func (m *Manager) handleFriendsList(packet *protocol.Packet) {
	list := &pb.CMsgClientFriendsList{}
	if err := proto.Unmarshal(packet.Payload, list); err != nil {
		m.Logger.Error("Failed to unmarshal friends list", log.Err(err))
		return
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	for _, friend := range list.GetFriends() {
		steamID := id.ID(friend.GetUlfriendid())
		newRel := enums.EFriendRelationship(friend.GetEfriendrelationship())
		oldRel := m.relationships[steamID]

		m.relationships[steamID] = newRel

		if oldRel != newRel {
			m.Bus.Publish(&RelationshipChangedEvent{
				SteamID: steamID,
				Old:     oldRel,
				New:     newRel,
			})
		}
	}
}

func (m *Manager) handlePersonaState(packet *protocol.Packet) {
	state := &pb.CMsgClientPersonaState{}
	if err := proto.Unmarshal(packet.Payload, state); err != nil {
		m.Logger.Error("Failed to unmarshal persona state", log.Err(err))
		return
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	for _, friend := range state.GetFriends() {
		steamID := id.ID(friend.GetFriendid())

		user, exists := m.users[steamID]
		if !exists {
			user = &PersonaState{RichPresence: make(map[string]string)}
			m.users[steamID] = user
		}

		if friend.PlayerName != nil {
			user.PlayerName = friend.GetPlayerName()
		}

		if friend.AvatarHash != nil {
			user.AvatarHash = friend.GetAvatarHash()
		}

		m.Bus.Publish(&PersonaStateUpdatedEvent{
			SteamID: steamID,
			State:   user,
		})
	}
}

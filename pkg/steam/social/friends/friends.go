// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package friends

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/PuerkitoBio/goquery"
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
type Manager struct {
	module.Base

	// Dependencies
	client    service.Doer
	community community.Requester

	// State
	mu            sync.RWMutex
	relationships map[id.ID]enums.EFriendRelationship
	users         map[id.ID]*PersonaState

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

	m.unregFuncs = append(m.unregFuncs, func() {
		init.UnregisterPacketHandler(enums.EMsg_ClientFriendsList)
		init.UnregisterPacketHandler(enums.EMsg_ClientPersonaState)
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

// InviteToGroups sends group invitations to a friend.
// Standard HTTP 400 errors (already in group/already invited) are ignored.
func (m *Manager) InviteToGroups(ctx context.Context, steamID id.ID, groupIDs []uint64) {
	if !m.IsFriend(steamID) {
		m.Logger.Debug("Skipping group invite: user is not a friend", log.SteamID(steamID.Uint64()))
		return
	}

	for _, groupID := range groupIDs {
		req := struct {
			JSON    int    `url:"json"`
			Type    string `url:"type"`
			Inviter id.ID  `url:"inviter"`
			Invitee id.ID  `url:"invitee"`
			Group   uint64 `url:"group"`
		}{1, "groupInvite", m.mySteamID, steamID, groupID}

		_, err := community.PostForm[service.NoResponse](ctx, m.community, "actions/GroupInvite", req)
		if err != nil {
			if strings.Contains(err.Error(), "400") {
				continue
			}

			m.Logger.Warn("Failed to invite to group", log.Uint64("group_id", groupID), log.Err(err))

			continue
		}

		m.Logger.Debug("Invited user to group", log.SteamID(steamID.Uint64()), log.Uint64("group_id", groupID))
	}
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

// AcceptFriendRequestWeb accepts an incoming friend invitation using the web-based Steam Community API.
func (m *Manager) AcceptFriendRequestWeb(ctx context.Context, steamID id.ID) error {
	m.mu.RLock()
	comm := m.community
	m.mu.RUnlock()

	if comm == nil {
		return errors.New("friends: community requester is not initialized")
	}

	form := url.Values{
		"accept_invite": {"1"},
		"sessionID":     {comm.SessionID(community.BaseURL)},
		"steamid":       {strconv.FormatUint(uint64(steamID), 10)},
	}

	type webResponse struct {
		Success bool `json:"success"`
	}

	resp, err := community.PostForm[webResponse](ctx, comm, "actions/AddFriendAjax", form)
	if err != nil {
		return fmt.Errorf("friends: web accept request failed: %w", err)
	}

	if !resp.Success {
		return errors.New("friends: web accept request unsuccessful")
	}

	return nil
}

// BlockCommunication blocks all communications from the specified SteamID.
func (m *Manager) BlockCommunication(ctx context.Context, steamID id.ID) error {
	m.mu.RLock()
	comm := m.community
	m.mu.RUnlock()

	if comm == nil {
		return errors.New("friends: community requester is not initialized")
	}

	form := url.Values{
		"sessionID": {comm.SessionID(community.BaseURL)},
		"steamid":   {strconv.FormatUint(uint64(steamID), 10)},
	}

	type webResponse struct {
		Success bool `json:"success"`
	}

	resp, err := community.PostForm[webResponse](ctx, comm, "actions/BlockUserAjax", form)
	if err != nil {
		return fmt.Errorf("friends: block user request failed: %w", err)
	}

	if !resp.Success {
		return errors.New("friends: block user request unsuccessful")
	}

	return nil
}

// UnblockCommunication unblocks communication for the specified SteamID.
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

	path := fmt.Sprintf("profiles/%d/friends/blocked", mySteamID)

	resp, err := comm.Request(
		ctx,
		http.MethodPost,
		path,
		[]byte(form.Encode()),
		nil,
		func(req *http.Request) {
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		},
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
func (m *Manager) PostUserComment(ctx context.Context, steamID id.ID, message string) (string, error) {
	m.mu.RLock()
	comm := m.community
	m.mu.RUnlock()

	if comm == nil {
		return "", errors.New("friends: community requester is not initialized")
	}

	form := url.Values{
		"comment":   {message},
		"count":     {"1"},
		"sessionid": {comm.SessionID(community.BaseURL)},
	}

	path := fmt.Sprintf("comment/Profile/post/%d/-1", steamID)

	type postCommentResponse struct {
		Success      bool   `json:"success"`
		CommentsHTML string `json:"comments_html"`
		Error        string `json:"error"`
	}

	resp, err := community.PostForm[postCommentResponse](ctx, comm, path, form)
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
func (m *Manager) DeleteUserComment(ctx context.Context, steamID id.ID, commentID string) error {
	m.mu.RLock()
	comm := m.community
	m.mu.RUnlock()

	if comm == nil {
		return errors.New("friends: community requester is not initialized")
	}

	form := url.Values{
		"gidcomment": {commentID},
		"start":      {"0"},
		"count":      {"1"},
		"sessionid":  {comm.SessionID(community.BaseURL)},
		"feature2":   {"-1"},
	}

	path := fmt.Sprintf("comment/Profile/delete/%d/-1", steamID)

	type deleteCommentResponse struct {
		Success      bool   `json:"success"`
		CommentsHTML string `json:"comments_html"`
		Error        string `json:"error"`
	}

	resp, err := community.PostForm[deleteCommentResponse](ctx, comm, path, form)
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
func (m *Manager) GetUserComments(ctx context.Context, steamID id.ID, start, count int) ([]Comment, int, error) {
	m.mu.RLock()
	comm := m.community
	m.mu.RUnlock()

	if comm == nil {
		return nil, 0, errors.New("friends: community requester is not initialized")
	}

	form := url.Values{
		"start":     {strconv.Itoa(start)},
		"count":     {strconv.Itoa(count)},
		"feature2":  {"-1"},
		"sessionid": {comm.SessionID(community.BaseURL)},
	}

	path := fmt.Sprintf("comment/Profile/render/%d/-1", steamID)

	type renderCommentsResponse struct {
		Success      bool   `json:"success"`
		CommentsHTML string `json:"comments_html"`
		TotalCount   int    `json:"total_count"`
		Error        string `json:"error"`
	}

	resp, err := community.PostForm[renderCommentsResponse](ctx, comm, path, form)
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

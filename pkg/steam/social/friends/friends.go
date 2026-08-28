// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package friends manages friend lists, user persona caches, profile comments, and group invitations.
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

	"github.com/lemon4ksan/aoni"
	"github.com/lemon4ksan/aoni/mod"
	log "github.com/lemon4ksan/foundation/async/logkit"
	"github.com/lemon4ksan/foundation/generic"
	"golang.org/x/net/html"
	"google.golang.org/protobuf/proto"

	"github.com/lemon4ksan/g-man/pkg/steam/client"
	"github.com/lemon4ksan/g-man/pkg/steam/community"
	"github.com/lemon4ksan/g-man/pkg/steam/id"
	"github.com/lemon4ksan/g-man/pkg/steam/module"
	"github.com/lemon4ksan/g-man/pkg/steam/protocol/enums"
	"github.com/lemon4ksan/g-man/pkg/steam/service"
	pb "github.com/lemon4ksan/g-man/protobuf/steam"
)

const ModuleName string = "friends"

// WithModule registers the Friends module in the client.
func WithModule() client.Option {
	return client.WithModule(New())
}

// From retrieves the Friends module instance from the client.
func From(c *client.Client) *Manager {
	return client.GetModule[*Manager](c)
}

var (
	// ErrUserNotAFriend indicates group invitations failed because target user is not a friend.
	ErrUserNotAFriend = errors.New("friends: group invite failed: user is not a friend")
	// ErrWebAcceptFailed indicates web-based friend accept request was rejected.
	ErrWebAcceptFailed = errors.New("friends: web accept request unsuccessful")
	// ErrBlockUserFailed indicates web-based block request was rejected.
	ErrBlockUserFailed = errors.New("friends: block user request unsuccessful")
	// ErrCommentNotFound indicates posted comment element was missing from returned HTML.
	ErrCommentNotFound = errors.New("friends: new comment not found in returned HTML")
	// ErrCommentMissingID indicates posted comment element lacked an 'id' attribute.
	ErrCommentMissingID = errors.New("friends: new comment missing id attribute")
	// ErrDeleteCommentInHTML indicates deleted comment element remained in returned HTML payload.
	ErrDeleteCommentInHTML = errors.New("friends: failed to delete comment (comment still in HTML)")
	// ErrCommunityNotInitialized indicates community requester is not configured.
	ErrCommunityNotInitialized = errors.New("friends: community requester is not initialized")
)

// Manager synchronizes relationships, user persona states, and group memberships.
//
// Thread Safety:
//   - Safe for concurrent use across all methods.
type Manager struct {
	module.Base

	client    service.Doer
	community community.Requester
	events    Events

	relationships *generic.ShardedMap[id.ID, enums.EFriendRelationship]
	users         *generic.ShardedMap[id.ID, *PersonaState]
	nicknames     *generic.ShardedMap[id.ID, string]

	mu           sync.RWMutex
	friendGroups map[int32]FriendGroup
	mySteamID    id.ID
	maxFriends   int
}

// New constructs a Manager instance.
func New() *Manager {
	return &Manager{
		Base:          module.New(ModuleName),
		relationships: generic.NewShardedMap[id.ID, enums.EFriendRelationship](),
		users:         generic.NewShardedMap[id.ID, *PersonaState](),
		nicknames:     generic.NewShardedMap[id.ID, string](),
		friendGroups:  make(map[int32]FriendGroup),
	}
}

func (m *Manager) Init(init module.InitContext) error {
	if err := m.Base.Init(init); err != nil {
		return err
	}

	m.client = init.Service()
	m.events = NewEvents(init)

	m.events.OnFriendsList(m.handleFriendsList)
	m.events.OnPersonaState(m.handlePersonaState)
	m.events.OnFriendsGroupsList(m.handleFriendsGroupsList)
	m.events.OnPlayerNicknameList(m.handlePlayerNicknameList)
	m.events.OnNotifyFriendNicknameChanged(m.handleNotifyFriendNicknameChanged)

	return nil
}

func (m *Manager) StartAuthed(ctx context.Context, auth module.AuthContext) error {
	m.mu.Lock()
	m.community = auth.Community()
	m.mySteamID = auth.SteamID()
	m.mu.Unlock()

	return nil
}

func (m *Manager) Close() error {
	if m.events != nil {
		_ = m.events.Close()
	}

	return m.Base.Close()
}

// GetFriend retrieves cached persona state for steamID.
func (m *Manager) GetFriend(steamID id.ID) (*PersonaState, bool) {
	if m == nil || m.users == nil {
		return nil, false
	}

	return m.users.Get(steamID)
}

// IsFriend reports whether steamID exists in our friends list.
func (m *Manager) IsFriend(steamID id.ID) bool {
	if m == nil || m.relationships == nil {
		return false
	}

	enum, ok := m.relationships.Get(steamID)

	return ok && enum == enums.EFriendRelationship_Friend
}

// GetFriends returns all user SteamIDs with active Friend relationships.
func (m *Manager) GetFriends() []id.ID {
	if m == nil || m.relationships == nil {
		return nil
	}

	allRels := m.relationships.All()
	friendsList := make([]id.ID, 0, len(allRels))

	for steamID, relation := range allRels {
		if relation == enums.EFriendRelationship_Friend {
			friendsList = append(friendsList, steamID)
		}
	}

	return friendsList
}

// GetMaxFriends calculates friend slots capacity based on user Steam level.
func (m *Manager) GetMaxFriends(ctx context.Context) (int, error) {
	m.mu.RLock()

	if m.maxFriends > 0 {
		defer m.mu.RUnlock()

		return m.maxFriends, nil
	}

	m.mu.RUnlock()

	req := struct {
		SteamID id.ID `query:"steamid"`
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

// AddFriend sends a friend request or accepts an incoming invite.
func (m *Manager) AddFriend(ctx context.Context, steamID uint64) error {
	return m.events.AddFriend(ctx, &pb.CMsgClientAddFriend{
		SteamidToAdd: &steamID,
	})
}

// RemoveFriend removes a friend or declines an invite.
func (m *Manager) RemoveFriend(ctx context.Context, steamID uint64) error {
	return m.events.RemoveFriend(ctx, &pb.CMsgClientRemoveFriend{
		Friendid: &steamID,
	})
}

// SetPersona updates online status or profile display name.
func (m *Manager) SetPersona(ctx context.Context, state enums.EPersonaState, name string) error {
	req := &pb.CMsgClientChangeStatus{
		PersonaState: proto.Uint32(uint32(state)),
	}

	if name != "" {
		req.PlayerName = proto.String(name)
	}

	return m.events.ChangeStatus(ctx, req)
}

// InviteToGroups sends group invites to a friend.
func (m *Manager) InviteToGroups(ctx context.Context, steamID id.ID, groupIDs []uint64) error {
	client, err := m.ensureAuthenticated()
	if err != nil {
		return err
	}

	if !m.IsFriend(steamID) {
		return ErrUserNotAFriend
	}

	var (
		mu   sync.Mutex
		errs []error
	)

	_ = generic.ParallelForEach(ctx, groupIDs, 5, func(ctx context.Context, groupID uint64) error {
		reqForm := struct {
			JSON    int    `query:"json"`
			Type    string `query:"type"`
			Inviter id.ID  `query:"inviter"`
			Invitee id.ID  `query:"invitee"`
			Group   uint64 `query:"group"`
		}{1, "groupInvite", m.mySteamID, steamID, groupID}

		_, err := community.PostFormTo[service.NoResponse](ctx, client, "actions/GroupInvite", reqForm)
		if err != nil {
			var apiErr *aoni.APIError
			if errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusBadRequest {
				return nil
			}

			m.Logger.Warn("Failed to invite to group", log.Uint64("group_id", groupID), log.Err(err))

			mu.Lock()

			errs = append(errs, err)
			mu.Unlock()
		}

		return nil
	})

	return errors.Join(errs...)
}

// GetFriendGroups returns all custom friend group definitions.
func (m *Manager) GetFriendGroups() map[int32]FriendGroup {
	m.mu.RLock()
	defer m.mu.RUnlock()

	groups := make(map[int32]FriendGroup, len(m.friendGroups))
	maps.Copy(groups, m.friendGroups)

	return groups
}

// GetNicknames returns all assigned friend nicknames.
func (m *Manager) GetNicknames() map[id.ID]string {
	if m == nil || m.nicknames == nil {
		return nil
	}

	all := m.nicknames.All()
	nicks := make(map[id.ID]string, len(all))
	maps.Copy(nicks, all)

	return nicks
}

// GetNickname retrieves a custom nickname for steamID.
func (m *Manager) GetNickname(steamID id.ID) (string, bool) {
	if m == nil || m.nicknames == nil {
		return "", false
	}

	return m.nicknames.Get(steamID)
}

// AcceptFriendRequestWeb accepts a friend request using web endpoints.
func (m *Manager) AcceptFriendRequestWeb(ctx context.Context, steamID id.ID) error {
	client, err := m.ensureAuthenticated()
	if err != nil {
		return err
	}

	reqForm := struct {
		AcceptInvite int   `query:"accept_invite"`
		SteamID      id.ID `query:"steamid"`
	}{1, steamID}

	type respType struct {
		Success bool `json:"success"`
	}

	resp, err := community.PostFormTo[respType](ctx, client, "actions/AddFriendAjax", reqForm)
	if err != nil {
		return fmt.Errorf("friends: web accept request failed: %w", err)
	}

	if !resp.Success {
		return ErrWebAcceptFailed
	}

	return nil
}

// BlockCommunication blocks all communications from steamID.
func (m *Manager) BlockCommunication(ctx context.Context, steamID id.ID) error {
	client, err := m.ensureAuthenticated()
	if err != nil {
		return err
	}

	reqForm := struct {
		SteamID id.ID `query:"steamid"`
	}{steamID}

	type respType struct {
		Success bool `json:"success"`
	}

	resp, err := community.PostFormTo[respType](ctx, client, "actions/BlockUserAjax", reqForm)
	if err != nil {
		return fmt.Errorf("friends: block user request failed: %w", err)
	}

	if !resp.Success {
		return ErrBlockUserFailed
	}

	return nil
}

// UnblockCommunication unblocks communications from steamID.
func (m *Manager) UnblockCommunication(ctx context.Context, steamID id.ID) error {
	client, err := m.ensureAuthenticated()
	if err != nil {
		return err
	}

	m.mu.RLock()
	mySteamID := m.mySteamID
	m.mu.RUnlock()

	form := url.Values{
		"action":                            {"unignore"},
		"friends[" + steamID.String() + "]": {"1"},
	}

	_, err = community.PostFormTo[aoni.NoResponse](
		ctx, client, "profiles/{mySteamID}/friends/blocked", form,
		mod.WithVar("mySteamID", mySteamID),
	)
	if err != nil {
		return fmt.Errorf("friends: unblock request failed: %w", err)
	}

	return nil
}

// PostUserComment posts a text comment on a user profile.
func (m *Manager) PostUserComment(ctx context.Context, steamID id.ID, message string) (string, error) {
	client, err := m.ensureAuthenticated()
	if err != nil {
		return "", err
	}

	reqForm := struct {
		Comment string `query:"comment"`
		Count   int    `query:"count"`
	}{message, 1}

	type respType struct {
		Success      bool   `json:"success"`
		CommentsHTML string `json:"comments_html"`
		Error        string `json:"error"`
	}

	resp, err := community.PostFormTo[respType](
		ctx, client, "comment/Profile/post/{steamID}/-1", reqForm,
		mod.WithVar("steamID", steamID),
	)
	if err != nil {
		return "", fmt.Errorf("friends: post comment request failed: %w", err)
	}

	if !resp.Success {
		return "", fmt.Errorf("friends: post comment failed: %s", generic.Coalesce(resp.Error, "unknown error"))
	}

	doc, err := html.Parse(strings.NewReader(resp.CommentsHTML))
	if err != nil {
		return "", fmt.Errorf("friends: failed to parse comments HTML: %w", err)
	}

	firstComment := findFirstWithClass(doc, "commentthread_comment")
	if firstComment == nil {
		return "", ErrCommentNotFound
	}

	idAttr := getHTMLAttr(firstComment, "id")
	if idAttr == "" {
		return "", ErrCommentMissingID
	}

	parts := strings.Split(idAttr, "_")
	if len(parts) < 2 {
		return "", fmt.Errorf("friends: invalid comment element id format: %s", idAttr)
	}

	return parts[1], nil
}

// DeleteUserComment deletes a profile comment by commentID.
func (m *Manager) DeleteUserComment(ctx context.Context, steamID id.ID, commentID string) error {
	client, err := m.ensureAuthenticated()
	if err != nil {
		return err
	}

	reqForm := struct {
		GIDComment string `query:"gidcomment"`
		Start      int    `query:"start"`
		Count      int    `query:"count"`
		Feature2   int    `query:"feature2"`
	}{commentID, 0, 1, -1}

	type respType struct {
		Success      bool   `json:"success"`
		CommentsHTML string `json:"comments_html"`
		Error        string `json:"error"`
	}

	resp, err := community.PostFormTo[respType](
		ctx, client, "comment/Profile/delete/{steamID}/-1", reqForm,
		mod.WithVar("steamID", steamID),
	)
	if err != nil {
		return fmt.Errorf("friends: delete comment request failed: %w", err)
	}

	if !resp.Success {
		return fmt.Errorf("friends: delete comment failed: %s", generic.Coalesce(resp.Error, "unknown error"))
	}

	if strings.Contains(resp.CommentsHTML, commentID) {
		return ErrDeleteCommentInHTML
	}

	return nil
}

// GetUserComments fetches profile comments for steamID.
func (m *Manager) GetUserComments(ctx context.Context, steamID id.ID, start, count int) ([]Comment, int, error) {
	client, err := m.ensureAuthenticated()
	if err != nil {
		return nil, 0, err
	}

	reqForm := struct {
		Start    int `query:"start"`
		Count    int `query:"count"`
		Feature2 int `query:"feature2"`
	}{start, count, -1}

	type respType struct {
		Success      bool   `json:"success"`
		CommentsHTML string `json:"comments_html"`
		TotalCount   int    `json:"total_count"`
		Error        string `json:"error"`
	}

	resp, err := community.PostFormTo[respType](
		ctx, client, "comment/Profile/render/{steamID}/-1", reqForm,
		mod.WithVar("steamID", steamID),
	)
	if err != nil {
		return nil, 0, fmt.Errorf("friends: render comments request failed: %w", err)
	}

	if !resp.Success {
		return nil, 0, fmt.Errorf("friends: render comments failed: %s", generic.Coalesce(resp.Error, "unknown error"))
	}

	doc, err := html.Parse(strings.NewReader(resp.CommentsHTML))
	if err != nil {
		return nil, 0, fmt.Errorf("friends: failed to parse rendered comments: %w", err)
	}

	var (
		comments []Comment
		walk     func(*html.Node)
	)

	walk = func(n *html.Node) {
		if n.Type == html.ElementNode && hasHTMLClass(n, "commentthread_comment") &&
			hasHTMLClass(n, "responsive_body_text") {
			elID := getHTMLAttr(n, "id")
			if elID != "" {
				parts := strings.Split(elID, "_")
				if len(parts) >= 2 {
					commentID := parts[1]

					var authorSteamID id.ID
					if mpNode := findFirstWithAttr(n, "data-miniprofile"); mpNode != nil {
						miniprofile := getHTMLAttr(mpNode, "data-miniprofile")
						if miniprofile != "" {
							mpID, _ := strconv.ParseUint(miniprofile, 10, 64)
							authorSteamID = id.ID(76561197960265728 + mpID)
						}
					}

					var name string
					if bdiNode := findFirstElement(n, "bdi"); bdiNode != nil {
						name = getHTMLText(bdiNode)
					}

					var avatar string
					if playerAvatarNode := findFirstWithClass(n, "playerAvatar"); playerAvatarNode != nil {
						if imgNode := findFirstElement(playerAvatarNode, "img"); imgNode != nil {
							avatar = getHTMLAttr(imgNode, "src")
						}
					}

					var timestamp time.Time
					if tsNode := findFirstWithClass(n, "commentthread_comment_timestamp"); tsNode != nil {
						tsAttr := getHTMLAttr(tsNode, "data-timestamp")
						if tsAttr != "" {
							unixTS, _ := strconv.ParseInt(tsAttr, 10, 64)
							timestamp = time.Unix(unixTS, 0).UTC()
						}
					}

					var commentText string
					if textNode := findFirstWithClass(n, "commentthread_comment_text"); textNode != nil {
						commentText = strings.TrimSpace(getHTMLText(textNode))
					}

					comments = append(comments, Comment{
						ID:            commentID,
						AuthorSteamID: authorSteamID,
						AuthorName:    name,
						AuthorAvatar:  avatar,
						Date:          timestamp,
						Text:          commentText,
					})
				}
			}
		}

		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(doc)

	return comments, resp.TotalCount, nil
}

func findFirstWithClass(n *html.Node, className string) *html.Node {
	if n == nil {
		return nil
	}

	if n.Type == html.ElementNode && hasHTMLClass(n, className) {
		return n
	}

	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if res := findFirstWithClass(c, className); res != nil {
			return res
		}
	}

	return nil
}

func findFirstWithAttr(n *html.Node, attrName string) *html.Node {
	if n == nil {
		return nil
	}

	if n.Type == html.ElementNode && getHTMLAttr(n, attrName) != "" {
		return n
	}

	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if res := findFirstWithAttr(c, attrName); res != nil {
			return res
		}
	}

	return nil
}

func findFirstElement(n *html.Node, tag string) *html.Node {
	if n == nil {
		return nil
	}

	if n.Type == html.ElementNode && n.Data == tag {
		return n
	}

	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if res := findFirstElement(c, tag); res != nil {
			return res
		}
	}

	return nil
}

func hasHTMLClass(n *html.Node, className string) bool {
	for _, a := range n.Attr {
		if a.Key == "class" {
			for _, c := range strings.Fields(a.Val) {
				if c == className {
					return true
				}
			}
		}
	}

	return false
}

func getHTMLAttr(n *html.Node, key string) string {
	if n == nil {
		return ""
	}

	for _, a := range n.Attr {
		if a.Key == key {
			return a.Val
		}
	}

	return ""
}

func getHTMLText(n *html.Node) string {
	if n == nil {
		return ""
	}

	var (
		sb   strings.Builder
		walk func(*html.Node)
	)

	walk = func(curr *html.Node) {
		if curr.Type == html.TextNode {
			sb.WriteString(curr.Data)
		}

		for c := curr.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(n)

	return sb.String()
}

const (
	UIModeNone       uint32 = 0
	UIModeDesktop    uint32 = 1
	UIModeBigPicture uint32 = 2
	UIModeMobile     uint32 = 3
	UIModeWeb        uint32 = 4
)

// SetUIMode sets active client interface mode.
func (m *Manager) SetUIMode(ctx context.Context, mode uint32) error {
	return m.events.SetUIMode(ctx, &pb.CMsgClientUIMode{
		Uimode: &mode,
	})
}

// UploadRichPresence uploads custom rich presence KeyValues data.
func (m *Manager) UploadRichPresence(ctx context.Context, appID uint32, richPresence map[string]string) error {
	var buf bytes.Buffer

	buf.WriteByte(0)
	buf.Write([]byte("RP\x00"))

	for k, v := range richPresence {
		buf.WriteByte(1)
		buf.Write([]byte(k + "\x00"))
		buf.Write([]byte(v + "\x00"))
	}

	buf.WriteByte(8)
	buf.WriteByte(8)

	return m.events.UploadRichPresence(ctx, &pb.CMsgClientRichPresenceUpload{
		RichPresenceKv: buf.Bytes(),
	})
}

// CreateFriendInviteToken generates a shareable quick-invite link token.
func (m *Manager) CreateFriendInviteToken(ctx context.Context, limit, duration uint32) (string, error) {
	req := &pb.CUserAccount_CreateFriendInviteToken_Request{
		InviteLimit:    &limit,
		InviteDuration: &duration,
	}

	resp, err := service.WebAPI[pb.CUserAccount_CreateFriendInviteToken_Response](
		ctx, m.client, "POST", "UserAccount", "CreateFriendInviteToken", 1, req,
	)
	if err != nil {
		return "", fmt.Errorf("friends: failed to create quick-invite token: %w", err)
	}

	return resp.GetInviteToken(), nil
}

// GetFriendInviteTokens lists active quick-invite tokens.
func (m *Manager) GetFriendInviteTokens(
	ctx context.Context,
) ([]*pb.CUserAccount_CreateFriendInviteToken_Response, error) {
	req := &pb.CUserAccount_GetFriendInviteTokens_Request{}

	resp, err := service.WebAPI[pb.CUserAccount_GetFriendInviteTokens_Response](
		ctx, m.client, "GET", "UserAccount", "GetFriendInviteTokens", 1, req,
	)
	if err != nil {
		return nil, fmt.Errorf("friends: failed to list quick-invite tokens: %w", err)
	}

	return resp.GetTokens(), nil
}

// RevokeFriendInviteToken invalidates a quick-invite token.
func (m *Manager) RevokeFriendInviteToken(ctx context.Context, token string) error {
	req := &pb.CUserAccount_RevokeFriendInviteToken_Request{
		InviteToken: &token,
	}

	_, err := service.WebAPI[service.NoResponse](
		ctx, m.client, "POST", "UserAccount", "RevokeFriendInviteToken", 1, req,
	)
	if err != nil {
		return fmt.Errorf("friends: failed to revoke quick-invite token: %w", err)
	}

	return nil
}

// ViewFriendInviteToken views details for a quick-invite token owned by steamID.
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
		ctx, m.client, "GET", "UserAccount", "ViewFriendInviteToken", 1, req,
	)
	if err != nil {
		return nil, fmt.Errorf("friends: failed to view quick-invite token: %w", err)
	}

	return resp, nil
}

// SetFriendNickname assigns a custom nickname to steamID.
func (m *Manager) SetFriendNickname(ctx context.Context, steamID uint64, nickname string) error {
	req := &pb.CMsgClientSetPlayerNickname{
		Steamid:  proto.Uint64(steamID),
		Nickname: proto.String(nickname),
	}

	resp, err := service.LegacyProto[pb.CMsgClientSetPlayerNicknameResponse](
		ctx, m.client, enums.EMsg_AMClientSetPlayerNickname, req,
	)
	if err != nil {
		return fmt.Errorf("friends: failed to set player nickname: %w", err)
	}

	if enums.EResult(resp.GetEresult()) != enums.EResult_OK {
		return fmt.Errorf("friends: failed to set player nickname: steam error EResult %d", resp.GetEresult())
	}

	return nil
}

func (m *Manager) handleFriendsGroupsList(list *pb.CMsgClientFriendsGroupsList) {
	if list == nil {
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

func (m *Manager) handlePlayerNicknameList(list *pb.CMsgClientPlayerNicknameList) {
	if list == nil {
		return
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	for _, user := range list.GetNicknames() {
		steamID := id.ID(user.GetSteamid())
		if list.GetRemoval() {
			m.nicknames.Delete(steamID)
		} else {
			m.nicknames.Set(steamID, user.GetNickname())
		}
	}

	if !list.GetIncremental() {
		m.Bus.Publish(&NicknameListEvent{
			Nicknames: m.nicknames.All(),
		})
	}
}

func (m *Manager) handleNotifyFriendNicknameChanged(msg *pb.CPlayer_FriendNicknameChanged_Notification) {
	if msg == nil {
		return
	}

	sid := id.FromAccountID(msg.GetAccountid())
	nickname := msg.GetNickname()

	m.mu.Lock()
	if _, ok := m.relationships.Get(id.ID(msg.GetAccountid())); ok {
		sid = id.ID(msg.GetAccountid())
	}

	if nickname == "" {
		m.nicknames.Delete(sid)
	} else {
		m.nicknames.Set(sid, nickname)
	}

	m.mu.Unlock()

	m.Bus.Publish(&NicknameChangedEvent{
		SteamID:  sid,
		Nickname: nickname,
	})
}

func (m *Manager) handleFriendsList(list *pb.CMsgClientFriendsList) {
	if list == nil {
		return
	}

	var events []RelationshipChangedEvent

	m.mu.Lock()
	for _, friend := range list.GetFriends() {
		steamID := id.ID(friend.GetUlfriendid())
		newRel := enums.EFriendRelationship(friend.GetEfriendrelationship())
		oldRel, _ := m.relationships.Get(steamID)
		m.relationships.Set(steamID, newRel)

		if oldRel != newRel {
			events = append(events, RelationshipChangedEvent{
				SteamID: steamID,
				Old:     oldRel,
				New:     newRel,
			})
		}
	}

	m.mu.Unlock()

	for i := range events {
		m.Bus.Publish(&events[i])
	}
}

func (m *Manager) handlePersonaState(state *pb.CMsgClientPersonaState) {
	if state == nil {
		return
	}

	var events []PersonaStateUpdatedEvent

	m.mu.Lock()
	for _, friend := range state.GetFriends() {
		steamID := id.ID(friend.GetFriendid())

		user, exists := m.users.Get(steamID)
		if !exists {
			user = &PersonaState{RichPresence: make(map[string]string)}
			m.users.Set(steamID, user)
		}

		if friend.PlayerName != nil {
			user.PlayerName = friend.GetPlayerName()
		}

		if friend.AvatarHash != nil {
			user.AvatarHash = friend.GetAvatarHash()
		}

		events = append(events, PersonaStateUpdatedEvent{
			SteamID: steamID,
			State:   user,
		})
	}

	m.mu.Unlock()

	for i := range events {
		m.Bus.Publish(&events[i])
	}
}

func (m *Manager) ensureAuthenticated() (community.Requester, error) {
	m.mu.RLock()
	comm := m.community
	m.mu.RUnlock()

	if comm == nil {
		return nil, ErrCommunityNotInitialized
	}

	return comm, nil
}

// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package friends

import (
	"context"
	"net/url"
	"strconv"
	"strings"
	"sync"

	"github.com/lemon4ksan/g-man/pkg/log"
	"github.com/lemon4ksan/g-man/pkg/steam"
	"github.com/lemon4ksan/g-man/pkg/steam/api"
	"github.com/lemon4ksan/g-man/pkg/steam/bus"
	pb "github.com/lemon4ksan/g-man/pkg/steam/protobuf"
	"github.com/lemon4ksan/g-man/pkg/steam/protocol"
	"google.golang.org/protobuf/proto"
)

const ModuleName string = "friends"

// Manager manages the friends list, user cache, and groups.
type Manager struct {
	bus       *bus.Bus
	logger    log.Logger
	web       api.WebAPIRequester
	proto     api.LegacyRequester
	community api.CommunityRequester

	mu            sync.RWMutex
	relationships map[uint64]protocol.EFriendRelationship
	users         map[uint64]*PersonaState

	mySteamID  uint64
	maxFriends int
	closeFunc  func()
}

// New creates a new instance of the friends module.
func New() *Manager {
	return &Manager{
		relationships: make(map[uint64]protocol.EFriendRelationship),
		users:         make(map[uint64]*PersonaState),
	}
}

func (m *Manager) Name() string {
	return ModuleName
}

func (m *Manager) Init(init steam.InitContext) error {
	m.bus = init.Bus()
	m.logger = init.Logger().WithModule(ModuleName)
	m.web = init.WebAPI()
	m.proto = init.Proto()

	init.RegisterPacketHandler(protocol.EMsg_ClientFriendsList, m.handleFriendsList)
	init.RegisterPacketHandler(protocol.EMsg_ClientPersonaState, m.handlePersonaState)

	m.closeFunc = func() {
		init.UnregisterPacketHandler(protocol.EMsg_ClientFriendsList)
		init.UnregisterPacketHandler(protocol.EMsg_ClientPersonaState)
	}

	return nil
}

func (m *Manager) StartAuthed(ctx context.Context, auth steam.AuthContext) error {
	m.community = auth.Community()
	m.mySteamID = auth.SteamID()
	return nil
}

func (m *Manager) Start(ctx context.Context) error {
	return nil
}

func (m *Manager) Close() error {
	if m.closeFunc != nil {
		m.closeFunc()
		m.closeFunc = nil
	}
	return nil
}

// GetFriend returns cached user information or nil.
func (m *Manager) GetFriend(steamID uint64) *PersonaState {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.users[steamID]
}

// IsFriend checks if the user is our friend.
func (m *Manager) IsFriend(steamID uint64) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.relationships[steamID] == protocol.EFriendRelationship_Friend
}

// GetFriends returns a list of SteamID64s of all current friends.
func (m *Manager) GetFriends() []uint64 {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var friends []uint64
	for steamID, relation := range m.relationships {
		if relation == protocol.EFriendRelationship_Friend {
			friends = append(friends, steamID)
		}
	}
	return friends
}

// GetMaxFriends calculates the maximum number of friends based on Steam level.
func (m *Manager) GetMaxFriends(ctx context.Context) (int, error) {
	m.mu.RLock()
	if m.maxFriends > 0 {
		m.mu.RUnlock()
		return m.maxFriends, nil
	}
	m.mu.RUnlock()

	var resp GetBadgesResponse
	err := m.web.CallWebAPI(ctx, "GET", "IPlayerService", "GetBadges", 1, &resp,
		api.WithParam("steamid", strconv.FormatUint(m.mySteamID, 10)),
	)
	if err != nil {
		return 0, err
	}

	level := resp.Response.PlayerLevel
	base := 250
	multiplier := 5
	max := base + (level * multiplier)

	m.mu.Lock()
	m.maxFriends = max
	m.mu.Unlock()

	return max, nil
}

// AddFriend sends a friend request or accepts an incoming request.
func (m *Manager) AddFriend(ctx context.Context, steamID uint64) error {
	req := &pb.CMsgClientAddFriend{
		SteamidToAdd: &steamID,
	}
	return m.proto.CallLegacy(ctx, protocol.EMsg_ClientAddFriend, req, nil)
}

// RemoveFriend removes a user from friends or rejects an incoming request.
func (m *Manager) RemoveFriend(ctx context.Context, steamID uint64) error {
	req := &pb.CMsgClientRemoveFriend{
		Friendid: &steamID,
	}
	return m.proto.CallLegacy(ctx, protocol.EMsg_ClientRemoveFriend, req, nil)
}

// InviteToGroups sends invitations to the specified groups.
// Returns an error only if there is a system failure. 400 (already invited) errors are ignored.
func (m *Manager) InviteToGroups(ctx context.Context, steamID uint64, groupIDs []uint64) {
	if !m.IsFriend(steamID) {
		m.logger.Debug("Skipping group invite, user is not a friend", log.Uint64("steamID", steamID))
		return
	}

	for _, groupID := range groupIDs {
		data := url.Values{
			"json":    {"1"},
			"type":    {"groupInvite"},
			"inviter": {strconv.FormatUint(m.mySteamID, 10)},
			"invitee": {strconv.FormatUint(steamID, 10)},
			"group":   {strconv.FormatUint(groupID, 10)},
		}

		_, err := m.community.PostForm(ctx, "actions/GroupInvite", data)
		if err != nil {
			// Steam returns 400 if the user is already in the group, already invited, or banned.
			if strings.Contains(err.Error(), "400") {
				m.logger.Debug("Ignored HTTP 400 while inviting to group",
					log.Uint64("steamID", steamID),
					log.Uint64("groupID", groupID),
				)
				continue
			}

			m.logger.Warn("Failed to invite to group",
				log.Uint64("steamID", steamID),
				log.Uint64("groupID", groupID),
				log.Err(err),
			)
			continue
		}

		m.logger.Debug("Successfully invited user to group", log.Uint64("steamID", steamID), log.Uint64("groupID", groupID))
	}
}

// handleFriendsList is called upon login and any changes to the list (adding/deleting).
func (m *Manager) handleFriendsList(packet *protocol.Packet) {
	var list pb.CMsgClientFriendsList
	if err := proto.Unmarshal(packet.Payload, &list); err != nil {
		m.logger.Error("Failed to unmarshal friends list", log.Err(err))
		return
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	for _, friend := range list.GetFriends() {
		steamID := friend.GetUlfriendid()
		newRel := protocol.EFriendRelationship(friend.GetEfriendrelationship())
		oldRel := m.relationships[steamID]

		m.relationships[steamID] = newRel

		if oldRel != newRel {
			m.bus.Publish(&RelationshipChangedEvent{
				SteamID: steamID,
				Old:     oldRel,
				New:     newRel,
			})
		}
	}
}

// handleFriendsList is called upon login and any changes to the list (adding/deleting).
func (m *Manager) handlePersonaState(packet *protocol.Packet) {
	var state pb.CMsgClientPersonaState
	if err := proto.Unmarshal(packet.Payload, &state); err != nil {
		m.logger.Error("Failed to unmarshal persona state", log.Err(err))
		return
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	for _, friend := range state.GetFriends() {
		steamID := friend.GetFriendid()

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

		m.bus.Publish(&PersonaStateUpdatedEvent{
			SteamID: steamID,
			State:   user,
		})
	}
}

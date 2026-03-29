// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package friends manages the Steam friends list, persona states, and group invitations.
package friends

import (
	"context"
	"strings"
	"sync"

	"github.com/lemon4ksan/g-man/pkg/log"
	"github.com/lemon4ksan/g-man/pkg/modules"
	"github.com/lemon4ksan/g-man/pkg/steam"
	"github.com/lemon4ksan/g-man/pkg/steam/community"
	pb "github.com/lemon4ksan/g-man/pkg/steam/protobuf"
	"github.com/lemon4ksan/g-man/pkg/steam/protocol"
	"github.com/lemon4ksan/g-man/pkg/steam/service"
	"google.golang.org/protobuf/proto"
)

const ModuleName string = "friends"

func WithModule() steam.Option {
    return func(c *steam.Client) {
        c.RegisterModule(New())
    }
}

// Manager handles friends list synchronization and user status tracking.
// It embeds modules.BaseModule for standardized lifecycle management.
type Manager struct {
	modules.BaseModule

	// Dependencies
	client    service.Requester
	community community.Requester

	// State
	mu            sync.RWMutex
	relationships map[uint64]protocol.EFriendRelationship
	users         map[uint64]*PersonaState

	mySteamID  uint64
	maxFriends int

	unregFuncs []func()
}

// New creates a new instance of the friends manager.
func New() *Manager {
	return &Manager{
		BaseModule:    modules.NewBase(ModuleName),
		relationships: make(map[uint64]protocol.EFriendRelationship),
		users:         make(map[uint64]*PersonaState),
	}
}

// Init registers packet handlers and sets up module dependencies.
func (m *Manager) Init(init modules.InitContext) error {
	if err := m.BaseModule.Init(init); err != nil {
		return err
	}

	m.client = init.Service()

	init.RegisterPacketHandler(protocol.EMsg_ClientFriendsList, m.handleFriendsList)
	init.RegisterPacketHandler(protocol.EMsg_ClientPersonaState, m.handlePersonaState)

	m.unregFuncs = append(m.unregFuncs, func() {
		init.UnregisterPacketHandler(protocol.EMsg_ClientFriendsList)
		init.UnregisterPacketHandler(protocol.EMsg_ClientPersonaState)
	})

	return nil
}

// StartAuthed is called when the client is logged in and ready.
func (m *Manager) StartAuthed(ctx context.Context, auth modules.AuthContext) error {
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
	return m.BaseModule.Close()
}

// GetFriend returns cached user information (persona state) for a given SteamID.
func (m *Manager) GetFriend(steamID uint64) *PersonaState {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.users[steamID]
}

// IsFriend returns true if the specified SteamID is in our friends list.
func (m *Manager) IsFriend(steamID uint64) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.relationships[steamID] == protocol.EFriendRelationship_Friend
}

// GetFriends returns a list of SteamIDs for all users with a "Friend" relationship.
func (m *Manager) GetFriends() []uint64 {
	m.mu.RLock()
	defer m.mu.RUnlock()

	friends := make([]uint64, 0, len(m.relationships))
	for steamID, relation := range m.relationships {
		if relation == protocol.EFriendRelationship_Friend {
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
		SteamID uint64 `url:"steamid"`
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
	_, err := service.Legacy[any](ctx, m.client, protocol.EMsg_ClientAddFriend, req)
	return err
}

// RemoveFriend removes a friend or rejects an incoming request.
func (m *Manager) RemoveFriend(ctx context.Context, steamID uint64) error {
	req := &pb.CMsgClientRemoveFriend{
		Friendid: &steamID,
	}
	_, err := service.Legacy[any](ctx, m.client, protocol.EMsg_ClientRemoveFriend, req)
	return err
}

// InviteToGroups sends group invitations to a friend.
// Standard HTTP 400 errors (already in group/already invited) are ignored.
func (m *Manager) InviteToGroups(ctx context.Context, steamID uint64, groupIDs []uint64) {
	if !m.IsFriend(steamID) {
		m.Logger.Debug("Skipping group invite: user is not a friend", log.Uint64("steam_id", steamID))
		return
	}

	for _, groupID := range groupIDs {
		req := struct {
			JSON    int    `url:"json"`
			Type    string `url:"type"`
			Inviter uint64 `url:"inviter"`
			Invitee uint64 `url:"invitee"`
			Group   uint64 `url:"group"`
		}{1, "groupInvite", m.mySteamID, steamID, groupID}

		_, err := community.PostForm[any](ctx, m.community, "actions/GroupInvite", req)
		if err != nil {
			if strings.Contains(err.Error(), "400") {
				continue
			}
			m.Logger.Warn("Failed to invite to group", log.Uint64("group_id", groupID), log.Err(err))
			continue
		}
		m.Logger.Debug("Invited user to group", log.Uint64("steam_id", steamID), log.Uint64("group_id", groupID))
	}
}

// --- Handlers ---

func (m *Manager) handleFriendsList(packet *protocol.Packet) {
	list := &pb.CMsgClientFriendsList{}
	if err := proto.Unmarshal(packet.Payload, list); err != nil {
		m.Logger.Error("Failed to unmarshal friends list", log.Err(err))
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

		m.Bus.Publish(&PersonaStateUpdatedEvent{
			SteamID: steamID,
			State:   user,
		})
	}
}

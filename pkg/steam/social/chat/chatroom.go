// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package chat

import (
	"context"

	"google.golang.org/protobuf/proto"

	pb "github.com/lemon4ksan/g-man/pkg/protobuf/steam"
	"github.com/lemon4ksan/g-man/pkg/steam/service"
)

// SendChatMessage sends a message to a specific modern chat room group channel.
func (m *Chat) SendChatMessage(ctx context.Context, chatGroupID, chatID uint64, message string) error {
	if err := m.applyRateLimit(); err != nil {
		return err
	}

	req := &pb.CChatRoom_SendChatMessage_Request{
		ChatGroupId: proto.Uint64(chatGroupID),
		ChatId:      proto.Uint64(chatID),
		Message:     proto.String(message),
	}

	_, err := service.Unified[pb.CChatRoom_SendChatMessage_Response](ctx, m.service, req)

	return err
}

// SendChatReaction updates (adds or removes) a reaction to a specific message in a modern chat room channel.
func (m *Chat) SendChatReaction(
	ctx context.Context,
	chatGroupID, chatID uint64,
	serverTimestamp, ordinal uint32,
	reaction string,
	reactionType pb.EChatRoomMessageReactionType,
	isAdd bool,
) error {
	req := &pb.CChatRoom_UpdateMessageReaction_Request{
		ChatGroupId:     proto.Uint64(chatGroupID),
		ChatId:          proto.Uint64(chatID),
		ServerTimestamp: proto.Uint32(serverTimestamp),
		Ordinal:         proto.Uint32(ordinal),
		ReactionType:    &reactionType,
		Reaction:        proto.String(reaction),
		IsAdd:           proto.Bool(isAdd),
	}

	_, err := service.Unified[pb.CChatRoom_UpdateMessageReaction_Response](ctx, m.service, req)

	return err
}

// GetChatHistory retrieves chat history for a given modern chat room group channel with pagination options.
func (m *Chat) GetChatHistory(
	ctx context.Context,
	chatGroupID, chatID uint64,
	startTime, startOrdinal, maxCount uint32,
) ([]*pb.CChatRoom_GetMessageHistory_Response_ChatMessage, error) {
	req := &pb.CChatRoom_GetMessageHistory_Request{
		ChatGroupId:  proto.Uint64(chatGroupID),
		ChatId:       proto.Uint64(chatID),
		StartTime:    proto.Uint32(startTime),
		StartOrdinal: proto.Uint32(startOrdinal),
		MaxCount:     proto.Uint32(maxCount),
	}

	resp, err := service.Unified[pb.CChatRoom_GetMessageHistory_Response](ctx, m.service, req)
	if err != nil {
		return nil, err
	}

	return resp.GetMessages(), nil
}

// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package custom

import (
	protoreflect "google.golang.org/protobuf/reflect/protoreflect"
	protoimpl "google.golang.org/protobuf/runtime/protoimpl"
)

// CMsgClientStoreUserStats is a low-level structure for updating achievements and statistics.
// Corresponds to EMsg_ClientStoreUserStats (820).
type CMsgClientStoreUserStats struct {
	state         protoimpl.MessageState
	sizeCache     protoimpl.SizeCache
	unknownFields protoimpl.UnknownFields

	GameId        *uint64                                 `protobuf:"fixed64,1,opt,name=game_id"`
	ExplicitReset *bool                                   `protobuf:"varint,2,opt,name=explicit_reset"`
	Stats         []*CMsgClientStoreUserStats_Stat        `protobuf:"bytes,3,rep,name=stats_to_store"`
	Achievements  []*CMsgClientStoreUserStats_Achievement `protobuf:"bytes,6,rep,name=achievement_blocks"`
}

func (x *CMsgClientStoreUserStats) Reset()         { *x = CMsgClientStoreUserStats{} }
func (x *CMsgClientStoreUserStats) String() string { return protoimpl.X.MessageStringOf(x) }
func (*CMsgClientStoreUserStats) ProtoMessage()    {}
func (x *CMsgClientStoreUserStats) ProtoReflect() protoreflect.Message {
	return protoimpl.X.MessageOf(x)
}

// CMsgClientStoreUserStats_Stat is a low-level structure for updating statistics.
// Corresponds to EMsg_ClientStoreUserStats (820).
type CMsgClientStoreUserStats_Stat struct {
	state     protoimpl.MessageState
	StatId    *uint32 `protobuf:"varint,1,opt,name=stat_id"`
	StatValue *uint32 `protobuf:"varint,2,opt,name=stat_value"`
}

func (x *CMsgClientStoreUserStats_Stat) Reset()      { *x = CMsgClientStoreUserStats_Stat{} }
func (*CMsgClientStoreUserStats_Stat) ProtoMessage() {}
func (x *CMsgClientStoreUserStats_Stat) ProtoReflect() protoreflect.Message {
	return protoimpl.X.MessageOf(x)
}

// CMsgClientStoreUserStats_Achievement is a low-level structure for updating achievements.
// Corresponds to EMsg_ClientStoreUserStats (820).
type CMsgClientStoreUserStats_Achievement struct {
	state         protoimpl.MessageState
	AchievementId *uint32  `protobuf:"varint,1,opt,name=achievement_id"`
	UnlockTime    []uint32 `protobuf:"fixed32,2,rep,name=unlock_time"`
}

func (x *CMsgClientStoreUserStats_Achievement) Reset()      { *x = CMsgClientStoreUserStats_Achievement{} }
func (*CMsgClientStoreUserStats_Achievement) ProtoMessage() {}
func (x *CMsgClientStoreUserStats_Achievement) ProtoReflect() protoreflect.Message {
	return protoimpl.X.MessageOf(x)
}

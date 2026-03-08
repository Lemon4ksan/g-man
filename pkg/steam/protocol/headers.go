// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package protocol

import (
	"encoding/binary"
	"io"
	"math"

	pb "github.com/lemon4ksan/g-man/pkg/steam/protocol/protobuf"
	"google.golang.org/protobuf/proto"
)

const (
	NoJob     uint64 = math.MaxUint64
	ProtoMask uint32 = 0x80000000
	EMsgMask  uint32 = ^ProtoMask
)

// Header describes the common interface for all Steam header types.
type Header interface {
	GetSourceJob() uint64
	GetTargetJob() uint64
	SerializeTo(w io.Writer) error
}

// AuthorizedHeader describes a header that contains steamID and SessionID.
type AuthorizedHeader interface {
	GetSteamID() uint64
	GetSessionID() int32
}

// EHeader describes a header that has a [EResult].
type EHeader interface {
	GetEResult() EResult
}

type MsgHdr struct {
	EMsg        EMsg
	TargetJobID uint64
	SourceJobID uint64
}

// NewMsgHdr creates a new message header with the specified EMsg and target job ID.
// SourceJobID is automatically set to [NoJob].
func NewMsgHdr(eMsg EMsg, targetJobID uint64) *MsgHdr {
	return &MsgHdr{
		EMsg:        eMsg,
		TargetJobID: targetJobID,
		SourceJobID: NoJob,
	}
}

func (h *MsgHdr) GetSourceJob() uint64 { return h.SourceJobID }
func (h *MsgHdr) GetTargetJob() uint64 { return h.TargetJobID }
func (h *MsgHdr) SerializeTo(w io.Writer) error {
	buf := make([]byte, 20)
	binary.LittleEndian.PutUint32(buf[0:4], uint32(h.EMsg))
	binary.LittleEndian.PutUint64(buf[4:12], h.TargetJobID)
	binary.LittleEndian.PutUint64(buf[12:20], h.SourceJobID)
	_, err := w.Write(buf)
	return err
}

// Constants for the extended message header format.
const (
	HeaderSizeExtended = 36   // Total size of extended header in bytes
	HeaderVersion      = 2    // Version number for extended header
	HeaderCanary       = 0xEF // Magic value to verify header integrity
)

type MsgHdrExtended struct {
	EMsg         EMsg
	HeaderSize   byte   // 36
	HeaderVer    uint16 // 2
	TargetJobID  uint64
	SourceJobID  uint64
	HeaderCanary byte // 239
	SteamID      uint64
	SessionID    int32
}

// NewMsgHdrExtended creates a new extended header with the specified parameters.
// TargetJobID and SourceJobID are set to [NoJob] by default.
func NewMsgHdrExtended(eMsg EMsg, steamID uint64, sessionID int32) *MsgHdrExtended {
	return &MsgHdrExtended{
		EMsg:         eMsg,
		HeaderSize:   HeaderSizeExtended,
		HeaderVer:    HeaderVersion,
		TargetJobID:  NoJob,
		SourceJobID:  NoJob,
		HeaderCanary: HeaderCanary,
		SteamID:      steamID,
		SessionID:    sessionID,
	}
}

func (h *MsgHdrExtended) GetSourceJob() uint64 { return h.SourceJobID }
func (h *MsgHdrExtended) GetTargetJob() uint64 { return h.TargetJobID }
func (h *MsgHdrExtended) GetSteamID() uint64   { return h.SteamID }
func (h *MsgHdrExtended) GetSessionID() int32  { return h.SessionID }
func (h *MsgHdrExtended) SerializeTo(w io.Writer) error {
	buf := make([]byte, 36)
	binary.LittleEndian.PutUint32(buf[0:4], uint32(h.EMsg))
	buf[4] = 36
	binary.LittleEndian.PutUint16(buf[5:7], 2)
	binary.LittleEndian.PutUint64(buf[7:15], h.TargetJobID)
	binary.LittleEndian.PutUint64(buf[15:23], h.SourceJobID)
	buf[23] = 239
	binary.LittleEndian.PutUint64(buf[24:32], h.SteamID)
	binary.LittleEndian.PutUint32(buf[32:36], uint32(h.SessionID))
	_, err := w.Write(buf)
	return err
}

type MsgHdrProtoBuf struct {
	EMsg  EMsg
	Proto *pb.CMsgProtoBufHeader
}

// NewMsgHdrProtoBuf creates a new protobuf-style header.
// and job IDs are set to [NoJob].
func NewMsgHdrProtoBuf(eMsg EMsg, steamID uint64, sessionID int32) *MsgHdrProtoBuf {
	return &MsgHdrProtoBuf{
		EMsg: eMsg,
		Proto: &pb.CMsgProtoBufHeader{
			Steamid:         proto.Uint64(steamID),
			ClientSessionid: proto.Int32(sessionID),
			JobidSource:     proto.Uint64(NoJob),
			JobidTarget:     proto.Uint64(NoJob),
		},
	}
}

func (h *MsgHdrProtoBuf) GetSourceJob() uint64 { return h.Proto.GetJobidSource() }
func (h *MsgHdrProtoBuf) GetTargetJob() uint64 { return h.Proto.GetJobidTarget() }
func (h *MsgHdrProtoBuf) GetSteamID() uint64   { return h.Proto.GetSteamid() }
func (h *MsgHdrProtoBuf) GetSessionID() int32  { return h.Proto.GetClientSessionid() }
func (h *MsgHdrProtoBuf) GetEResult() EResult  { return EResult(h.Proto.GetEresult()) }
func (h *MsgHdrProtoBuf) SerializeTo(w io.Writer) error {
	protoData, err := proto.Marshal(h.Proto)
	if err != nil {
		return err
	}

	buf := make([]byte, 8)
	binary.LittleEndian.PutUint32(buf[0:4], uint32(h.EMsg)|ProtoMask)
	binary.LittleEndian.PutUint32(buf[4:8], uint32(len(protoData)))

	if _, err := w.Write(buf); err != nil {
		return err
	}
	_, err = w.Write(protoData)
	return err
}

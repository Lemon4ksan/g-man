// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package protocol

import (
	"encoding/binary"
	"fmt"
	"io"
	"math"

	pb "github.com/lemon4ksan/g-man/pkg/protobuf/steam"
	"google.golang.org/protobuf/proto"
)

const (
	// NoJob is a sentinel value used to indicate that a message is not part
	// of an asynchronous job chain. It represents the maximum uint64 value.
	NoJob uint64 = math.MaxUint64

	// ProtoMask is a bit flag applied to the EMsg (message type) to indicate
	// that the message body and header are encoded using Protobuf.
	ProtoMask uint32 = 0x80000000

	// EMsgMask is used to strip the ProtoMask bit and retrieve the actual
	// EMsg numeric value.
	EMsgMask uint32 = ^ProtoMask
)

// MsgHdr (Standard Header) is a basic header format primarily used during
// the initial connection phase and encryption handshake.
// It does not contain SteamID or SessionID.
type MsgHdr struct {
	EMsg        EMsg
	TargetJobID uint64
	SourceJobID uint64
}

// NewMsgHdr creates a new standard message header with the specified EMsg
// and target job ID. SourceJobID is automatically initialized to [NoJob].
func NewMsgHdr(eMsg EMsg, targetJobID uint64) *MsgHdr {
	return &MsgHdr{
		EMsg:        eMsg,
		TargetJobID: targetJobID,
		SourceJobID: NoJob,
	}
}

func (h *MsgHdr) GetSourceJob() uint64 { return h.SourceJobID }
func (h *MsgHdr) GetTargetJob() uint64 { return h.TargetJobID }

// SerializeTo writes the 20-byte standard header to the provided writer.
func (h *MsgHdr) SerializeTo(w io.Writer) error {
	var buf [20]byte
	binary.LittleEndian.PutUint32(buf[0:4], uint32(h.EMsg))
	binary.LittleEndian.PutUint64(buf[4:12], h.TargetJobID)
	binary.LittleEndian.PutUint64(buf[12:20], h.SourceJobID)
	_, err := w.Write(buf[:])
	return err
}

func (h *MsgHdr) Deserialize(r io.Reader) error {
	var jobIDs [16]byte
	if _, err := io.ReadFull(r, jobIDs[:]); err != nil {
		return err
	}
	h.TargetJobID = binary.LittleEndian.Uint64(jobIDs[0:8])
	h.SourceJobID = binary.LittleEndian.Uint64(jobIDs[8:16])
	return nil
}

const (
	// HeaderSizeExtended is the fixed size (36 bytes) of a legacy extended header.
	HeaderSizeExtended = 36
	// HeaderVersion is the protocol version for extended headers.
	HeaderVersion = 2
	// HeaderCanary is a magic byte (0xEF) used to verify header integrity.
	HeaderCanary = 0xEF
	// MaxPayloadSize is the maximum allowed payload size.
	// Packages should never exceed this limit.
	MaxPayloadSize = 16 * 1024 * 1024
	// MaxHeaderSize is the maximum allowed header size.
	// Parsed packages should never exceed this limit.
	MaxHeaderSize = 1024 * 1024
)

// MsgHdrExtended (Extended Header) is used for legacy Steam messages that require
// session state (SteamID and SessionID) but do not use Protobuf.
type MsgHdrExtended struct {
	EMsg         EMsg
	HeaderSize   byte   // Always HeaderSizeExtended (36)
	HeaderVer    uint16 // Always HeaderVersion (2)
	TargetJobID  uint64
	SourceJobID  uint64
	HeaderCanary byte // Always HeaderCanary (239)
	SteamID      uint64
	SessionID    int32
}

// NewMsgHdrExtended creates an extended header for authorized messages.
// Both Job IDs are initialized to [NoJob].
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

func (h *MsgHdrExtended) GetSteamID() uint64  { return h.SteamID }
func (h *MsgHdrExtended) GetSessionID() int32 { return h.SessionID }

// SerializeTo writes the 36-byte extended header to the provided writer.
func (h *MsgHdrExtended) SerializeTo(w io.Writer) error {
	var buf [HeaderSizeExtended]byte
	binary.LittleEndian.PutUint32(buf[0:4], uint32(h.EMsg))
	buf[4] = HeaderSizeExtended
	binary.LittleEndian.PutUint16(buf[5:7], HeaderVersion)
	binary.LittleEndian.PutUint64(buf[7:15], h.TargetJobID)
	binary.LittleEndian.PutUint64(buf[15:23], h.SourceJobID)
	buf[23] = HeaderCanary
	binary.LittleEndian.PutUint64(buf[24:32], h.SteamID)
	binary.LittleEndian.PutUint32(buf[32:36], uint32(h.SessionID))
	_, err := w.Write(buf[:])
	return err
}

// Deserialize reads the extended header fields from an io.Reader.
// Note: It assumes the EMsg (first 4 bytes) has already been read to determine the header type.
func (h *MsgHdrExtended) Deserialize(r io.Reader) error {
	var data [HeaderSizeExtended - 4]byte
	if _, err := io.ReadFull(r, data[:]); err != nil {
		return err
	}
	h.HeaderSize = data[0]
	if h.HeaderSize != HeaderSizeExtended {
		return fmt.Errorf("%w: invalid header size: %d", ErrInvalidHeader, h.HeaderSize)
	}
	h.HeaderVer = binary.LittleEndian.Uint16(data[1:3])
	if h.HeaderVer != HeaderVersion {
		return fmt.Errorf("%w: invalid header version: %d", ErrInvalidHeader, h.HeaderVer)
	}
	h.TargetJobID = binary.LittleEndian.Uint64(data[3:11])
	h.SourceJobID = binary.LittleEndian.Uint64(data[11:19])
	h.HeaderCanary = data[19]
	if h.HeaderCanary != HeaderCanary {
		return fmt.Errorf("%w: invalid header canary: %x", ErrInvalidHeader, h.HeaderCanary)
	}
	h.SteamID = binary.LittleEndian.Uint64(data[20:28])
	h.SessionID = int32(binary.LittleEndian.Uint32(data[28:32]))
	return nil
}

// MsgHdrProtoBuf is the modern Steam header format. It wraps
// a Protobuf message containing routing and session metadata.
type MsgHdrProtoBuf struct {
	EMsg  EMsg
	Proto *pb.CMsgProtoBufHeader
}

// NewMsgHdrProtoBuf creates a modern Protobuf-style header.
// It initializes a default CMsgProtoBufHeader with the provided session info
// and sets Job IDs to [NoJob].
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

func (h *MsgHdrProtoBuf) GetSteamID() uint64  { return h.Proto.GetSteamid() }
func (h *MsgHdrProtoBuf) GetSessionID() int32 { return h.Proto.GetClientSessionid() }

// GetEResult returns the result code from the header if present.
func (h *MsgHdrProtoBuf) GetEResult() EResult { return EResult(h.Proto.GetEresult()) }

// SerializeTo marshals the Protobuf header and writes it to the writer,
// preceded by the EMsg (with ProtoMask set) and the header length.
func (h *MsgHdrProtoBuf) SerializeTo(w io.Writer) error {
	protoData, err := proto.Marshal(h.Proto)
	if err != nil {
		return err
	}

	var buf [8]byte
	// Set the highest bit to signify this is a Protobuf message
	binary.LittleEndian.PutUint32(buf[0:4], uint32(h.EMsg)|ProtoMask)
	binary.LittleEndian.PutUint32(buf[4:8], uint32(len(protoData)))

	if _, err := w.Write(buf[:]); err != nil {
		return err
	}
	_, err = w.Write(protoData)
	return err
}

func (h *MsgHdrProtoBuf) Deserialize(r io.Reader) error {
	var hdrLen uint32
	if err := binary.Read(r, binary.LittleEndian, &hdrLen); err != nil {
		return fmt.Errorf("read proto hdr len: %w", err)
	}

	if hdrLen > MaxHeaderSize {
		return ErrHeaderTooLarge
	}

	hdrBuf := make([]byte, hdrLen)
	if _, err := io.ReadFull(r, hdrBuf); err != nil {
		return fmt.Errorf("read proto hdr body: %w", err)
	}

	h.Proto = new(pb.CMsgProtoBufHeader)
	if err := proto.Unmarshal(hdrBuf, h.Proto); err != nil {
		return fmt.Errorf("unmarshal proto hdr: %w", err)
	}

	return nil
}

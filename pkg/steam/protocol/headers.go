// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package protocol

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"sync"
	"unsafe"

	"google.golang.org/protobuf/encoding/protowire"
	"google.golang.org/protobuf/proto"

	pb "github.com/lemon4ksan/g-man/pkg/protobuf/steam"
	"github.com/lemon4ksan/g-man/pkg/steam/protocol/enums"
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
	// EMsg is the Steam protocol message type identifier.
	EMsg enums.EMsg
	// TargetJobID is the unique job correlation ID of the recipient.
	TargetJobID uint64
	// SourceJobID is the unique job correlation ID of the sender.
	SourceJobID uint64
}

// NewMsgHdr creates a new standard message header with the specified EMsg
// and target job ID. SourceJobID is automatically initialized to [NoJob].
func NewMsgHdr(eMsg enums.EMsg, targetJobID uint64) *MsgHdr {
	return &MsgHdr{
		EMsg:        eMsg,
		TargetJobID: targetJobID,
		SourceJobID: NoJob,
	}
}

// GetSourceJob returns the source JobID.
func (h *MsgHdr) GetSourceJob() uint64 { return h.SourceJobID }

// GetTargetJob returns the target JobID.
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

// Deserialize reads the standard header fields (excluding EMsg) from the reader.
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
	// EMsg is the Steam protocol message type identifier.
	EMsg enums.EMsg
	// HeaderSize is the size of the legacy extended header in bytes (always 36).
	HeaderSize byte
	// HeaderVer is the protocol version for extended headers (always 2).
	HeaderVer uint16
	// TargetJobID is the unique job correlation ID of the recipient.
	TargetJobID uint64
	// SourceJobID is the unique job correlation ID of the sender.
	SourceJobID uint64
	// HeaderCanary is a magic byte used to verify header integrity (always 0xEF).
	HeaderCanary byte
	// SteamID is the 64-bit Steam identifier associated with the active session.
	SteamID uint64
	// SessionID is the 32-bit session ID assigned by the Connection Manager.
	SessionID int32
}

// NewMsgHdrExtended creates an extended header for authorized messages.
// Both Job IDs are initialized to [NoJob].
func NewMsgHdrExtended(eMsg enums.EMsg, steamID uint64, sessionID int32) *MsgHdrExtended {
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

// GetSourceJob returns the source JobID.
func (h *MsgHdrExtended) GetSourceJob() uint64 { return h.SourceJobID }

// GetTargetJob returns the target JobID.
func (h *MsgHdrExtended) GetTargetJob() uint64 { return h.TargetJobID }

// GetSteamID returns the SteamID associated with this header.
func (h *MsgHdrExtended) GetSteamID() uint64 { return h.SteamID }

// GetSessionID returns the SessionID associated with this header.
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
// Note: It assumes the EMsg (first 4 bytes) has already been read.
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

type pooledProtoHeader struct {
	hdr       pb.CMsgProtoBufHeader
	steamID   uint64
	sessionID int32
	jobSource uint64
	jobTarget uint64
	eResult   int32
}

var protoHeaderPool = sync.Pool{
	New: func() any {
		ph := &pooledProtoHeader{}
		ph.hdr.Steamid = &ph.steamID
		ph.hdr.ClientSessionid = &ph.sessionID
		ph.hdr.JobidSource = &ph.jobSource
		ph.hdr.JobidTarget = &ph.jobTarget
		ph.hdr.Eresult = &ph.eResult

		return ph
	},
}

// AcquireProtoHeader acquires a protobuf header from the pool and resets it for use.
func AcquireProtoHeader() *pb.CMsgProtoBufHeader {
	ph := protoHeaderPool.Get().(*pooledProtoHeader)
	ph.steamID = 0
	ph.sessionID = 0
	ph.jobSource = NoJob
	ph.jobTarget = NoJob
	ph.eResult = 0

	ph.hdr.TargetJobName = nil
	ph.hdr.WgToken = nil
	ph.hdr.RoutingAppid = nil

	ph.hdr.ForwardToSysid = ph.hdr.GetForwardToSysid()[:0]

	ph.hdr.ExcludeClientSessionids = ph.hdr.GetExcludeClientSessionids()[:0]
	if ph.hdr.GetRoutingGc() != nil {
		ph.hdr.GetRoutingGc().Reset()
	}

	return &ph.hdr
}

// ReleaseProtoHeader releases a protobuf header back to the pool.
func ReleaseProtoHeader(h *pb.CMsgProtoBufHeader) {
	if h == nil {
		return
	}

	ph := (*pooledProtoHeader)(unsafe.Pointer(h))
	protoHeaderPool.Put(ph)
}

var msgHdrProtoBufPool = sync.Pool{
	New: func() any { return &MsgHdrProtoBuf{} },
}

func acquireMsgHdrProtoBuf(eMsg enums.EMsg) *MsgHdrProtoBuf {
	h := msgHdrProtoBufPool.Get().(*MsgHdrProtoBuf)
	h.EMsg = eMsg

	if h.Proto == nil {
		h.Proto = AcquireProtoHeader()
	}

	return h
}

func releaseMsgHdrProtoBuf(h *MsgHdrProtoBuf) {
	if h == nil {
		return
	}

	if h.Proto != nil {
		ReleaseProtoHeader(h.Proto)
		h.Proto = nil
	}

	msgHdrProtoBufPool.Put(h)
}

// MsgHdrProtoBuf is the modern Steam header format. It wraps
// a Protobuf message containing routing and session metadata.
type MsgHdrProtoBuf struct {
	// EMsg is the Steam protocol message type identifier.
	EMsg enums.EMsg
	// Proto is the Protobuf-encoded routing and session metadata.
	Proto *pb.CMsgProtoBufHeader
}

// NewMsgHdrProtoBuf creates a modern Protobuf-style header.
// It initializes a default CMsgProtoBufHeader with the provided session info
// and sets Job IDs to [NoJob].
func NewMsgHdrProtoBuf(eMsg enums.EMsg, steamID uint64, sessionID int32) *MsgHdrProtoBuf {
	hdr := acquireMsgHdrProtoBuf(eMsg)

	SetProtoUint64(&hdr.Proto.Steamid, steamID)
	SetProtoInt32(&hdr.Proto.ClientSessionid, sessionID)
	SetProtoUint64(&hdr.Proto.JobidSource, NoJob)
	SetProtoUint64(&hdr.Proto.JobidTarget, NoJob)

	return hdr
}

// GetSourceJob returns the source JobID from the Protobuf header.
func (h *MsgHdrProtoBuf) GetSourceJob() uint64 { return h.Proto.GetJobidSource() }

// GetTargetJob returns the target JobID from the Protobuf header.
func (h *MsgHdrProtoBuf) GetTargetJob() uint64 { return h.Proto.GetJobidTarget() }

// GetSteamID returns the SteamID from the Protobuf header.
func (h *MsgHdrProtoBuf) GetSteamID() uint64 { return h.Proto.GetSteamid() }

// GetSessionID returns the SessionID from the Protobuf header.
func (h *MsgHdrProtoBuf) GetSessionID() int32 { return h.Proto.GetClientSessionid() }

// GetEResult returns the result code from the header if present.
func (h *MsgHdrProtoBuf) GetEResult() enums.EResult {
	if h.Proto.Eresult == nil {
		return enums.EResult_OK
	}

	return enums.EResult(h.Proto.GetEresult())
}

// SerializeTo marshals the Protobuf header and writes it to the writer,
// preceded by the EMsg (with ProtoMask set) and the header length.
func (h *MsgHdrProtoBuf) SerializeTo(w io.Writer) error {
	protoData, err := proto.Marshal(h.Proto)
	if err != nil {
		return err
	}

	var buf [8]byte
	binary.LittleEndian.PutUint32(buf[0:4], uint32(h.EMsg)|ProtoMask)
	binary.LittleEndian.PutUint32(buf[4:8], uint32(len(protoData)))

	if _, err := w.Write(buf[:]); err != nil {
		return err
	}

	_, err = w.Write(protoData)

	return err
}

var protoHeaderBufPool = sync.Pool{
	New: func() any {
		b := make([]byte, 512)
		return &b
	},
}

// Deserialize reads the Protobuf header length and body from the reader.
// Uses pooled header buffers to guarantee zero heap escape.
func (h *MsgHdrProtoBuf) Deserialize(r io.Reader) error {
	var lenBuf [4]byte
	if _, err := io.ReadFull(r, lenBuf[:]); err != nil {
		return fmt.Errorf("read proto hdr len: %w", err)
	}

	hdrLen := binary.LittleEndian.Uint32(lenBuf[:])

	if hdrLen > MaxHeaderSize {
		return ErrHeaderTooLarge
	}

	var hdrBuf []byte
	if hdrLen <= 512 {
		bufPtr := protoHeaderBufPool.Get().(*[]byte)

		hdrBuf = (*bufPtr)[:hdrLen]
		defer protoHeaderBufPool.Put(bufPtr)
	} else {
		hdrBuf = make([]byte, hdrLen)
	}

	if _, err := io.ReadFull(r, hdrBuf); err != nil {
		return fmt.Errorf("read proto hdr body: %w", err)
	}

	if h.Proto == nil {
		h.Proto = AcquireProtoHeader()
	} else {
		h.Proto.Reset()
	}

	err := UnmarshalProto(hdrBuf, h.Proto)
	if err != nil {
		return fmt.Errorf("unmarshal proto hdr: %w", err)
	}

	return nil
}

// FastUnmarshal unmarshals the given data into the MsgHdrProtoBuf struct without allocations.
func (h *MsgHdrProtoBuf) FastUnmarshal(data []byte) error {
	if h == nil {
		return errors.New("nil MsgHdrProtoBuf")
	}

	if h.Proto == nil {
		h.Proto = AcquireProtoHeader()
	}

	offset := 0
	for offset < len(data) {
		num, typ, n := protowire.ConsumeTag(data[offset:])
		if n < 0 {
			return errors.New("unmarshal proto hdr: invalid wire format tag")
		}

		offset += n

		switch num {
		case 1: // steamid (fixed64)
			if typ == protowire.Fixed64Type {
				v, n := protowire.ConsumeFixed64(data[offset:])
				if n > 0 {
					SetProtoUint64(&h.Proto.Steamid, v)

					offset += n
					continue
				}
			}

		case 2: // client_sessionid (varint)
			if typ == protowire.VarintType {
				v, n := protowire.ConsumeVarint(data[offset:])
				if n > 0 {
					SetProtoInt32(&h.Proto.ClientSessionid, int32(v))

					offset += n
					continue
				}
			}

		case 5: // target_job_name (string)
			if typ == protowire.BytesType {
				v, n := protowire.ConsumeBytes(data[offset:])
				if n > 0 {
					s := string(v)
					h.Proto.TargetJobName = &s
					offset += n

					continue
				}
			}

		case 6: // wg_token (string)
			if typ == protowire.BytesType {
				v, n := protowire.ConsumeBytes(data[offset:])
				if n > 0 {
					s := string(v)
					h.Proto.WgToken = &s
					offset += n

					continue
				}
			}

		case 10: // jobid_source (fixed64)
			if typ == protowire.Fixed64Type {
				v, n := protowire.ConsumeFixed64(data[offset:])
				if n > 0 {
					SetProtoUint64(&h.Proto.JobidSource, v)

					offset += n
					continue
				}
			}

		case 11: // jobid_target (fixed64)
			if typ == protowire.Fixed64Type {
				v, n := protowire.ConsumeFixed64(data[offset:])
				if n > 0 {
					SetProtoUint64(&h.Proto.JobidTarget, v)

					offset += n
					continue
				}
			}

		case 13: // eresult (varint)
			if typ == protowire.VarintType {
				v, n := protowire.ConsumeVarint(data[offset:])
				if n > 0 {
					SetProtoInt32(&h.Proto.Eresult, int32(v))

					offset += n
					continue
				}
			}

		case 18: // routing_appid (varint)
			if typ == protowire.VarintType {
				v, n := protowire.ConsumeVarint(data[offset:])
				if n > 0 {
					u := uint32(v)
					h.Proto.RoutingAppid = &u
					offset += n

					continue
				}
			}
		}

		n = protowire.ConsumeFieldValue(num, typ, data[offset:])
		if n < 0 {
			return errors.New("unmarshal proto hdr: invalid field value")
		}

		offset += n
	}

	return nil
}

// SetProtoUint64 sets the value of a *uint64 pointer, allocating if necessary.
func SetProtoUint64(p **uint64, val uint64) {
	if *p == nil {
		v := val
		*p = &v
	} else {
		**p = val
	}
}

// SetProtoInt32 sets the value of a *int32 pointer, allocating if necessary.
func SetProtoInt32(p **int32, val int32) {
	if *p == nil {
		v := val
		*p = &v
	} else {
		**p = val
	}
}

// Reset resets the MsgHdrProtoBuf struct fields.
func (h *MsgHdrProtoBuf) Reset() {
	if h.Proto != nil {
		ReleaseProtoHeader(h.Proto)
		h.Proto = nil
	}

	h.EMsg = 0
}

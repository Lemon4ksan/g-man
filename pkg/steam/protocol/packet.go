// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package protocol

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"

	pb "github.com/lemon4ksan/g-man/pkg/protobuf/steam"
	"google.golang.org/protobuf/proto"
)

var (
	ErrHeaderTooLarge  = errors.New("proto header too large")
	ErrPayloadTooLarge = errors.New("payload exceeds maximum size")
	ErrInvalidHeader   = errors.New("invalid header format")
)

// Header describes the common interface for all Steam message headers.
// It provides methods for accessing job IDs used for request-response tracking.
type Header interface {
	GetSourceJob() uint64
	GetTargetJob() uint64
	SerializeTo(w io.Writer) error
}

// AuthorizedHeader describes a header that contains steamID and SessionID.
type AuthorizedHeader interface {
	Header
	GetSteamID() uint64
	GetSessionID() int32
}

// EHeader describes a header that has a [EResult].
type EHeader interface {
	Header
	GetEResult() EResult
}

// Packet represents a parsed message received from or sent to a Steam Connection Manager.
// It serves as a unified interface regardless of the underlying header format (Standard, Extended, or Protobuf).
type Packet struct {
	// EMsg identifies the type of message this packet contains
	EMsg EMsg
	// IsProto is true if the packet uses a Protobuf-style header.
	IsProto bool
	// Header contains metadata about the sender, session and job tracking
	Header Header
	// Payload is the raw message body, which can be further
	// unmarshaled into a specific Protobuf struct or VDF map.
	Payload []byte
}

// ParsePacket decodes a steam network message from an [io.Reader].
//
// It automatically detects the header format by examining EMsg bitmask.
// Specifically, it handles:
//   - Protobuf headers (if the ProtoMask bit is set)
//   - Basic headers (for low-level encryption handshakes)
//   - Extended headers (for most legacy-style Steam messages)
//
// An error is returned if the header length is suspiciously large (>1MB)
// or if the binary data is malformed.
func ParsePacket(r io.Reader) (*Packet, error) {
	var rawEMsg uint32
	if err := binary.Read(r, binary.LittleEndian, &rawEMsg); err != nil {
		return nil, fmt.Errorf("read emsg: %w", err)
	}

	eMsg := EMsg(rawEMsg & EMsgMask)
	isProto := (rawEMsg & ProtoMask) != 0

	var header interface {
		Header
		Deserialize(r io.Reader) error
	}

	switch {
	case isProto:
		header = &MsgHdrProtoBuf{EMsg: eMsg}
	case eMsg == EMsg_ChannelEncryptRequest ||
		eMsg == EMsg_ChannelEncryptResponse ||
		eMsg == EMsg_ChannelEncryptResult:
		header = &MsgHdr{EMsg: eMsg}
	default:
		header = &MsgHdrExtended{EMsg: eMsg}
	}

	if err := header.Deserialize(r); err != nil {
		return nil, fmt.Errorf("deserialize header: %w", err)
	}

	payload, err := io.ReadAll(r) // Potential OOM, but steam shouldn't send large packages
	if err != nil {
		return nil, err
	}
	if len(payload) > MaxPayloadSize {
		return nil, ErrPayloadTooLarge
	}

	return &Packet{
		EMsg:    eMsg,
		IsProto: isProto,
		Header:  header,
		Payload: payload,
	}, nil
}

// GetTargetJobID returns the JobID of the intended recipient.
// Returns [NoJob] if the header does not support job tracking
// or is not present.
func (p *Packet) GetTargetJobID() uint64 {
	if p.Header != nil {
		return p.Header.GetTargetJob()
	}
	return NoJob
}

// GetSourceJobID returns the JobID assigned by the sender to track this request.
// This is used to map responses back to their original requests.
func (p *Packet) GetSourceJobID() uint64 {
	if p.Header != nil {
		return p.Header.GetSourceJob()
	}
	return NoJob
}

// GetSteamID returns the steamID of the header.
// Returns 0 if header doesn't implement [AuthorizedHeader].
func (p *Packet) GetSteamID() uint64 {
	if ah, ok := p.Header.(AuthorizedHeader); ok {
		return ah.GetSteamID()
	}
	return 0
}

// GetSessionID returns the sessionID of the header.
// Returns 0 if header doesn't implement [AuthorizedHeader].
func (p *Packet) GetSessionID() int32 {
	if ah, ok := p.Header.(AuthorizedHeader); ok {
		return ah.GetSessionID()
	}
	return 0
}

// GetEResult returns the header result code.
// Returns [EResult_Invalid] if header doesn't implement [EHeader].
func (p *Packet) GetEResult() EResult {
	if eh, ok := p.Header.(EHeader); ok {
		return eh.GetEResult()
	}
	return EResult_Invalid
}

// SerializeTo encodes the packet to [io.Writer] for sending.
// Returns error if packet marked as proto but header is not [MsgHdrProtoBuf].
func (p *Packet) SerializeTo(w io.Writer) error {
	if p.IsProto {
		if _, ok := p.Header.(*MsgHdrProtoBuf); !ok {
			return fmt.Errorf("%w: packet marked as proto but header is not MsgHdrProtoBuf", ErrInvalidHeader)
		}
	}

	if err := p.Header.SerializeTo(w); err != nil {
		return err
	}

	_, err := w.Write(p.Payload)
	return err
}

// GCPacket represents a Game Coordinator message.
type GCPacket struct {
	AppID       uint32
	MsgType     uint32
	IsProto     bool
	TargetJobID uint64
	SourceJobID uint64
	Payload     []byte
}

// NewGCPacket creates a new GC packet with the given parameters.
func NewGCPacket(appID, msgType uint32, payload []byte) *GCPacket {
	return &GCPacket{
		AppID:   appID,
		MsgType: msgType,
		Payload: payload,
	}
}

// Serialize encodes the packet into the wire format expected by the Steam GC.
// It automatically handles the protobuf header wrapping if IsProto is true.
func (p *GCPacket) Serialize() ([]byte, error) {
	buf := new(bytes.Buffer)

	if p.IsProto {
		// Protobuf Header: [MsgType | Mask] [HeaderLength] [ProtoHeader] [Body]
		msgType := p.MsgType | ProtoMask
		binary.Write(buf, binary.LittleEndian, msgType)

		hdr := &pb.CMsgProtoBufHeader{
			JobidSource: proto.Uint64(p.SourceJobID),
			JobidTarget: proto.Uint64(p.TargetJobID),
		}
		hdrBytes, err := proto.Marshal(hdr)
		if err != nil {
			return nil, fmt.Errorf("gc: marshal proto header: %w", err)
		}

		binary.Write(buf, binary.LittleEndian, uint32(len(hdrBytes)))
		buf.Write(hdrBytes)

	} else {
		// [HeaderVersion(1)] [TargetJobID] [SourceJobID] [Body]
		// Note: Legacy GC header structure varies by game, but usually standard 18 bytes
		header := make([]byte, 18)
		binary.LittleEndian.PutUint16(header[0:], 1) // Header Version
		binary.LittleEndian.PutUint64(header[2:], p.TargetJobID)
		binary.LittleEndian.PutUint64(header[10:], p.SourceJobID)
		buf.Write(header)
	}

	buf.Write(p.Payload)
	return buf.Bytes(), nil
}

// ParseGCPacket decodes a raw byte slice from ClientFromGC into a Packet.
func ParseGCPacket(appID uint32, msgType uint32, data []byte) (*GCPacket, error) {
	p := &GCPacket{
		AppID:   appID,
		MsgType: msgType & ^uint32(ProtoMask), // Strip mask
		IsProto: (msgType & ProtoMask) > 0,
	}

	r := bytes.NewReader(data)

	if p.IsProto {
		// Read Header Length
		var hdrLen uint32
		if err := binary.Read(r, binary.LittleEndian, &hdrLen); err != nil {
			return nil, fmt.Errorf("gc: read proto header len: %w", err)
		}

		// Read Proto Header
		hdrBytes := make([]byte, hdrLen)
		if _, err := io.ReadFull(r, hdrBytes); err != nil {
			return nil, fmt.Errorf("gc: read proto header: %w", err)
		}

		hdr := &pb.CMsgProtoBufHeader{}
		if err := proto.Unmarshal(hdrBytes, hdr); err != nil {
			return nil, fmt.Errorf("gc: unmarshal proto header: %w", err)
		}

		p.TargetJobID = hdr.GetJobidTarget()
		p.SourceJobID = hdr.GetJobidSource()

	} else {
		// Legacy Header (18 bytes)
		header := make([]byte, 18)
		if _, err := io.ReadFull(r, header); err != nil {
			return nil, fmt.Errorf("gc: read legacy header: %w", err)
		}

		// Skip version (2 bytes)
		p.TargetJobID = binary.LittleEndian.Uint64(header[2:])
		p.SourceJobID = binary.LittleEndian.Uint64(header[10:])
	}

	// The rest is payload
	var err error
	p.Payload, err = io.ReadAll(r)
	if err != nil {
		return nil, fmt.Errorf("gc: read payload: %w", err)
	}

	return p, nil
}

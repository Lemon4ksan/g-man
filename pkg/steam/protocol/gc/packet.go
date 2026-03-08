// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package gc

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"

	pb "github.com/lemon4ksan/g-man/pkg/steam/protocol/protobuf"
	"google.golang.org/protobuf/proto"
)

const (
	// ProtoMask is the bitmask for protobuf messages.
	ProtoMask = 0x80000000
)

// Packet represents a Game Coordinator message.
type Packet struct {
	AppID       uint32
	MsgType     uint32
	IsProto     bool
	TargetJobID uint64
	SourceJobID uint64
	Payload     []byte
}

// NewPacket creates a new GC packet with the given parameters.
func NewPacket(appID, msgType uint32, payload []byte) *Packet {
	return &Packet{
		AppID:   appID,
		MsgType: msgType,
		Payload: payload,
	}
}

// Serialize encodes the packet into the wire format expected by the Steam GC.
// It automatically handles the protobuf header wrapping if IsProto is true.
func (p *Packet) Serialize() ([]byte, error) {
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

// ParsePacket decodes a raw byte slice from ClientFromGC into a Packet.
func ParsePacket(appID uint32, msgType uint32, data []byte) (*Packet, error) {
	p := &Packet{
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

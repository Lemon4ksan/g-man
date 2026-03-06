// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package protocol

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"

	pb "github.com/lemon4ksan/g-man/steam/protocol/protobuf"
	"google.golang.org/protobuf/proto"
)

type Packet struct {
	EMsg    EMsg
	IsProto bool
	Header  Header
	Payload []byte
}

func ParsePacket(data []byte) (*Packet, error) {
	if len(data) < 4 {
		return nil, fmt.Errorf("packet too short")
	}

	rawEMsg := binary.LittleEndian.Uint32(data[:4])
	eMsg := EMsg(rawEMsg & EMsgMask)
	isProto := (rawEMsg & ProtoMask) != 0

	pkt := &Packet{
		EMsg:    eMsg,
		IsProto: isProto,
	}

	reader := bytes.NewReader(data[4:])

	switch {
	case isProto:
		var hdrLen uint32
		if err := binary.Read(reader, binary.LittleEndian, &hdrLen); err != nil {
			return nil, err
		}

		hdrBuf := make([]byte, hdrLen)
		if _, err := io.ReadFull(reader, hdrBuf); err != nil {
			return nil, err
		}

		protoHdr := new(pb.CMsgProtoBufHeader)
		if err := proto.Unmarshal(hdrBuf, protoHdr); err != nil {
			return nil, err
		}
		pkt.Header = &MsgHdrProtoBuf{EMsg: eMsg, Proto: protoHdr}

	case eMsg == EMsg_ChannelEncryptRequest || eMsg == EMsg_ChannelEncryptResult:
		hdr := &MsgHdr{EMsg: eMsg}
		if err := binary.Read(reader, binary.LittleEndian, &hdr.TargetJobID); err != nil {
			return nil, err
		}
		if err := binary.Read(reader, binary.LittleEndian, &hdr.SourceJobID); err != nil {
			return nil, err
		}
		pkt.Header = hdr

	default:
		h := &MsgHdrExtended{EMsg: eMsg}
		if err := h.deserialize(reader); err != nil {
			return nil, err
		}
		pkt.Header = h
	}

	pkt.Payload, _ = io.ReadAll(reader)
	return pkt, nil
}

func (p *Packet) GetTargetJobID() uint64 {
	if p.Header != nil {
		return p.Header.GetTargetJob()
	}
	return NoJob
}

func (p *Packet) GetSourceJobID() uint64 {
	if p.Header != nil {
		return p.Header.GetSourceJob()
	}
	return NoJob
}

func (h *MsgHdrExtended) deserialize(r io.Reader) error {
	data := make([]byte, 32)
	if _, err := io.ReadFull(r, data); err != nil {
		return err
	}
	h.HeaderSize = data[0]
	h.HeaderVer = binary.LittleEndian.Uint16(data[1:3])
	h.TargetJobID = binary.LittleEndian.Uint64(data[3:11])
	h.SourceJobID = binary.LittleEndian.Uint64(data[11:19])
	h.HeaderCanary = data[19]
	h.SteamID = binary.LittleEndian.Uint64(data[20:28])
	h.SessionID = int32(binary.LittleEndian.Uint32(data[28:32]))
	return nil
}

// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package socket

import (
	"bytes"
	"fmt"

	"github.com/lemon4ksan/g-man/pkg/steam/protocol"
	"google.golang.org/protobuf/proto"
)

// Encoder defines the contract for serializing Steam messages before transmitting
// them over the network. It handles the creation of correct Steam message headers
// (Basic, Extended, or Protobuf) and appends the serialized payload.
type Encoder interface {
	// EncodeProto serializes a Protobuf message and wraps it in a MsgHdrProtoBuf.
	EncodeProto(w *bytes.Buffer, eMsg protocol.EMsg, steamID uint64, sessionID int32, sourceJob, targetJob uint64, body proto.Message) error

	// EncodeUnified serializes a Unified Service Method call (Protobuf) with routing.
	EncodeUnified(w *bytes.Buffer, steamID uint64, sessionID int32, methodName string, sourceJob uint64, body proto.Message) error

	// EncodeLegacy wraps a raw byte payload in a MsgHdrExtended (Legacy Steam Header).
	EncodeLegacy(w *bytes.Buffer, eMsg protocol.EMsg, steamID uint64, sessionID int32, sourceJob, targetJob uint64, body []byte) error

	// EncodeProtoRaw wraps an already serialized Protobuf payload in a MsgHdrProtoBuf.
	EncodeProtoRaw(w *bytes.Buffer, eMsg protocol.EMsg, steamID uint64, sessionID int32, sourceJob, targetJob uint64, body []byte) error

	// EncodeUnifiedRaw wraps an already serialized Service payload in a MsgHdrProtoBuf.
	EncodeUnifiedRaw(w *bytes.Buffer, steamID uint64, sessionID int32, methodName string, sourceJob uint64, body []byte) error

	// EncodeRaw wraps a plain byte payload in a basic MsgHdr (No SteamID/SessionID).
	EncodeRaw(w *bytes.Buffer, eMsg protocol.EMsg, targetJob, sourceJob uint64, body []byte) error
}

// BaseEncoder is the default implementation of the Encoder interface.
// It complies with standard Steam CM protocol rules.
var _ Encoder = (*BaseEncoder)(nil)

type BaseEncoder struct{}

func (e *BaseEncoder) EncodeProto(w *bytes.Buffer, eMsg protocol.EMsg, steamID uint64, sessionID int32, sourceJob, targetJob uint64, body proto.Message) error {
	// Protocol Quirk: Steam drops the connection if a SessionID or SteamID
	// is provided during the initial ClientHello handshake. We enforce 0 here.
	if eMsg == protocol.EMsg_ClientHello {
		steamID = 0
		sessionID = 0
	}

	hdr := protocol.NewMsgHdrProtoBuf(eMsg, steamID, sessionID)
	hdr.Proto.JobidSource = proto.Uint64(sourceJob)
	hdr.Proto.JobidTarget = proto.Uint64(targetJob)

	if err := hdr.SerializeTo(w); err != nil {
		return fmt.Errorf("encode proto header: %w", err)
	}

	payload, err := proto.Marshal(body)
	if err != nil {
		return fmt.Errorf("encode proto body: %w", err)
	}

	w.Write(payload)
	return nil
}

func (e *BaseEncoder) EncodeUnified(w *bytes.Buffer, steamID uint64, sessionID int32, methodName string, sourceJob uint64, body proto.Message) error {
	hdr := protocol.NewMsgHdrProtoBuf(protocol.EMsg_ServiceMethodCallFromClient, steamID, sessionID)
	hdr.Proto.JobidSource = proto.Uint64(sourceJob)
	hdr.Proto.TargetJobName = proto.String(methodName)

	if err := hdr.SerializeTo(w); err != nil {
		return fmt.Errorf("encode unified header: %w", err)
	}

	payload, err := proto.Marshal(body)
	if err != nil {
		return fmt.Errorf("encode unified body: %w", err)
	}

	w.Write(payload)
	return nil
}

func (e *BaseEncoder) EncodeLegacy(w *bytes.Buffer, eMsg protocol.EMsg, steamID uint64, sessionID int32, sourceJob, targetJob uint64, body []byte) error {
	hdr := protocol.NewMsgHdrExtended(eMsg, steamID, sessionID)
	hdr.SourceJobID = sourceJob
	hdr.TargetJobID = targetJob

	if err := hdr.SerializeTo(w); err != nil {
		return fmt.Errorf("encode legacy header: %w", err)
	}

	w.Write(body)
	return nil
}

func (e *BaseEncoder) EncodeProtoRaw(w *bytes.Buffer, eMsg protocol.EMsg, steamID uint64, sessionID int32, sourceJob, targetJob uint64, body []byte) error {
	hdr := protocol.NewMsgHdrProtoBuf(eMsg, steamID, sessionID)
	hdr.Proto.JobidSource = proto.Uint64(sourceJob)
	hdr.Proto.JobidTarget = proto.Uint64(targetJob)

	if err := hdr.SerializeTo(w); err != nil {
		return fmt.Errorf("encode proto raw header: %w", err)
	}

	w.Write(body)
	return nil
}

func (e *BaseEncoder) EncodeUnifiedRaw(w *bytes.Buffer, steamID uint64, sessionID int32, targetName string, sourceJob uint64, body []byte) error {
	hdr := protocol.NewMsgHdrProtoBuf(protocol.EMsg_ServiceMethodCallFromClient, steamID, sessionID)
	hdr.Proto.JobidSource = proto.Uint64(sourceJob)
	hdr.Proto.TargetJobName = proto.String(targetName)

	if err := hdr.SerializeTo(w); err != nil {
		return fmt.Errorf("encode unified raw header: %w", err)
	}

	w.Write(body)
	return nil
}

// EncodeRaw handles basic non-protobuf, non-extended messages
// (e.g., ChannelEncryptResponse).
func (e *BaseEncoder) EncodeRaw(w *bytes.Buffer, eMsg protocol.EMsg, targetJob, sourceJob uint64, body []byte) error {
	hdr := protocol.NewMsgHdr(eMsg, targetJob)
	hdr.SourceJobID = sourceJob

	if err := hdr.SerializeTo(w); err != nil {
		return fmt.Errorf("encode basic raw header: %w", err)
	}

	w.Write(body)
	return nil
}

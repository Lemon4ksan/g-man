// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package socket

import (
	"bytes"
	"fmt"

	"github.com/lemon4ksan/foundation/async/task"
	"google.golang.org/protobuf/proto"

	"github.com/lemon4ksan/g-man/pkg/steam/protocol"
	"github.com/lemon4ksan/g-man/pkg/steam/protocol/enums"
)

// Handler processes a fully parsed Steam packet.
type Handler func(p *protocol.Packet)

// MsgHandler defines the signature for raw EMsg packet handlers.
type MsgHandler = Handler

// ServiceHandler defines the signature for unified service method packet handlers.
type ServiceHandler = Handler

// SendConfig specifies parameters for sending outbound socket messages.
type SendConfig struct {
	Callback task.Callback[*protocol.Packet]
	Token    string
}

// SendOption configures a Send operation.
type SendOption func(*SendConfig)

// WithCallback assigns an asynchronous job response callback.
func WithCallback(cb task.Callback[*protocol.Packet]) SendOption {
	return func(c *SendConfig) { c.Callback = cb }
}

// WithToken configures an access token for service call routing.
func WithToken(token string) SendOption {
	return func(c *SendConfig) { c.Token = token }
}

// SessionReader reads SteamID and SessionID values.
type SessionReader interface {
	SteamID() uint64
	SessionID() int32
}

// PayloadBuilder serializes a binary packet into a destination buffer.
type PayloadBuilder func(sess SessionReader, buf *bytes.Buffer, sourceJobID uint64, token string) error

// Proto builds a standard Protobuf-wrapped packet.
func Proto(eMsg enums.EMsg, req proto.Message) PayloadBuilder {
	return func(sess SessionReader, buf *bytes.Buffer, sourceJobID uint64, _ string) (err error) {
		var (
			steamID   uint64
			sessionID int32
		)

		if sess != nil {
			steamID = sess.SteamID()
			sessionID = sess.SessionID()
		}

		hdr := protocol.NewMsgHdrProtoBuf(eMsg, steamID, sessionID)
		hdr.Proto.JobidSource = new(sourceJobID)

		if err := hdr.SerializeTo(buf); err != nil {
			return fmt.Errorf("serialize proto header: %w", err)
		}

		if req != nil {
			payload, err := proto.Marshal(req)
			if err != nil {
				return fmt.Errorf("marshal proto: %w", err)
			}

			buf.Write(payload)
		}

		return nil
	}
}

// Unified builds a Protobuf packet for Unified Service methods.
func Unified(method string, req proto.Message) PayloadBuilder {
	return func(sess SessionReader, buf *bytes.Buffer, sourceJobID uint64, _ string) (err error) {
		var (
			steamID   uint64
			sessionID int32
		)

		if sess != nil {
			steamID = sess.SteamID()
			sessionID = sess.SessionID()
		}

		hdr := protocol.NewMsgHdrProtoBuf(enums.EMsg_ServiceMethodCallFromClient, steamID, sessionID)
		hdr.Proto.JobidSource = new(sourceJobID)
		hdr.Proto.TargetJobName = new(method)

		if err := hdr.SerializeTo(buf); err != nil {
			return fmt.Errorf("serialize unified proto header: %w", err)
		}

		if req != nil {
			payload, err := proto.Marshal(req)
			if err != nil {
				return fmt.Errorf("marshal unified proto: %w", err)
			}

			buf.Write(payload)
		}

		return nil
	}
}

// Raw builds a packet with Standard or Extended headers depending on the EMsg opcode.
func Raw(eMsg enums.EMsg, payload []byte) PayloadBuilder {
	return func(sess SessionReader, buf *bytes.Buffer, sourceJobID uint64, _ string) error {
		if eMsg == enums.EMsg_ChannelEncryptRequest ||
			eMsg == enums.EMsg_ChannelEncryptResponse ||
			eMsg == enums.EMsg_ChannelEncryptResult {
			hdr := protocol.NewMsgHdr(eMsg, protocol.NoJob)
			hdr.SourceJobID = sourceJobID

			if err := hdr.SerializeTo(buf); err != nil {
				return fmt.Errorf("serialize standard header: %w", err)
			}

			buf.Write(payload)

			return nil
		}

		var (
			steamID   uint64
			sessionID int32
		)

		if sess != nil {
			steamID = sess.SteamID()
			sessionID = sess.SessionID()
		}

		hdr := protocol.NewMsgHdrExtended(eMsg, steamID, sessionID)
		hdr.SourceJobID = sourceJobID

		if err := hdr.SerializeTo(buf); err != nil {
			return fmt.Errorf("serialize extended header: %w", err)
		}

		buf.Write(payload)

		return nil
	}
}

// DynamicRaw builds a packet selecting Protobuf or Extended headers based on targetName presence.
func DynamicRaw(eMsg enums.EMsg, targetName string, payload []byte, routingAppID uint32) PayloadBuilder {
	return func(sess SessionReader, buf *bytes.Buffer, sourceJobID uint64, _ string) error {
		var (
			steamID   uint64
			sessionID int32
		)

		if sess != nil {
			steamID = sess.SteamID()
			sessionID = sess.SessionID()
		}

		if targetName != "" {
			hdr := protocol.NewMsgHdrProtoBuf(eMsg, steamID, sessionID)
			hdr.Proto.JobidSource = new(sourceJobID)
			hdr.Proto.TargetJobName = new(targetName)

			if routingAppID != 0 {
				hdr.Proto.RoutingAppid = new(routingAppID)
			}

			if err := hdr.SerializeTo(buf); err != nil {
				return fmt.Errorf("serialize dynamic proto header: %w", err)
			}
		} else {
			hdr := protocol.NewMsgHdrExtended(eMsg, steamID, sessionID)

			hdr.SourceJobID = sourceJobID
			if err := hdr.SerializeTo(buf); err != nil {
				return fmt.Errorf("serialize dynamic extended header: %w", err)
			}
		}

		buf.Write(payload)

		return nil
	}
}

// DynamicRawProto builds a packet using Protobuf headers for non-unified EMsg messages.
func DynamicRawProto(eMsg enums.EMsg, payload []byte, routingAppID uint32) PayloadBuilder {
	return func(sess SessionReader, buf *bytes.Buffer, sourceJobID uint64, _ string) error {
		var (
			steamID   uint64
			sessionID int32
		)

		if sess != nil {
			steamID = sess.SteamID()
			sessionID = sess.SessionID()
		}

		hdr := protocol.NewMsgHdrProtoBuf(eMsg, steamID, sessionID)
		hdr.Proto.JobidSource = new(sourceJobID)

		if routingAppID != 0 {
			hdr.Proto.RoutingAppid = new(routingAppID)
		}

		if err := hdr.SerializeTo(buf); err != nil {
			return fmt.Errorf("serialize proto header: %w", err)
		}

		buf.Write(payload)

		return nil
	}
}

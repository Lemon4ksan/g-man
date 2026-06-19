// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package protocol implements the low-level binary wire format used by Steam Connection Managers (CM).
//
// At its core, every communication with Steam is a "Packet". This package provides the
// primitives to encode, decode, and route these packets based on their headers.
//
// # Key Components
//
//   - [Packet]: Represents a parsed message consisting of an EMsg, a Header, and a raw binary payload.
//   - [Header]: The common interface representing various Steam message headers.
//   - [MsgHdr]: A basic, non-authorized standard header used during handshakes.
//   - [MsgHdrExtended]: A legacy authorized header format containing SteamID and SessionID.
//   - [MsgHdrProtoBuf]: The modern, Protobuf-based header format wrapping routing metadata.
//   - [GCPacket]: Represents a message sent to or received from a Game Coordinator (GC).
//
// # Basic Usage Example
//
//	package main
//
//	import (
//		"bytes"
//		"fmt"
//		"github.com/lemon4ksan/g-man/pkg/steam/protocol"
//		"github.com/lemon4ksan/g-man/pkg/steam/protocol/enums"
//	)
//
//	func main() {
//		// Create a basic packet with standard header
//		hdr := protocol.NewMsgHdr(enums.EMsg_ChannelEncryptRequest, 0)
//		pkt := &protocol.Packet{
//			EMsg:    enums.EMsg_ChannelEncryptRequest,
//			IsProto: false,
//			Header:  hdr,
//			Payload: []byte{0x01, 0x02, 0x03, 0x04},
//		}
//
//		// Serialize the packet to a buffer
//		var buf bytes.Buffer
//		if err := pkt.SerializeTo(&buf); err != nil {
//			fmt.Println("Serialization failed:", err)
//			return
//		}
//
//		// Parse the packet back from the buffer
//		parsed, err := protocol.ParsePacket(&buf)
//		if err != nil {
//			fmt.Println("Parsing failed:", err)
//			return
//		}
//
//		fmt.Println("Parsed EMsg:", parsed.EMsg)
//		fmt.Println("Payload length:", len(parsed.Payload))
//	}
package protocol

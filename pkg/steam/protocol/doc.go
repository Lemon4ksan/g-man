// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package protocol implements the lower-level Steam Connection Manager (CM) binary wire protocol,
// length-prefixed packet framing, and multi-packet compression routines.
//
// # 1. TCP Packet Framing (VT01 Wire Format)
//
// Messages sent over raw TCP sockets are framed using an 8-byte header:
//
//	+-------------------------------+-------------------------------+-------------------------------+
//	|     Payload Length (uint32)   |     Magic Identifier "VT01"   |        Message Payload        |
//	|            4 bytes            |            4 bytes            |            N bytes            |
//	+-------------------------------+-------------------------------+-------------------------------+
//
// Magic Constant: 0x31305456 ("VT01" in Little Endian).
//
// # 2. Header Kinds & Bitmasks
//
// Steam utilizes three distinct header layouts preceding the message payload:
//
//  1. Standard Header (MsgHdr, 20 bytes):
//     Used during unencrypted handshake stages (e.g. EMsg_ChannelEncryptRequest).
//     Fields: EMsg (uint32), TargetJobID (uint64), SourceJobID (uint64).
//
//  2. Extended Header (MsgHdrExtended, 36 bytes):
//     Legacy header for non-protobuf authorized messages.
//     Fields: EMsg, HeaderSize (36), HeaderVersion (2), TargetJobID, SourceJobID, HeaderCanary (0xEF), SteamID, SessionID.
//
//  3. Protobuf Header (MsgHdrProtoBuf):
//     Modern header wrapping CMsgProtoBufHeader. Marked by setting the top bit of the EMsg:
//
//     IsProtobuf = (rawEMsg & 0x80000000) != 0
//     ActualEMsg = rawEMsg & 0x7FFFFFFF
//
// # 3. Multi-Packet Batching (EMsg_Multi)
//
// Large bursts or batched responses from Steam are wrapped inside an EMsg_Multi message containing a CMsgMulti payload.
// If SizeUnzipped > 0, the body is Gzip-compressed.
//
// Security Guarantee:
// To prevent Zip-Bomb OOM vulnerabilities, decompression is guarded by a hard threshold (DecompressionLimit = 100MB).
package protocol

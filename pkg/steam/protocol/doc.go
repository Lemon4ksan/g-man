// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

/*
Package protocol implements the low-level binary wire format used by Steam Connection Managers (CM).

At its core, every communication with Steam is a "Packet". This package provides the
primitives to encode, decode, and route these packets based on their headers.

# Packet Structure

A Steam message consists of three parts:
 1. EMsg (4 bytes): Identifies the message type and indicates if it's a Protobuf message via ProtoMask.
 2. Header: Metadata containing session info and routing data.
 3. Payload: The actual body of the message (Protobuf, VDF, or raw bytes).

# Header Evolution

Steam uses three distinct header formats, which this package unifies under the Header interface:

  - MsgHdr (Standard): Used for low-level handshakes (e.g., encryption setup).
    Contains only Job IDs.
  - MsgHdrExtended: Used for legacy messages. Adds SteamID and SessionID fields
    directly into the binary stream.
  - MsgHdrProtoBuf: The modern format. Encapsulates a Protobuf-encoded header
    (CMsgProtoBufHeader), allowing for flexible routing and detailed error reporting.

# Job Tracking (Asynchronous RPC)

Steam's protocol is inherently asynchronous. Unlike standard HTTP, responses
are matched to requests using Job IDs:

  - SourceJobID: A unique ID assigned by the sender of a request.
  - TargetJobID: When responding, Steam puts the sender's SourceID into this field.

This mechanism allows the high-level 'socket' package to track multiple
concurrent requests over a single TCP/WebSocket connection.

# Bitmasking

The package handles the ProtoMask automatically.
When parsing, the mask is stripped to reveal the underlying EMsg, and when
serializing MsgHdrProtoBuf, the mask is restored to inform the CM that
Protobuf decoding is required.

# Enums

The package also contains a complete list of possible EMessages and flags that may be encountered.
They were automatically generated from legacy steamd files that can be found at https://github.com/SteamRE/SteamKit/tree/master/Resources/SteamLanguage.
*/
package protocol

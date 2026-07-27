// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package gc provides a multiplexing gateway for communicating with game-specific Game Coordinator (GC) servers.
//
// # 1. Game Coordinator (GC) Architecture
//
// While Steam Connection Managers handle core platform authentication and social features, specific Valve titles
// (such as Team Fortress 2, Counter-Strike 2, and Dota 2) run dedicated secondary backend servers called Game Coordinators.
//
// Target AppIDs:
//   - AppID 440: Team Fortress 2 (TF2 GC)
//   - AppID 730: Counter-Strike 2 (CS2 GC)
//   - AppID 570: Dota 2 (Dota GC)
//
// # 2. Envelope Tunneling Mechanics
//
// GC messages are not sent via direct TCP sockets. Instead, they are tunneled through the active Steam CM connection
// using wrapper Protobuf envelopes:
//   - Outbound: Wrapped inside CMsgGCClient and transmitted via EMsg_ClientToGC with RoutingAppID set.
//   - Inbound: Unwrapped from EMsg_ClientFromGC packets.
//
// # 3. Asynchronous JobID Matching
//
// Requests sent via Call or CallRaw generate an asynchronous tracking JobID. When the Game Coordinator responds,
// the response packet contains a matching TargetJobID, allowing the Coordinator to resolve the pending callback
// without blocking worker threads.
package gc

// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package id parses, validates, and transforms 64-bit Steam identifiers (SteamID)
// across legacy Steam2, modern Steam3, and custom vanity URL formats.
//
// # 1. Bit Structure of a 64-bit SteamID
//
// A SteamID is a 64-bit unsigned integer structured into four distinct bitfield segments:
//
//	 63        56 55    52 51                    32 31                             0
//	+------------+--------+------------------------+-------------------------------+
//	|  Universe  |  Type  |        Instance        |          Account ID           |
//	|   8 bits   | 4 bits |        20 bits         |            32 bits            |
//	+------------+--------+------------------------+-------------------------------+
//
// Segment Definitions:
//   - Account ID (bits 0-31): Unique 32-bit account sequence number assigned by Valve.
//   - Instance (bits 32-51): Account instance modifier (1 = Desktop, 2 = Console, 4 = Web).
//   - Type (bits 52-55): Account classification type code:
//   - 1: Individual (User)
//   - 3: GameServer
//   - 7: Clan (Steam Group)
//   - 8: Chat Room
//   - Universe (bits 56-63): Network universe code (1 = Public, 2 = Beta, 3 = Internal, 4 = Dev).
//
// # 2. Representation Formats & Conversions
//
//   - Raw 64-bit Integer: 76561197999999999
//   - Legacy Steam2 Format: "STEAM_X:Y:Z"
//     Formula: AccountID = Z * 2 + Y (where X is Universe, usually 0 or 1).
//   - Modern Steam3 Format: "[Type:Universe:AccountID]"
//     Example: "[U:1:39734273]" (where U stands for Individual).
//
// # 3. Resolution Cascade
//
// The Resolve function resolves arbitrary string inputs using a deterministic, multi-stage fallback cascade:
//  1. Fast Numeric Check: Attempts direct 64-bit uint parsing.
//  2. Legacy Format Parsing: Matches "STEAM_" prefix and computes integer SteamID.
//  3. Steam3 Format Parsing: Matches "[U:1:...]" brackets.
//  4. Profile URL Extraction: Extracts custom slugs or numeric IDs from "steamcommunity.com/profiles/..." or "/id/...".
//  5. WebAPI Fallback: Dispatches an ISteamUser/ResolveVanityURL API request if the input is a custom vanity slug.
package id

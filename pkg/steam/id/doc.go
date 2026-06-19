// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package id provides utilities for parsing, validating, and converting SteamIDs.
//
// It supports converting identifiers between standard legacy formats (Steam2, Steam3, AccountID)
// and the 64-bit SteamID representation.
//
// # Key Components
//
//   - [ID]: The primary, thread-safe 64-bit representation of a Steam identifier.
//   - [Universe]: Defines the target Steam network universe (such as Public or Dev).
//   - [AccountType]: Specifies the classification of the account (such as Individual or Clan).
//
// # Basic Usage Example
//
//	package main
//
//	import (
//		"fmt"
//		"github.com/lemon4ksan/g-man/pkg/steam/id"
//	)
//
//	func main() {
//		// Parse a legacy Steam2 format ID
//		steamID := id.Parse("STEAM_0:0:42063864")
//		if !steamID.IsValid() {
//			fmt.Println("Invalid SteamID")
//			return
//		}
//
//		fmt.Println("SteamID64:", steamID.String())
//		fmt.Println("Steam3:", steamID.Steam3())
//	}
package id

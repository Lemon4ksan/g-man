// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package id provides tools to parse, validate, and resolve Steam 64-bit unique identifiers.
// It decodes legacy Steam2 formats, modern Steam3 formats, and profile URLs, checking them against
// plausible network universes and account classifications.
//
// The central type is [ID] representing a 64-bit integer, which can be extracted via [Parse],
// created via [New], or resolved from custom URLs via [Resolve] using [Universe] and [AccountType] configurations.
//
// Basic usage example:
//
//	package main
//
//	import (
//		"fmt"
//
//		"github.com/lemon4ksan/g-man/pkg/steam/id"
//	)
//
//	func main() {
//		parsed := id.Parse("STEAM_0:0:42063864")
//		if parsed.IsValid() {
//			fmt.Println("Steam3 Format:", parsed.Steam3())
//			fmt.Println("Account ID:", parsed.AccountID())
//		}
//	}
package id

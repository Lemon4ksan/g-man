// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package encoding provides utilities for parsing Steam WebAPI responses in various formats.
//
// # Key Components
//
//   - [UnmarshalRegistry]: A thread-safe registry of decoders used to parse multiple response formats.
//   - [ResponseFormat]: An enumeration defining supported formats (JSON, VDF, Protobuf, Binary VDF).
//
// # Basic Usage Example
//
//	package main
//
//	import (
//		"fmt"
//		"github.com/lemon4ksan/g-man/pkg/steam/encoding"
//	)
//
//	type Summary struct {
//		SteamID uint64 `json:"steamid"`
//	}
//
//	func main() {
//		// Create a new unmarshal registry
//		reg := encoding.NewUnmarshalRegistry()
//
//		// Mock raw Steam WebAPI JSON response data (usually wrapped under "response")
//		rawData := []byte(`{"response":{"steamid":76561197960265728}}`)
//
//		var res Summary
//		err := reg.Unmarshal(rawData, &res, encoding.FormatJSON)
//		if err != nil {
//			fmt.Println("Unmarshal failed:", err)
//			return
//		}
//
//		fmt.Println("Parsed SteamID:", res.SteamID)
//	}
package encoding

// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package guard implements the logic for handling Steam Guard Mobile Authenticator confirmations.
//
// This module automates the process of fetching, parsing, and acting upon mobile
// confirmations, which are required for market listings and trade offers when
// two-factor authentication (2FA) is enabled.
//
// # Key Components
//
//   - [Guardian]: The central module manager that coordinates Steam Guard actions and generates TOTP codes.
//   - [Confirmation]: Represents a single pending mobile confirmation on Steam.
//   - [Clock]: An interface used to synchronize local time with Steam server time.
//   - [ConfService]: An interface used to communicate with Steam's mobile confirmation endpoints.
//   - [Config]: Defines configuration options including shared secrets and device identifiers.
//
// # Basic Usage Example
//
//	package main
//
//	import (
//		"fmt"
//		"github.com/lemon4ksan/g-man/pkg/steam/guard"
//	)
//
//	func main() {
//		cfg := guard.DefaultConfig()
//		cfg.SharedSecret = "base64SharedSecret=="
//		cfg.DeviceID = "android:8ff408c4-e869-42b7-a36a-2d9d1be64889"
//
//		g, err := guard.New(cfg)
//		if err != nil {
//			fmt.Println("Initialization failed:", err)
//			return
//		}
//
//		code, err := g.GenerateAuthCode()
//		if err != nil {
//			fmt.Println("Failed to generate code:", err)
//			return
//		}
//
//		fmt.Println("Steam Guard Code:", code)
//	}
package guard

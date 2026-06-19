// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package auth implements the complex, multi-stage authentication logic required to establish a secure session with Steam.
//
// # Key Components
//
//   - [Authenticator]: The primary orchestrator that negotiates the secure handshake and logon phase with Steam CMs.
//   - [AuthenticationService]: A gateway for Steam's modern Unified WebAPI authentication endpoints.
//   - [LogOnDetails]: Holds credential parameters, refresh tokens, and device identifiers used during authentication.
//   - [Store]: An interface used to persist authentication states (such as tokens and machine IDs).
//   - [DeviceConfig]: Allows customizing how the client presents its hardware profile to Steam.
//
// # Basic Usage Example
//
//	package main
//
//	import (
//		"context"
//		"fmt"
//		"github.com/lemon4ksan/g-man/pkg/steam/auth"
//		"github.com/lemon4ksan/g-man/pkg/steam/service"
//		tr "github.com/lemon4ksan/g-man/pkg/steam/transport"
//	)
//
//	func main() {
//		ctx := context.Background()
//
//		// Prepare HTTP transport wrapping the WebAPI base
//		transport := tr.NewHTTPTransport(nil, service.WebAPIBase)
//		serviceClient := service.New(transport)
//
//		// Create a new authentication service
//		authSvc := auth.NewAuthenticationService(serviceClient, nil)
//
//		// Retrieve the public key for an account
//		resp, err := authSvc.GetPasswordRSAPublicKey(ctx, "steam_user")
//		if err != nil {
//			fmt.Println("Failed to retrieve public key:", err)
//			return
//		}
//
//		fmt.Println("Timestamp of key:", resp.GetTimestamp())
//	}
package auth

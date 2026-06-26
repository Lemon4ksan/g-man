// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package openid provides automated OpenID authentication for third-party websites
// that use "Sign in through Steam".
//
// It manages CookieJars and executes the OpenID claim assertion flow on Steam Community.
//
// # Basic Usage Example
//
//	package main
//
//	import (
//		"context"
//		"fmt"
//		"net/http"
//		"github.com/lemon4ksan/aoni"
//		"github.com/lemon4ksan/g-man/pkg/steam/community/openid"
//	)
//
//	func main() {
//		ctx := context.Background()
//		// Active Steam cookies retrieved from auth.WebSession
//		var activeCookies []*http.Cookie
//		// Authorize and retrieve a session-authenticated client
//		client, err := openid.Login(ctx, "https://csgo-trading-site.com/login", activeCookies)
//		if err != nil {
//			fmt.Println("Login failed:", err)
//			return
//		}
//		// Now use the client to perform requests to the third-party site
//		_ = client
//	}
package openid

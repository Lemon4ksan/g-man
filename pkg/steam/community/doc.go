// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package community provides high-level helper functions for interacting with the Steam Community.
// It simplifies performing common HTTP operations such as executing GET requests for JSON or HTML,
// as well as submitting POST requests with form URL-encoded or JSON payloads.
//
// The package defines the [Requester] and [SessionProvider] interface aliases which map directly
// to their counterparts in [client]. It provides the [Decorate] function to wrap a requester
// with default modifiers, and helper functions like [GetJSON], [GetHTML], [PostForm], and [PostJSON]
// to streamline communications.
//
// Basic usage example:
//
//	package main
//
//	import (
//		"context"
//		"fmt"
//
//		"github.com/lemon4ksan/g-man/pkg/steam/community"
//	)
//
//	type Inventory struct {
//		Success bool `json:"success"`
//	}
//
//	func main() {
//		c := community.NewClient(nil, nil)
//		inv, err := community.Get[Inventory](context.Background(), c, "inventory", nil)
//		if err != nil {
//			panic(err)
//		}
//		fmt.Println("Inventory fetch success:", inv.Success)
//	}
package community

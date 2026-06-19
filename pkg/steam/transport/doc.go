// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package transport is the architectural bridge that unifies communication over different network protocols.
//
// It provides a common, protocol-agnostic API for sending requests and receiving responses,
// allowing higher-level packages to operate without knowledge of the underlying network layer.
//
// # Key Components
//
//   - [Transport]: The core interface defining the contract for executing unified, protocol-agnostic requests.
//   - [Target]: A marker interface representing the logical destination of an API call.
//   - [Request]: A generic container holding the target, payload, headers, and routing metadata.
//   - [Response]: A generic container wrapping the resulting payload and transport-specific metadata.
//   - [HTTPTransport]: An implementation of [Transport] for HTTP-based communication.
//   - [SocketTransport]: An implementation of [Transport] for persistent socket-based communication.
//
// # Basic Usage Example
//
//	package main
//
//	import (
//		"context"
//		"fmt"
//		"github.com/lemon4ksan/g-man/pkg/steam/transport"
//	)
//
//	// MockTarget implements transport.Target for testing.
//	type MockTarget struct{}
//
//	func (m MockTarget) String() string { return "Mock.Method" }
//
//	func (m MockTarget) HTTPPath() string { return "Mock/Method/v1" }
//
//	func (m MockTarget) HTTPMethod() string { return "GET" }
//
//	func main() {
//		ctx := context.Background()
//
//		// Create HTTP transport wrapping the default http.Client
//		tr := transport.NewHTTPTransport(nil, "https://api.example.com/")
//
//		// Prepare an abstract request
//		req := transport.NewRequest(MockTarget{}, nil)
//
//		// Execute the request
//		resp, err := tr.Do(ctx, req)
//		if err != nil {
//			fmt.Println("Request failed:", err)
//			return
//		}
//
//		// Extract HTTP-specific metadata safely using response helpers
//		if meta, ok := resp.HTTP(); ok {
//			fmt.Println("HTTP Status Code:", meta.StatusCode)
//		}
//	}
package transport

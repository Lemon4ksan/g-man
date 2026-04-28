// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package rest provides a lightweight, generic wrapper around net/http with
// first-class support for proxy rotation and structured request building.
//
// The package focuses on reducing boilerplate when dealing with REST APIs
// by using Go generics for automatic decoding and a "RequestModifier" pattern
// for flexible request customization.
//
// # Core Features
//
//   - Generics: Automatic JSON/VDF decoding directly into structs using GetJSON[T](...).
//   - Immutability: Client methods return new instances, making it safe to share
//     a base client across different parts of an application.
//   - Proxy Rotation: Robust [ProxyRotator] with passive health checks (Circuit Breaker).
//   - Struct-to-Query: Convert Go structs into url.Values using "url" tags via [StructToValues].
//
// # Proxy & Resilience
//
// The [ProxyRotator] is designed for high-load scrapers and bots. It distinguishes
// between "logic errors" (like 404 Not Found) and "proxy errors" (like 407 Proxy Auth,
// 429 Too Many Requests, or network timeouts).
//
// If a proxy is identified as faulty, the rotator:
//  1. Marks it as unhealthy and temporarily excludes it from rotation.
//  2. Automatically retries the request using the next available proxy.
//  3. Gives the proxy a "cooldown" period before attempting to use it again.
//
// # Structured Requests
//
// Instead of manual string manipulation for query parameters, you can use structs:
//
//	type SearchParams struct {
//	    Query string `url:"q"`
//	    Page  int    `url:"p,omitempty"`
//	}
//	v, _ := rest.StructToValues(SearchParams{Query: "g-man"})
//	// Result: q=g-man
//
// # Error Handling
//
// If the server returns a non-2xx status code, methods return an [*APIError].
// It wraps the status code and raw response body. You can use errors.Is with
// [ErrProxyAuthRequired] to detect proxy-level authentication issues.
//
// # Usage Example
//
//	// Create a client with rotating proxies
//	p1, _ := rest.NewProxyClient(rest.ProxyConfig{ProxyURL: "http://proxy1:8080"})
//	p2, _ := rest.NewProxyClient(rest.ProxyConfig{ProxyURL: "http://proxy2:8080"})
//
//	rotator, _ := rest.NewProxyRotator(rest.ProxyRotatorConfig{MaxFails: 3}, p1, p2)
//	client := rest.NewClient(rest.WithDoer(rotator))
//
//	// Perform a generic GET request
//	data, err := rest.GetJSON[MyResponse](ctx, client, "https://api.example.com/data")
package rest

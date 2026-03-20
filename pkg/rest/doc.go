// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package rest provides a lightweight, generic wrapper around net/http.
//
// The package focuses on reducing boilerplate when dealing with JSON APIs
// by using Go generics for automatic decoding and a "RequestModifier" pattern
// for flexible request customization.
//
// # Features
//   - Zero-boilerplate JSON decoding via generics: GetJSON[T](...)
//   - Immutability: Client.WithHeader returns a new client instance.
//   - Flexible modifiers: Pass any function to modify headers, cookies, or context per-request.
//
// # Error Handling
// If the server returns a non-2xx status code, the methods return an *APIError.
// This error contains both the status code and the raw response body, which
// is often essential for debugging failed REST calls.
package rest

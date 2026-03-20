// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package community provides a high-level client for the steamcommunity.com website.
//
// Unlike the structured 'service' package which uses official WebAPIs, this
// package interacts with the "web" side of Steam. This includes parsing HTML pages,
// performing AJAX calls, and handling legacy form-encoded data.
//
// # Soft Error Detection
//
// Steam Community often returns a "200 OK" status code even when an error occurs,
// displaying a "Sorry!" page or a login prompt instead of the requested data.
// This package automatically scrapes the response body to detect these cases
// and returns meaningful Go errors (e.g., ErrNotLoggedIn, ErrRateLimit).
//
// # Session Management
//
// Most state-changing operations (POST/PUT) on Steam Community require a
// 'sessionid' for CSRF protection. The Client in this package automatically
// manages and injects this identifier into requests.
//
// # Use Cases
//
// Use this package for features not available in the official WebAPI, such as:
//   - Managing trade offers and confirmations.
//   - Interacting with Steam Market.
//   - Scraping public profiles or group pages.
package community

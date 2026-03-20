// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

/*
Package service provides a high-level RPC-like commander for interacting with
Steam's official interfaces. It acts as the "Command Center" of the library,
abstracting the three primary ways to communicate with Steam.

# Three Communication Patterns

1. WebAPI (Classic):
Standard JSON-based endpoints (e.g., ISteamUser/GetPlayerSummaries). These usually
require a WebAPI Key and return results in JSON or VDF.

2. Unified Services (Modern):
The modern, Protobuf-based service architecture (e.g., IPlayerService/GetNickname).
These can be called over HTTP (POST + Protobuf) or via WebSockets/TCP.

3. Legacy (Socket-only):
Raw messages identified by an EMsg (e.g., EMsgClientLogOn), primarily used for
low-level Steam client logic over a socket connection.

# Transport Agnosticism

The package is built on top of the 'transport' layer. This means you can use
the same 'service.Client' to send a request regardless of whether you are
connected via a standard HTTP client or a persistent WebSocket/TCP connection.
The underlying 'Target' implementations handle the mapping to the correct
URL path or EMsg identifier.

# Automatic Method Inference

To reduce boilerplate, the package can infer the Steam Interface and Method name
directly from the Protobuf request struct's name using reflection:

	// Infers "Player" interface and "GetGameBadgeLevels" method
	req := &CPlayer_GetGameBadgeLevels_Request{...}
	res, err := service.Unified[MyResponse](ctx, client, req)

# Error Handling

The package automatically intercepts and validates Steam-specific results.
It checks for:
  - HTTP Status Codes (standard 4xx/5xx errors).
  - Steam EResult codes (provided in HTTP headers or Socket metadata).

If an EResult is not 'OK', the client returns an 'api.EResultError'.
*/
package service

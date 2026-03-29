// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

/*
Package auth provides the core authentication flows for the Steam client.

Unlike standard modules (like 'friends' or 'market') which operate after a
connection is established, the 'auth' module is responsible for bootstrapping
the connection itself. It is deeply integrated into the lifecycle of the
main 'steam.Client'.

# The Two-Phase Authentication Flow

Steam's authentication is historically complex, blending modern OAuth-like
flows with legacy binary socket handshakes. This package abstracts that
complexity into a seamless two-phase process:

 1. WebAPI Phase (AuthenticationService):
    Uses the 'service' package (REST over HTTP) to perform a modern JWT-based
    login. It fetches RSA keys, encrypts the password, handles 2FA/Email Guard
    challenges, and polls until Steam issues a "Refresh Token".

 2. Socket Phase (Authenticator):
    Takes the Refresh Token from Phase 1 and establishes a persistent TCP/WebSocket
    connection to a Connection Manager (CM) server. It negotiates the symmetric
    channel encryption (ChannelEncryptRequest/Response) and sends a `ClientLogOn`
    message containing the token to finalize the session.

# Key Components

  - Authenticator: The state machine that orchestrates the entire login process.
    It manages the transition from HTTP to Sockets, handles reconnection logic,
    and publishes state changes to the global Event Bus.

  - AuthenticationService: A stateless wrapper around the 'service' package
    that implements the gRPC-like 'Authentication' endpoints. It is responsible
    for RSA encryption and token generation.

  - WebSession: A specialized HTTP client that performs the OIDC (OpenID Connect)
    redirection flow to acquire 'sessionid' and 'steamLoginSecure' cookies.
    These cookies are essential for modules like 'market' and 'community' to scrape
    HTML or perform AJAX actions on steamcommunity.com.
*/
package auth

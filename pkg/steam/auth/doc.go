// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

/*
Package auth implements the complex, multi-stage authentication logic required to
establish a secure session with Steam.

Unlike standard modules (like 'friends' or 'market') which operate after a
connection is established, the 'auth' module is responsible for bootstrapping
the connection itself. It is deeply integrated into the lifecycle of the
main 'steam.Client'.

# Core Responsibilities

 1. Credential Encryption: Securely encrypting passwords using Steam's RSA public
    keys before transmission.

 2. Multi-Factor Authentication: Handling Steam Guard (Email), Two-Factor (Mobile
    App), and modern Mobile App Confirmations via an event-driven callback system.

 3. Token Management: Orchestrating the lifecycle of JWT-based Refresh and Access
    tokens to provide persistent, long-running sessions.

 4. Transport Encryption: Handling the TCP handshake (ChannelEncrypt) to establish
    a symmetric encrypted tunnel with Steam CMs.

 5. Web Session Synchronization: Executing the OpenID Connect (OIDC) flow to
    synchronize authentication cookies across all Steam domains (community, store, help).

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

# Handling Steam Guard

Authentication often requires user interaction. When a challenge is issued,
the Authenticator emits a SteamGuardRequiredEvent via the event bus.
This event contains a 'Callback' function. To continue the login, the consumer
must obtain the code from the user and invoke this callback:

	bus.Subscribe(func(ev *auth.SteamGuardRequiredEvent) {
		code := promptUserForCode() // e.g., via CLI or UI
		ev.Callback(code)
	})

# Security and Persistence

To minimize 2FA prompts, it is highly recommended to provide a storage.AuthStore
implementation. This allows the Authenticator to persist MachineIDs and
Refresh Tokens, making the client appear as a "Recognized Device" to Steam.
*/
package auth

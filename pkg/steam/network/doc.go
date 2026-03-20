// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

/*
Package network provides low-level, protocol-specific network connection
implementations (TCP and WebSocket). It is the foundational "socket layer"
of the library, responsible for raw data transmission and framing.

# Architectural Role

This package deals with the raw realities of network programming:
  - Dialing endpoints.
  - Framing messages (e.g., length-prefixing for TCP).
  - Handling encryption and decryption.
  - Reading from the socket in a dedicated loop and pushing messages up.

It abstracts these details behind a single `Connection` interface. Higher-level
packages like `transport` use this interface to send and receive logical
Steam messages without needing to know if they are traveling over TCP or WebSockets.

# Connection Lifecycle

1. A connection is established using a `New...Connection` function.
2. The function immediately starts a `readLoop` in a background goroutine.
3. The `readLoop` continuously reads data from the socket. When a full message
   is received, it calls the `OnNetMessage` callback on the provided `Handler`.
4. Other parts of the application can send data using the `Send` method.
5. If the connection is terminated (by the remote peer or an error), the `OnNetClose`
   callback is invoked, signaling the end of the connection's life.

This event-driven model (via the `Handler` interface) allows for a clean
separation of network I/O from the business logic of processing messages.
*/
package network

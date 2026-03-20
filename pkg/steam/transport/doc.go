// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

/*
Package transport is the architectural "bridge" that unifies communication
over different network protocols (HTTP and WebSockets/TCP). It provides a
common, protocol-agnostic API for sending requests and receiving responses,
allowing higher-level packages like 'service' to operate without knowledge
of the underlying network layer.

# Core Concepts

1. Target:
An interface representing the destination of a request. Concrete implementations
(e.g., `service.UnifiedTarget`) know how to translate themselves into an
HTTP path for web requests or an `EMsg` for socket messages.

2. Request:
A protocol-agnostic container for an API call. It holds the `Target`, a binary
payload (`body`), URL parameters, and headers.

3. Transport:
An interface with a single `Do` method. There are two main implementations:
  - `HTTPTransport`: Translates a `Request` into a standard `http.Request` and
    sends it via the `rest` client.
  - `SocketTransport`: Translates a `Request` into a `protocol.Packet` and sends
    it via the `socket` client.

4. Response:
A protocol-agnostic container for the result of an API call. It includes the raw
response body and protocol-specific `metadata` (e.g., HTTP status codes or
Socket `EResult` codes), which can be safely extracted using the `As()` method.

# How It Works

When a high-level package like `service` makes a call, it creates a `Request`
containing a specific `Target`. This `Request` is then passed to a `Transport`.
The `Transport` inspects the `Target` type to determine how to handle it.

  - If `HTTPTransport` receives a `Request` with an `HTTPTarget`, it calls
    `target.HTTPPath()` to build the URL and sends the request.
  - If `SocketTransport` receives a `Request` with a `SocketTarget`, it calls
    `target.EMsg()` to get the message ID and sends it over the socket.

This design allows the same application code to function seamlessly whether it's
acting as a web-based bot or a full-fledged desktop client.
*/
package transport

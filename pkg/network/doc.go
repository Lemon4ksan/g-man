// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package network provides low-level, protocol-specific network connection
// implementations (TCP and WebSocket). It is the foundational "socket layer"
// of the library, responsible for raw data transmission and framing.
//
// # Key Components
//
//   - [Connection]: The primary interface representing a bi-directional connection.
//   - [Framer]: Handles message framing and unframing on stream-oriented transports.
//   - [Cipher]: Provides optional symmetric encryption and decryption for payloads.
//   - [Error]: Represents structured, transport-agnostic network errors.
//
// # Connection Lifecycle
//
// A connection is established using constructor functions like [NewTCP] or [NewWS].
// Incoming messages are read asynchronously in a background loop and sent to
// the channel returned by [Connection.Messages]. Outgoing messages are sent using
// the [Connection.Send] method.
//
// # Basic Usage Example
//
//	package main
//
//	import (
//		"context"
//		"fmt"
//		"io"
//		"github.com/lemon4ksan/g-man/pkg/log"
//		"github.com/lemon4ksan/g-man/pkg/network"
//	)
//
//	// DummyFramer implements network.Framer for basic testing.
//	type DummyFramer struct{}
//
//	func (f DummyFramer) ReadFrame(r io.Reader) ([]byte, error) {
//		buf := make([]byte, 1024)
//		n, err := r.Read(buf)
//		return buf[:n], err
//	}
//
//	func (f DummyFramer) WriteFrame(w io.Writer, data []byte) error {
//		_, err := w.Write(data)
//		return err
//	}
//
//	func main() {
//		logger := log.New(log.DefaultConfig(log.LevelInfo))
//		ctx := context.Background()
//
//		// Connect to a local server
//		conn, err := network.NewTCP(ctx, logger, "127.0.0.1:8080", "", DummyFramer{})
//		if err != nil {
//			fmt.Println("Connection failed:", err)
//			return
//		}
//		defer conn.Close()
//	}
package network

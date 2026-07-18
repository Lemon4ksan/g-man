// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package module implements a lifecycle-managed plugin system to extend client behavior.
//
// Use modules to add specialized features—like trading, chat handling, or inventory
// tracking—to a Steam client.
//
// # Implementing a Module
//
// To create a custom module, embed [Base] into your struct. This inherits default
// lifecycle handlers, concurrent task tracking, and logging helpers:
//
//	type ChatEchoModule struct {
//		module.Base
//	}
//
//	func NewEcho() *ChatEchoModule {
//		return &ChatEchoModule{
//			Base: module.New("chat_echo").WithDeps("chat"),
//		}
//	}
//
//	func (m *ChatEchoModule) Init(ctx module.InitContext) error {
//		_ = m.Base.Init(ctx)
//		ctx.RegisterPacketHandler(enums.EMsg_ClientPersonaState, m.onPersona)
//		return nil
//	}
//
// # Concurrency and Shutdown
//
// Run long-running background tasks inside a module using [Base.Go]. Goroutines spawned
// this way are automatically registered, tracked, and gracefully canceled when the
// client shuts down or restarts.
package module

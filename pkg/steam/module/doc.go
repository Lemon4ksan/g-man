// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

/*
Package module implements a robust, lifecycle-managed plugin system for the Steam client.

It provides a standardized way to extend the client with specialized logic such as
trading, chat management, game coordinators (GC), or inventory tracking.

# Architecture

The system is built around the "Base" pattern. Instead of implementing every method
of the [Module] interface manually, developers should embed [Base] into their structs.
This provides default implementations for lifecycle management, logging, and concurrency.

# Lifecycle

A module goes through several strictly defined phases:

 1. Initialization: The [Module.Init] method is called. This is where you register
    packet handlers via [InitContext] and subscribe to internal events.
 2. Startup: The [Module.Start] method is called when the client begins its main loop.
    Background tasks (tickers, pollers) should be launched here using [Base.Go].
 3. Authentication (Optional): If a module implements the [Auth] interface,
    [Auth.StartAuthed] is called whenever the client establishes a valid web session.
 4. Shutdown: When the client stops, the module's context is canceled, and
    [Base.Close] waits for all background goroutines to finish.

# Concurrency and Safety

All modules share the same event bus and network socket. The [Base.Go] method
ensures that goroutines are tracked and properly waited upon during shutdown,
preventing "leaky" goroutines and race conditions during client restarts.

# Example: Creating a Simple Module

	type MyModule struct {
		module.Base
	}

	func NewMyModule() *MyModule {
		return &MyModule{Base: module.New("my_module")}
	}

	func (m *MyModule) Init(ctx module.InitContext) error {
		m.Base.Init(ctx) // Always call Base.Init first
		ctx.RegisterPacketHandler(enums.EMsg_ClientPersonaState, m.onPersonaState)
		return nil
	}

	func (m *MyModule) Start(ctx context.Context) error {
		m.Base.Start(ctx)
		m.Go(m.myBackgroundLoop)
		return nil
	}

	func (m *MyModule) onPersonaState(p *protocol.Packet) {
		m.Logger.Info("Received persona state")
	}

	func (m *MyModule) myBackgroundLoop(ctx context.Context) {
		ticker := time.NewTicker(time.Minute)
		for {
			select {
			case <-ticker.C:
				m.Logger.Debug("Tick!")
			case <-ctx.Done():
				return
			}
		}
	}
*/
package module

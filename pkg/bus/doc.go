// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package bus implements a high-performance, type-based event bus for
// asynchronous communication between Steam client modules and plugins.
//
// The bus uses Go's reflection system to route events based on their
// underlying struct type. This allows for a decoupled architecture where
// components can react to system events (like "Disconnected" or "TradeReceived")
// without direct dependencies on the emitters.
//
// Example:
//
//	type MyEvent struct { bus.BaseEvent; Message string }
//
//	b := bus.NewBus()
//	sub := b.Subscribe(MyEvent{})
//
//	go func() {
//	    for ev := range sub.C() {
//	        msg := ev.(MyEvent).Message
//	        fmt.Println("Received:", msg)
//	    }
//	}()
//
//	b.Publish(MyEvent{Message: "Hello!"})
package bus

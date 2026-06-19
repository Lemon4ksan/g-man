// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package bus implements a type-based event bus for asynchronous communication.
//
// It provides a decoupled architecture where components can publish and
// subscribe to typed events without direct dependencies.
//
// # Key Components
//
//   - [Bus]: The central thread-safe coordinator that routes events.
//   - [Event]: The marker interface implemented by embedding BaseEvent.
//   - [Subscription]: The consumer handle providing an event channel.
//
// # Basic Usage Example
//
//	package main
//
//	import (
//		"fmt"
//		"sync"
//		"github.com/lemon4ksan/g-man/pkg/bus"
//	)
//
//	type MyEvent struct {
//		bus.BaseEvent
//		Message string
//	}
//
//	func main() {
//		b := bus.New()
//		sub := b.Subscribe(MyEvent{})
//		defer sub.Unsubscribe()
//
//		var wg sync.WaitGroup
//
//		wg.Go(func() {
//			for ev := range sub.C() {
//				msg := ev.(MyEvent).Message
//				fmt.Println("Received:", msg)
//			}
//		})
//
//		b.Publish(MyEvent{Message: "Hello!"})
//		b.Close() // Closes all subscription channels, letting the goroutine exit.
//		wg.Wait()
//	}
package bus

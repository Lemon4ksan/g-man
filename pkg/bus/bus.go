// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package bus

import (
	"maps"
	"reflect"
	"slices"
	"sync"
	"sync/atomic"
)

// Option is a common pattern used for configuration of components.
type Option[T any] func(T)

// Event is the marker interface for all events in the system.
// To make a struct an Event, simply embed BaseEvent.
//
// Example:
//
//	type UserLoginEvent struct {
//	    bus.BaseEvent
//	    Username string
//	}
type Event interface {
	isEvent()
}

// BaseEvent provides a default implementation of the Event interface.
// Structs should embed this to satisfy the Event marker.
type BaseEvent struct{}

func (BaseEvent) isEvent() {}

// resolveType strips the pointer from a type to ensure pointer and value
// instances of the same struct are treated as the same event type.
// For example, both `MyEvent{}` and `&MyEvent{}` resolve to `MyEvent`.
func resolveType(ev any) reflect.Type {
	t := reflect.TypeOf(ev)
	if t != nil && t.Kind() == reflect.Pointer {
		return t.Elem()
	}

	return t
}

// Subscription represents an active listener on the bus.
// It provides a channel for receiving events and methods for lifecycle management.
type Subscription struct {
	id     uint64
	types  []reflect.Type
	ch     chan Event
	closed atomic.Bool
	bus    *Bus
}

// C returns the read-only channel for receiving events.
// The channel will be closed when the subscriber calls Unsubscribe or the Bus is closed.
func (s *Subscription) C() <-chan Event { return s.ch }

// Unsubscribe removes the subscription from the bus and closes the event channel.
// It is safe to call this method multiple times.
func (s *Subscription) Unsubscribe() {
	if s.closed.CompareAndSwap(false, true) {
		s.bus.unsubscribe(s)
	}
}

// Bus implements a thread-safe, non-blocking event dispatcher.
// It routes events based on their Go [reflect.Type].
type Bus struct {
	mu     sync.RWMutex
	subs   map[reflect.Type]map[uint64]*Subscription
	all    map[uint64]*Subscription
	nextID atomic.Uint64
	closed bool
}

// New initializes a new Event Bus.
func New() *Bus {
	return &Bus{
		subs: make(map[reflect.Type]map[uint64]*Subscription),
		all:  make(map[uint64]*Subscription),
	}
}

// Subscribe subscribes to specific event types using examples.
// Passing MyEvent{} will subscribe to all events of type MyEvent.
//
// Example:
//
//	sub := bus.Subscribe(LoginEvent{}, &DisconnectEvent{})
func (b *Bus) Subscribe(evs ...Event) *Subscription {
	b.mu.Lock()
	defer b.mu.Unlock()

	id := b.nextID.Add(1)
	sub := &Subscription{
		id:  id,
		ch:  make(chan Event, 128), // Buffered to handle minor bursts
		bus: b,
	}

	if b.closed {
		sub.closed.Store(true)
		close(sub.ch)

		return sub
	}

	for _, ev := range evs {
		t := resolveType(ev)

		// Ensure we don't subscribe to the same type twice for one subscription
		if !slices.Contains(sub.types, t) {
			sub.types = append(sub.types, t)
			if b.subs[t] == nil {
				b.subs[t] = make(map[uint64]*Subscription)
			}

			b.subs[t][id] = sub
		}
	}

	return sub
}

// SubscribeAll creates a subscription that receives every single event published to the bus.
// This is useful for logging, debugging, or global state monitoring.
func (b *Bus) SubscribeAll() *Subscription {
	b.mu.Lock()
	defer b.mu.Unlock()

	id := b.nextID.Add(1)
	sub := &Subscription{
		id:  id,
		ch:  make(chan Event, 256), // Larger buffer for global listeners
		bus: b,
	}

	if b.closed {
		sub.closed.Store(true)
		close(sub.ch)

		return sub
	}

	b.all[id] = sub

	return sub
}

// Publish broadcasts an event to all interested subscribers.
//
// NON-BLOCKING: If a subscriber's channel buffer is full, the event is dropped
// for that specific subscriber to avoid stalling the entire bus. In high-load
// scenarios, ensure subscribers process events quickly or use larger buffers.
func (b *Bus) Publish(event Event) {
	if event == nil {
		return
	}

	b.mu.RLock()
	defer b.mu.RUnlock()

	if b.closed {
		return
	}

	// Send to specific type subscribers
	if typeSubs, ok := b.subs[resolveType(event)]; ok {
		for _, sub := range typeSubs {
			b.directSend(sub, event)
		}
	}

	// Send to "SubscribeAll" listeners
	for _, sub := range b.all {
		b.directSend(sub, event)
	}
}

// directSend attempts a non-blocking send to a subscription channel.
func (b *Bus) directSend(sub *Subscription, ev Event) {
	if sub.closed.Load() {
		return
	}

	select {
	case sub.ch <- ev:
	default:
		// Buffer full - drop event to maintain system stability.
	}
}

// Close shuts down the bus and closes all active subscription channels.
// After Close, no new subscriptions can be made and Publish calls will be ignored.
func (b *Bus) Close() error {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.closed {
		return nil
	}

	b.closed = true

	// Collect unique subs to close channels exactly once (a sub can be in multiple maps)
	unique := make(map[uint64]*Subscription)
	for _, m := range b.subs {
		maps.Copy(unique, m)
	}

	maps.Copy(unique, b.all)

	for _, s := range unique {
		s.closed.Store(true)
		close(s.ch)
	}

	b.subs = nil
	b.all = nil

	return nil
}

func (b *Bus) unsubscribe(sub *Subscription) {
	b.mu.Lock()
	defer b.mu.Unlock()

	// Remove from type-specific maps
	for _, t := range sub.types {
		if typeSubs, ok := b.subs[t]; ok {
			delete(typeSubs, sub.id)

			if len(typeSubs) == 0 {
				delete(b.subs, t)
			}
		}
	}

	// Remove from global map
	delete(b.all, sub.id)

	// Close channel to unblock any 'range sub.C()'
	close(sub.ch)
}

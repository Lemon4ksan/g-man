// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package bus implements a high-performance, type-based event bus for
// asynchronous communication between Steam client modules and plugins.
package bus

import (
	"slices"
	"reflect"
	"sync"
	"sync/atomic"
)

// Event is the marker interface for all events in the system.
// Any struct can be an event by embedding BaseEvent.
type Event interface {
	isEvent()
}

// BaseEvent provides a default implementation of the Event interface.
type BaseEvent struct{}

func (BaseEvent) isEvent() {}

// Subscription represents an active listener on the bus.
type Subscription struct {
	id     uint64
	types  []reflect.Type
	ch     chan Event
	closed atomic.Bool
	bus    *Bus
}

// C returns the read-only channel for receiving events.
func (s *Subscription) C() <-chan Event { return s.ch }

// Unsubscribe removes the subscription from the bus and closes the channel.
func (s *Subscription) Unsubscribe() {
	if s.closed.CompareAndSwap(false, true) {
		s.bus.unsubscribe(s)
	}
}

// Bus implements a thread-safe, non-blocking event dispatcher.
// It routes events based on their Go reflect.Type.
type Bus struct {
	mu     sync.RWMutex
	subs   map[reflect.Type]map[uint64]*Subscription
	all    map[uint64]*Subscription
	nextID atomic.Uint64
	closed bool
}

// NewBus initializes a new Event Bus.
func NewBus() *Bus {
	return &Bus{
		subs: make(map[reflect.Type]map[uint64]*Subscription),
		all:  make(map[uint64]*Subscription),
	}
}

// Subscribe subscribes to specific event types.
// Usage: bus.Subscribe(MyEvent{}, OtherEvent{})
func (b *Bus) Subscribe(eventExamples ...Event) *Subscription {
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

	for _, ev := range eventExamples {
		t := reflect.TypeOf(ev)
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

// SubscribeAll creates a subscription that receives every event published to the bus.
func (b *Bus) SubscribeAll() *Subscription {
	b.mu.Lock()
	defer b.mu.Unlock()

	id := b.nextID.Add(1)
	sub := &Subscription{
		id:  id,
		ch:  make(chan Event, 256),
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
// If a subscriber's buffer is full, the event is dropped to avoid blocking the system.
func (b *Bus) Publish(event Event) {
	if event == nil {
		return
	}

	t := reflect.TypeOf(event)

	b.mu.RLock()
	if b.closed {
		b.mu.RUnlock()
		return
	}

	// Optimization: instead of creating a slice of targets, we iterate
	// directly under RLock and send in goroutines or non-blocking.
	// This reduces allocations per publish.

	if typeSubs, ok := b.subs[t]; ok {
		for _, sub := range typeSubs {
			b.directSend(sub, event)
		}
	}

	for _, sub := range b.all {
		b.directSend(sub, event)
	}
	b.mu.RUnlock()
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
		// In a trading bot, log this as a warning.
	}
}

func (b *Bus) Close() {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.closed {
		return
	}
	b.closed = true

	// Collect unique subs to close channels exactly once
	unique := make(map[uint64]*Subscription)
	for _, m := range b.subs {
		for id, s := range m {
			unique[id] = s
		}
	}
	for id, s := range b.all {
		unique[id] = s
	}

	for _, s := range unique {
		s.closed.Store(true)
		close(s.ch)
	}

	b.subs = nil
	b.all = nil
}

func (b *Bus) unsubscribe(sub *Subscription) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.closed {
		return
	}

	for _, t := range sub.types {
		if typeSubs, ok := b.subs[t]; ok {
			delete(typeSubs, sub.id)
			if len(typeSubs) == 0 {
				delete(b.subs, t)
			}
		}
	}
	delete(b.all, sub.id)
	close(sub.ch)
}

// Option is a common pattern used across the library for configuration.
type Option[T any] func(T)

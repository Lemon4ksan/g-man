// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package bus

import (
	"reflect"
	"sync"
	"sync/atomic"
)

// Option is a generic function pattern for configuration.
type Option[T any] func(T)

// Event defines the interface that unifies all library events.
type Event interface {
	isEvent()
}

// BaseEvent is the basic structure for all library events.
type BaseEvent struct{}

func (BaseEvent) isEvent() {}

// Subscription represents an active subscription to a topic.
type Subscription struct {
	id     uint64
	types  []reflect.Type
	ch     chan Event
	closed atomic.Bool
	bus    *Bus
}

// C returns the channel from which events can be read.
func (s *Subscription) C() <-chan Event {
	return s.ch
}

// Unsubscribe removes the subscription from the bus and closes its channel.
// It is safe to call multiple times.
func (s *Subscription) Unsubscribe() {
	if s.closed.CompareAndSwap(false, true) {
		s.bus.unsubscribe(s)
	}
}

// Bus is a thread-safe publish/subscribe event bus.
// It routes events to channels based on event topics.
type Bus struct {
	mu     sync.RWMutex
	subs   map[reflect.Type]map[uint64]*Subscription
	all    map[uint64]*Subscription
	nextID atomic.Uint64
	closed bool
}

// NewBus creates a new event bus.
func NewBus() *Bus {
	return &Bus{
		subs: make(map[reflect.Type]map[uint64]*Subscription),
		all:  make(map[uint64]*Subscription),
	}
}

// Subscribe creates a subscription to a single or multiple topics.
// It returns a *Subscription object which contains the channel for reading events.
// The channel is buffered to prevent blocking the publisher.
func (b *Bus) Subscribe(eventTypes ...Event) *Subscription {
	b.mu.Lock()
	defer b.mu.Unlock()

	id := b.nextID.Add(1)
	sub := &Subscription{
		id:  id,
		ch:  make(chan Event, 100),
		bus: b,
	}

	if b.closed {
		sub.closed.Store(true)
		close(sub.ch)
		return sub
	}

	for _, ev := range eventTypes {
		t := reflect.TypeOf(ev)
		sub.types = append(sub.types, t)

		if b.subs[t] == nil {
			b.subs[t] = make(map[uint64]*Subscription)
		}
		b.subs[t][id] = sub
	}

	return sub
}

// SubscribeAll subscribes to all the events coming through the bus.
func (b *Bus) SubscribeAll() *Subscription {
	b.mu.Lock()
	defer b.mu.Unlock()

	id := b.nextID.Add(1)
	sub := &Subscription{
		id:  id,
		ch:  make(chan Event, 500),
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

// Publish broadcasts an event to all subscribers of the event's Topic.
// If a subscriber's channel is full, the event is dropped for that subscriber
// to prevent blocking the entire application.
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

	var targets []*Subscription

	if typeSubs, ok := b.subs[t]; ok {
		for _, sub := range typeSubs {
			targets = append(targets, sub)
		}
	}
	for _, sub := range b.all {
		targets = append(targets, sub)
	}
	b.mu.RUnlock()

	for _, sub := range targets {
		if sub.closed.Load() {
			continue
		}
		select {
		case sub.ch <- event:
		default:
		}
	}
}

// Close gracefully shuts down the event bus, closing all subscriber channels.
func (b *Bus) Close() {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.closed {
		return
	}
	b.closed = true

	uniqueSubs := make(map[*Subscription]struct{})
	for _, typeSubs := range b.subs {
		for _, sub := range typeSubs {
			uniqueSubs[sub] = struct{}{}
		}
	}
	for _, sub := range b.all {
		uniqueSubs[sub] = struct{}{}
	}

	for sub := range uniqueSubs {
		sub.closed.Store(true)
		close(sub.ch)
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

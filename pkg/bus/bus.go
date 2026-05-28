// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package bus

import (
	"context"
	"maps"
	"reflect"
	"slices"
	"sync"
	"sync/atomic"
)

// Option represents a functional option that configures a generic target.
type Option[T any] func(T)

// Event is the marker interface for all events processed by the bus.
// Custom event structures must embed [BaseEvent] to satisfy this interface.
type Event interface {
	isEvent()
}

// BaseEvent provides a default implementation of the [Event] interface.
// Custom event structures should embed this type to act as valid events.
type BaseEvent struct {
	// Ctx represents the request context associated with the event execution.
	Ctx context.Context
}

func (BaseEvent) isEvent() {}

// Context returns the event's execution context, defaulting to context.Background() if nil.
func (b *BaseEvent) Context() context.Context {
	if b.Ctx == nil {
		b.Ctx = context.Background()
	}

	return b.Ctx
}

// SetContext sets the execution context on the event.
func (b *BaseEvent) SetContext(ctx context.Context) {
	b.Ctx = ctx
}

// Subscription represents an active event listener on a Bus instance.
// It must be obtained via Bus.Subscribe or Bus.SubscribeAll. It manages
// the lifecycle of an internal buffered event channel with a capacity of
// 128 (for specific events) or 256 (for all events). When the buffer is
// full, new incoming events are silently dropped to prevent blocking.
type Subscription struct {
	id     uint64
	types  []reflect.Type
	ch     chan Event
	closed atomic.Bool
	bus    *Bus
}

// C returns the read-only channel for receiving subscribed events.
// The channel is closed when [Subscription.Unsubscribe] is called
// or when the Bus is closed.
func (s *Subscription) C() <-chan Event { return s.ch }

// Unsubscribe removes the subscription from its associated Bus and closes the channel.
// It is safe to call Unsubscribe concurrently from multiple goroutines.
func (s *Subscription) Unsubscribe() {
	if s.closed.CompareAndSwap(false, true) {
		s.bus.unsubscribe(s)
	}
}

// Bus implements a thread-safe, non-blocking event dispatcher.
// New instances of Bus must be created using the [New] constructor function.
// It routes published events to subscriptions based on their [reflect.Type].
// Thread safety is internally managed using a [sync.RWMutex] protecting maps of subscriptions.
type Bus struct {
	mu     sync.RWMutex
	subs   map[reflect.Type]map[uint64]*Subscription
	all    map[uint64]*Subscription
	nextID atomic.Uint64
	closed bool
}

// New returns a new initialized [Bus] instance.
// The returned Bus is ready to accept subscriptions and route events.
func New() *Bus {
	return &Bus{
		subs: make(map[reflect.Type]map[uint64]*Subscription),
		all:  make(map[uint64]*Subscription),
	}
}

// Subscribe registers a subscription to receive specific event types.
// If no event types are provided or if an event is nil, those entries are ignored.
// If the Bus is closed, Subscribe returns a subscription with a pre-closed channel.
// Duplicate event types in a single Subscribe call are automatically ignored.
func (b *Bus) Subscribe(evs ...Event) *Subscription {
	b.mu.Lock()
	defer b.mu.Unlock()

	id := b.nextID.Add(1)
	sub := &Subscription{
		id:  id,
		ch:  make(chan Event, 128),
		bus: b,
	}

	if b.closed {
		sub.closed.Store(true)
		close(sub.ch)

		return sub
	}

	for _, ev := range evs {
		if ev == nil {
			continue
		}

		t := resolveType(ev)

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

// SubscribeAll registers a subscription to receive every published event on the Bus.
// If the Bus is closed, SubscribeAll returns a subscription with a pre-closed channel.
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

// Publish broadcasts a single event to all active matched subscriptions.
// If the event is nil or the Bus is closed, Publish immediately returns.
// This operation is non-blocking and drops events if a subscriber channel buffer is full.
func (b *Bus) Publish(event Event) {
	if event == nil {
		return
	}

	b.mu.RLock()
	defer b.mu.RUnlock()

	if b.closed {
		return
	}

	if typeSubs, ok := b.subs[resolveType(event)]; ok {
		for _, sub := range typeSubs {
			b.directSend(sub, event)
		}
	}

	for _, sub := range b.all {
		b.directSend(sub, event)
	}
}

// Close shuts down the Bus and closes all active subscription channels.
// It returns nil and never errors, serving to implement standard closer interfaces.
// Subsequent calls to Close are idempotent and immediately return nil.
func (b *Bus) Close() error {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.closed {
		return nil
	}

	b.closed = true

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

// resolveType strips the pointer from a type to ensure pointer and value
// instances of the same struct are treated as the same event type.
// For example, both MyEvent{} and &MyEvent{} resolve to MyEvent.
func resolveType(ev any) reflect.Type {
	t := reflect.TypeOf(ev)
	if t != nil && t.Kind() == reflect.Pointer {
		return t.Elem()
	}

	return t
}

func (b *Bus) directSend(sub *Subscription, ev Event) {
	if sub.closed.Load() {
		return
	}

	select {
	case sub.ch <- ev:
	default:
	}
}

func (b *Bus) unsubscribe(sub *Subscription) {
	b.mu.Lock()
	defer b.mu.Unlock()

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

// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package bus

import (
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

type (
	TestEventA    struct{ BaseEvent }
	TestEventB    struct{ BaseEvent }
	TestEventData struct {
		BaseEvent
		Payload string
	}
)

func TestBus_SubscribeAndPublish(t *testing.T) {
	BaseEvent{}.isEvent() // no-op

	b := New()
	defer b.Close()

	sub := b.Subscribe(TestEventA{})

	// Test basic delivery
	b.Publish(TestEventA{})

	select {
	case ev := <-sub.C():
		assert.IsType(t, TestEventA{}, ev)
	case <-time.After(100 * time.Millisecond):
		t.Fatal("Timeout: Event not received")
	}
}

func TestBus_ResolveType(t *testing.T) {
	// Test pointer vs value resolution
	t1 := resolveType(TestEventA{})
	t2 := resolveType(&TestEventA{})
	assert.Equal(t, t1, t2)

	// Test nil safety in resolveType (internal check)
	assert.Nil(t, resolveType(nil))
}

func TestBus_PublishNil(t *testing.T) {
	b := New()
	defer b.Close()

	// Should not panic or hang
	assert.NotPanics(t, func() {
		b.Publish(nil)
	})
}

func TestBus_SubscribeDuplicates(t *testing.T) {
	b := New()
	defer b.Close()

	// Subscribing to same type twice in one call should only create one entry
	sub := b.Subscribe(TestEventA{}, TestEventA{})
	assert.Len(t, sub.types, 1)
}

func TestBus_SubscribeAll(t *testing.T) {
	b := New()
	defer b.Close()

	subAll := b.SubscribeAll()
	b.Publish(TestEventA{})
	b.Publish(TestEventB{})

	for i := range 2 {
		select {
		case <-subAll.C():
			// Success
		case <-time.After(100 * time.Millisecond):
			t.Fatalf("Timeout waiting for event %d in SubscribeAll", i)
		}
	}
}

func TestBus_Unsubscribe(t *testing.T) {
	b := New()
	sub := b.Subscribe(TestEventA{})

	// Ensure sub is in internal maps
	b.mu.RLock()
	assert.NotEmpty(t, b.subs[resolveType(TestEventA{})])
	b.mu.RUnlock()

	sub.Unsubscribe()

	// Verify maps are cleaned up
	b.mu.RLock()
	assert.Empty(t, b.subs)
	assert.Empty(t, b.all)
	b.mu.RUnlock()

	// Verify channel closed
	_, ok := <-sub.C()
	assert.False(t, ok, "Channel should be closed after unsubscribe")

	// Verify double unsubscribe is safe
	assert.NotPanics(t, func() {
		sub.Unsubscribe()
	})
}

func TestBus_UnsubscribeWhenBusClosed(t *testing.T) {
	b := New()
	sub := b.Subscribe(TestEventA{})
	_ = b.Close()

	// Should return early and not panic
	assert.NotPanics(t, func() {
		sub.Unsubscribe()
	})
}

func TestBus_Close(t *testing.T) {
	b := New()
	sub := b.Subscribe(TestEventA{})
	subAll := b.SubscribeAll()

	err := b.Close()
	assert.NoError(t, err)
	sub.Unsubscribe()

	// Test multiple Close calls
	err = b.Close()
	assert.NoError(t, err)

	// Verify channels are closed
	_, ok1 := <-sub.C()
	assert.False(t, ok1)

	_, ok2 := <-subAll.C()
	assert.False(t, ok2)

	// Verify internal state
	assert.True(t, b.closed)
	assert.Nil(t, b.subs)
	assert.Nil(t, b.all)
}

func TestBus_OperationOnClosedBus(t *testing.T) {
	b := New()
	_ = b.Close()

	// Subscribing to a closed bus should return a closed subscription
	sub := b.Subscribe(TestEventA{})
	_, ok := <-sub.C()
	assert.False(t, ok)

	subAll := b.SubscribeAll()
	_, okAll := <-subAll.C()
	assert.False(t, okAll)

	// Publishing on closed bus should do nothing
	assert.NotPanics(t, func() {
		b.Publish(TestEventA{})
	})
}

func TestBus_DropOnFullBuffer(t *testing.T) {
	b := New()
	defer b.Close()

	// Buffer for Subscribe is 128
	sub := b.Subscribe(TestEventA{})

	// Fill the buffer + 1
	for range 129 {
		b.Publish(TestEventA{})
	}

	// Drain the 128 events
	count := 0
	for range 128 {
		<-sub.C()

		count++
	}

	// The 129th event should have been dropped
	select {
	case <-sub.C():
		t.Error("Expected event to be dropped, but received it")
	default:
		// Correct behavior
	}

	assert.Equal(t, 128, count)
}

func TestBus_DirectSendToClosedSub(t *testing.T) {
	b := New()
	defer b.Close()

	sub := b.Subscribe(TestEventA{})
	sub.closed.Store(true) // Simulate internal closure

	// Should not block or panic
	assert.NotPanics(t, func() {
		b.directSend(sub, TestEventA{})
	})
}

func TestBus_ConcurrentUsage(t *testing.T) {
	b := New()

	var wg sync.WaitGroup

	publishers := 10
	subscribers := 10
	iterations := 50

	wg.Add(publishers + subscribers)

	// Concurrent Publishers
	for range publishers {
		go func() {
			defer wg.Done()

			for j := 0; j < iterations; j++ {
				b.Publish(TestEventData{Payload: "data"})
			}
		}()
	}

	// Concurrent Subscribers
	for range subscribers {
		go func() {
			defer wg.Done()

			sub := b.Subscribe(TestEventData{})
			defer sub.Unsubscribe()

			timeout := time.After(500 * time.Millisecond)

			received := 0
			for received < 10 { // Just try to catch some
				select {
				case <-sub.C():
					received++
				case <-timeout:
					return
				}
			}
		}()
	}

	// Chaos: randomly close bus during operations
	go func() {
		time.Sleep(10 * time.Millisecond)
		b.Close()
	}()

	wg.Wait()
}

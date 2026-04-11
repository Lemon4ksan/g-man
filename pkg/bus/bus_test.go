// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package bus

import (
	"sync"
	"testing"
	"time"
)

type TestEventA struct {
	BaseEvent
	Data string
}

type TestEventB struct {
	BaseEvent
	Value int
}

func TestBus_SubscribeAndPublish(t *testing.T) {
	b := NewBus()
	defer b.Close()

	subA := b.Subscribe(TestEventA{})
	subB := b.Subscribe(TestEventB{})

	b.Publish(TestEventA{Data: "hello"})
	b.Publish(TestEventB{Value: 42})

	select {
	case ev := <-subA.C():
		if ev.(TestEventA).Data != "hello" {
			t.Errorf("Unexpected event data in TestEventA")
		}
	case <-time.After(time.Second):
		t.Error("Did not receive TestEventA")
	}

	select {
	case ev := <-subB.C():
		if ev.(TestEventB).Value != 42 {
			t.Errorf("Unexpected event value in TestEventB")
		}
	case <-time.After(time.Second):
		t.Error("Did not receive TestEventB")
	}
}

func TestBus_PointerVsValueResolution(t *testing.T) {
	b := NewBus()
	defer b.Close()

	sub := b.Subscribe(TestEventA{})
	b.Publish(&TestEventA{Data: "pointer_data"})

	select {
	case ev := <-sub.C():
		ptr, ok := ev.(*TestEventA)
		if !ok {
			t.Fatalf("Expected *TestEventA, got %T", ev)
		}

		if ptr.Data != "pointer_data" {
			t.Errorf("Data mismatch")
		}

	case <-time.After(100 * time.Millisecond):
		t.Error("Pointer vs Value type resolution failed: Event lost!")
	}
}

func TestBus_SubscribeAll(t *testing.T) {
	b := NewBus()
	defer b.Close()

	subAll := b.SubscribeAll()

	b.Publish(TestEventA{})
	b.Publish(TestEventB{})

	count := 0
	timeout := time.After(time.Second)

	for count < 2 {
		select {
		case <-subAll.C():
			count++
		case <-timeout:
			t.Fatalf("SubscribeAll received only %d events out of 2", count)
		}
	}
}

func TestBus_FullBufferDropsEvent(t *testing.T) {
	b := NewBus()
	defer b.Close()

	sub := b.Subscribe(TestEventA{})

	for range 200 {
		b.Publish(TestEventA{Data: "spam"})
	}

	count := 0

	for {
		select {
		case <-sub.C():
			count++
		default:
			if count != 128 {
				t.Errorf("Expected exactly 128 buffered events, got %d", count)
			}

			return
		}
	}
}

func TestBus_Unsubscribe(t *testing.T) {
	b := NewBus()

	sub := b.Subscribe(TestEventA{})

	sub.Unsubscribe()
	b.Publish(TestEventA{})

	_, ok := <-sub.C()
	if ok {
		t.Error("Expected subscription channel to be closed after Unsubscribe")
	}
}

func TestBus_Close(t *testing.T) {
	b := NewBus()
	sub := b.Subscribe(TestEventA{})
	subAll := b.SubscribeAll()

	_ = b.Close()
	b.Publish(TestEventA{})

	if _, ok := <-sub.C(); ok {
		t.Error("Channel was not closed on Bus.Close()")
	}

	if _, ok := <-subAll.C(); ok {
		t.Error("SubscribeAll channel was not closed on Bus.Close()")
	}

	deadSub := b.Subscribe(TestEventA{})
	if _, ok := <-deadSub.C(); ok {
		t.Error("Expected newly created subscription on a closed bus to be pre-closed")
	}
}

func TestBus_ConcurrentAccess(t *testing.T) {
	b := NewBus()
	defer b.Close()

	var wg sync.WaitGroup

	for range 10 {
		wg.Go(func() {
			for range 5 {
				sub := b.Subscribe(TestEventA{})

				time.Sleep(2 * time.Millisecond)

				for len(sub.C()) > 0 {
					<-sub.C()
				}

				sub.Unsubscribe()
			}
		})
	}

	for range 5 {
		wg.Go(func() {
			for range 100 {
				b.Publish(TestEventA{})
				time.Sleep(1 * time.Millisecond)
			}
		})
	}

	wg.Wait()
}

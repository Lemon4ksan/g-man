// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package jobs

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestManager_AddAndResolve_Callback(t *testing.T) {
	m := NewManager[string](0)
	id := m.NextID()

	var gotResponse string
	var gotErr error
	var wg sync.WaitGroup
	wg.Add(1)

	err := m.Add(id, func(response string, err error) {
		gotResponse = response
		gotErr = err
		wg.Done()
	})

	if err != nil {
		t.Fatalf("Add failed: %v", err)
	}

	if m.Count() != 1 {
		t.Fatalf("expected 1 job, got %d", m.Count())
	}

	ok := m.Resolve(id, "success", nil)
	if !ok {
		t.Fatalf("Resolve returned false, expected true")
	}

	wg.Wait()

	if gotResponse != "success" {
		t.Errorf("expected response 'success', got '%s'", gotResponse)
	}
	if gotErr != nil {
		t.Errorf("expected no error, got %v", gotErr)
	}
	if m.Count() != 0 {
		t.Errorf("expected 0 jobs after resolve, got %d", m.Count())
	}
}

func TestManager_WaitFor(t *testing.T) {
	m := NewManager[string](0)
	id := m.NextID()

	err := m.Add(id, nil, WithWait[string]())
	if err != nil {
		t.Fatalf("Add failed: %v", err)
	}

	go func() {
		time.Sleep(50 * time.Millisecond)
		m.Resolve(id, "background_result", nil)
	}()

	res, err := m.WaitFor(context.Background(), id)
	if err != nil {
		t.Fatalf("WaitFor failed: %v", err)
	}

	if res != "background_result" {
		t.Errorf("expected 'background_result', got '%s'", res)
	}
}

func TestManager_WaitFor_Timeout(t *testing.T) {
	m := NewManager[string](0)
	id := m.NextID()

	err := m.Add(id, nil, WithWait[string](), WithTimeout[string](50*time.Millisecond))
	if err != nil {
		t.Fatalf("Add failed: %v", err)
	}

	_, err = m.WaitFor(context.Background(), id)

	if !errors.Is(err, ErrJobTimeout) {
		t.Errorf("expected ErrJobTimeout, got %v", err)
	}
	if m.Count() != 0 {
		t.Errorf("expected job to be removed after timeout")
	}
}

func TestManager_ContextCancellation(t *testing.T) {
	m := NewManager[string](0)
	id := m.NextID()

	ctx, cancel := context.WithCancel(context.Background())

	var gotErr error
	var wg sync.WaitGroup
	wg.Add(1)

	err := m.Add(id, func(_ string, err error) {
		gotErr = err
		wg.Done()
	}, WithContext[string](ctx))

	if err != nil {
		t.Fatalf("Add failed: %v", err)
	}

	cancel()
	wg.Wait()

	if !errors.Is(gotErr, ErrJobCancelled) {
		t.Errorf("expected ErrJobCancelled, got %v", gotErr)
	}
	if m.Count() != 0 {
		t.Errorf("expected 0 jobs, got %d", m.Count())
	}
}

func TestManager_CapacityLimit(t *testing.T) {
	m := NewManager[int](2)

	err1 := m.Add(1, nil)
	err2 := m.Add(2, nil)
	err3 := m.Add(3, nil)

	if err1 != nil || err2 != nil {
		t.Errorf("expected first 2 jobs to succeed")
	}
	if err3 == nil || !strings.Contains(err3.Error(), "capacity reached") {
		t.Errorf("expected capacity error, got %v", err3)
	}
}

func TestManager_DuplicateID(t *testing.T) {
	m := NewManager[int](0)

	_ = m.Add(1, nil)
	err := m.Add(1, nil)

	if !errors.Is(err, ErrJobDuplicate) {
		t.Errorf("expected ErrJobDuplicate, got %v", err)
	}
}

func TestManager_Close(t *testing.T) {
	m := NewManager[string](0)

	var wg sync.WaitGroup
	wg.Add(2)

	cb := func(_ string, err error) {
		if !errors.Is(err, ErrJobClosed) {
			t.Errorf("expected ErrJobClosed, got %v", err)
		}
		wg.Done()
	}

	_ = m.Add(1, cb)
	_ = m.Add(2, cb)

	err := m.Close()
	if err != nil {
		t.Fatalf("Close failed: %v", err)
	}

	err = m.Add(3, nil)
	if !errors.Is(err, ErrJobClosed) {
		t.Errorf("expected ErrJobClosed on Add after Close, got %v", err)
	}

	wg.Wait()

	if m.Count() != 0 {
		t.Errorf("expected 0 jobs after close, got %d", m.Count())
	}
}

func TestManager_ResolveMultipleTimes(t *testing.T) {
	m := NewManager[string](0)
	id := m.NextID()

	calls := 0
	var mu sync.Mutex
	var wg sync.WaitGroup
	wg.Add(1)

	_ = m.Add(id, func(res string, err error) {
		mu.Lock()
		calls++
		mu.Unlock()
		wg.Done()
	})

	go m.Resolve(id, "first", nil)
	go m.Resolve(id, "second", nil)

	wg.Wait()
	time.Sleep(10 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()
	if calls != 1 {
		t.Errorf("expected callback to be called exactly 1 time, got %d", calls)
	}
}

func TestManager_PanicInCallback(t *testing.T) {
	m := NewManager[string](0)
	id := m.NextID()

	_ = m.Add(id, func(res string, err error) {
		panic("test panic")
	})

	ok := m.Resolve(id, "success", nil)
	if !ok {
		t.Fatalf("Resolve failed")
	}

	time.Sleep(50 * time.Millisecond)
	// Successful recovery
}

func TestManager_WaitForWithoutWaitOption(t *testing.T) {
	m := NewManager[string](0)
	id := m.NextID()

	_ = m.Add(id, nil)

	_, err := m.WaitFor(context.Background(), id)
	if err == nil || err.Error() != "job was not created with WithWait option" {
		t.Errorf("expected error about missing WithWait option, got %v", err)
	}
}

func TestManager_WaitForContextTimeout(t *testing.T) {
	m := NewManager[string](0)
	id := m.NextID()

	_ = m.Add(id, nil, WithWait[string]())

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()

	_, err := m.WaitFor(ctx, id)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("expected context.DeadlineExceeded, got %v", err)
	}
}

// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package jobs provides a concurrent-safe mechanism for tracking asynchronous
// request-response cycles, commonly used in protocol implementations.
package jobs

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

var (
	// ErrJobTimeout is returned when a job exceeds its allowed execution time.
	ErrJobTimeout = errors.New("job: request timed out")

	// ErrJobClosed is returned when the manager is shutting down.
	ErrJobClosed = errors.New("job: manager closed")

	// ErrJobCancelled is returned when the associated context is cancelled.
	ErrJobCancelled = errors.New("job: context cancelled")

	// ErrJobDuplicate is returned when attempting to add a job ID that already exists.
	ErrJobDuplicate = errors.New("job: duplicate job ID")

	// ErrJobNotFound is returned when attempting to resolve a non-existent job.
	ErrJobNotFound = errors.New("job: not found")
)

// Callback defines the function signature for handling completed jobs.
type Callback[T any] func(response T, err error)

// Option configures a job when adding it to the manager.
type Option[T any] func(*config[T])

type config[T any] struct {
	timeout   time.Duration
	ctx       context.Context
	keepAlive bool
	wait      bool // Keep job after execution
}

func defaultConfig[T any]() config[T] {
	return config[T]{
		timeout: 30 * time.Second,
		ctx:     context.Background(),
	}
}

// entry represents the internal state of a tracked job.
type entry[T any] struct {
	callback Callback[T]
	waitCh   chan result[T] // Created only if WithWait is used

	// Cleanups
	timerStop func() bool // Stops the timeout timer
	ctxStop   func() bool // Stops the context watcher (Go 1.21+)
}

type result[T any] struct {
	val T
	err error
}

// Manager handles the lifecycle of asynchronous jobs.
// It maps request IDs to callbacks and handles timeouts and context cancellation.
type Manager[T any] struct {
	mu      sync.RWMutex
	jobs    map[uint64]*entry[T]
	counter atomic.Uint64
	closed  bool

	// capacity limits the number of concurrent jobs to prevent memory exhaustion.
	// 0 means unlimited.
	capacity int
}

// NewManager creates a new job manager.
// capacity: max concurrent jobs (0 for unlimited).
func NewManager[T any](capacity int) *Manager[T] {
	return &Manager[T]{
		jobs:     make(map[uint64]*entry[T]),
		capacity: capacity,
	}
}

// WithTimeout sets a timeout for the job.
func WithTimeout[T any](timeout time.Duration) Option[T] {
	return func(c *config[T]) {
		c.timeout = timeout
	}
}

// WithContext sets a context for the job.
func WithContext[T any](ctx context.Context) Option[T] {
	return func(c *config[T]) {
		c.ctx = ctx
	}
}

// WithKeepAlive keeps the job in the map after execution (for streaming responses).
func WithKeepAlive[T any](keepAlive bool) Option[T] {
	return func(c *config[T]) {
		c.keepAlive = keepAlive
	}
}

// WithWait tells the manager that a waiting channel should be created for this job.
func WithWait[T any]() Option[T] {
	return func(c *config[T]) { c.wait = true }
}

// NextID generates a monotonic unique ID for a new job.
func (m *Manager[T]) NextID() uint64 {
	return m.counter.Add(1)
}

// Add tracks a new job with the specified ID.
// If the ID already exists or the manager is closed, an error is returned.
func (m *Manager[T]) Add(id uint64, cb Callback[T], opts ...Option[T]) error {
	cfg := defaultConfig[T]()
	for _, opt := range opts {
		opt(&cfg)
	}

	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return ErrJobClosed
	}

	if m.capacity > 0 && len(m.jobs) >= m.capacity {
		m.mu.Unlock()
		return fmt.Errorf("job manager capacity reached (%d)", m.capacity)
	}

	if _, exists := m.jobs[id]; exists {
		m.mu.Unlock()
		return ErrJobDuplicate
	}

	e := &entry[T]{
		callback: cb,
	}

	if cfg.wait {
		e.waitCh = make(chan result[T], 1)
	}

	// Setup Timeout
	if cfg.timeout > 0 {
		timer := time.AfterFunc(cfg.timeout, func() {
			m.Resolve(id, *new(T), ErrJobTimeout)
		})
		e.timerStop = timer.Stop
	}

	// Setup Context Cancellation (Leak-free)
	if cfg.ctx != nil {
		// context.AfterFunc (Go 1.21+) waits for Done channel efficiently
		// and allows us to stop waiting if the job finishes successfully first.
		stop := context.AfterFunc(cfg.ctx, func() {
			m.Resolve(id, *new(T), ErrJobCancelled)
		})
		e.ctxStop = stop
	}

	m.jobs[id] = e
	m.mu.Unlock()

	return nil
}

// Resolve completes a job with a response or an error.
// It returns true if the job was found and resolved, false otherwise.
// It is safe to call Resolve multiple times, but only the first one wins.
func (m *Manager[T]) Resolve(id uint64, response T, err error) bool {
	m.mu.Lock()
	e, ok := m.jobs[id]
	if !ok {
		m.mu.Unlock()
		return false
	}

	// Remove job immediately to free map slot
	delete(m.jobs, id)
	m.mu.Unlock()

	// Clean up resources
	if e.timerStop != nil {
		e.timerStop()
	}
	if e.ctxStop != nil {
		e.ctxStop()
	}

	// 1. Notify Wait Channel (synchronous waiters)
	if e.waitCh != nil {
		e.waitCh <- result[T]{val: response, err: err}
		close(e.waitCh)
	}

	// 2. Notify Callback (asynchronous)
	if e.callback != nil {
		// Run callback in a separate goroutine to avoid blocking the caller of Resolve
		// and to isolate panics.
		go func() {
			defer func() { _ = recover() }() // Basic panic safety
			e.callback(response, err)
		}()
	}

	return true
}

// WaitFor blocks until the specific job is resolved or the context expires.
// Note: The job must have been created with the WithWait() option.
func (m *Manager[T]) WaitFor(ctx context.Context, id uint64) (T, error) {
	m.mu.RLock()
	e, ok := m.jobs[id]
	m.mu.RUnlock()

	if !ok {
		return *new(T), ErrJobNotFound
	}

	if e.waitCh == nil {
		return *new(T), errors.New("job was not created with WithWait option")
	}

	select {
	case res, ok := <-e.waitCh:
		if !ok {
			return *new(T), ErrJobClosed
		}
		return res.val, res.err
	case <-ctx.Done():
		return *new(T), ctx.Err()
	}
}

// Close cancels all pending jobs and shuts down the manager.
func (m *Manager[T]) Close() error {
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return nil
	}
	m.closed = true
	pending := m.jobs
	m.jobs = nil // Clear reference
	m.mu.Unlock()

	// Cancel all pending jobs
	for _, e := range pending {
		if e.timerStop != nil {
			e.timerStop()
		}
		if e.ctxStop != nil {
			e.ctxStop()
		}

		if e.waitCh != nil {
			e.waitCh <- result[T]{err: ErrJobClosed}
			close(e.waitCh)
		}

		if e.callback != nil {
			go e.callback(*new(T), ErrJobClosed)
		}
	}

	return nil
}

// Count returns the number of currently pending jobs.
func (m *Manager[T]) Count() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.jobs)
}

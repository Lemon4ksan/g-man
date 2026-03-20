// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package jobs provides a concurrent-safe mechanism for tracking asynchronous
// request-response cycles. It is particularly useful for building protocol implementations
// where a request is sent and a response is expected later with a matching correlation ID.
//
// Example usage (Callback style):
//
//	mgr := jobs.NewManager[string](100)
//	id := mgr.NextID()
//
//	mgr.Add(id, func(res string, err error) {
//	    if err != nil {
//	        fmt.Printf("Job %d failed: %v\n", id, err)
//	        return
//	    }
//	    fmt.Printf("Job %d received: %s\n", id, res)
//	})
//
//	// Simulating response from a remote server
//	mgr.Resolve(id, "Hello World", nil)
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
	// ErrJobTimeout is returned when a job exceeds its allowed execution time
	// defined by WithTimeout.
	ErrJobTimeout = errors.New("job: request timed out")

	// ErrJobClosed is returned when the manager is shutting down and all
	// pending jobs are being cancelled.
	ErrJobClosed = errors.New("job: manager closed")

	// ErrJobCancelled is returned when the context associated with the job
	// is cancelled or expires.
	ErrJobCancelled = errors.New("job: context cancelled")

	// ErrJobDuplicate is returned when attempting to add a job ID that is
	// already being tracked by the manager.
	ErrJobDuplicate = errors.New("job: duplicate job ID")

	// ErrJobNotFound is returned when attempting to resolve or wait for
	// a job ID that does not exist in the manager's registry.
	ErrJobNotFound = errors.New("job: not found")
)

// Callback defines the function signature for handling completed jobs.
// The response contains the result value, and err contains any error that
// occurred during job execution or management (timeout, cancellation, etc.).
type Callback[T any] func(response T, err error)

// Option configures a job's behavior such as timeout, context, and persistence.
type Option[T any] func(*config[T])

type config[T any] struct {
	timeout   time.Duration
	ctx       context.Context
	keepAlive bool
	wait      bool
}

func defaultConfig[T any]() config[T] {
	return config[T]{
		timeout: 30 * time.Second,
		ctx:     context.Background(),
	}
}

// entry represents the internal state and cleanup logic of a tracked job.
type entry[T any] struct {
	callback  Callback[T]
	waitCh    chan result[T] // Created only if WithWait is used
	keepAlive bool           // Keep job after execution

	// Cleanups
	timerStop func() bool // Stops the timeout timer
	ctxStop   func() bool // Stops the context watcher
}

type result[T any] struct {
	val T
	err error
}

// Manager handles the lifecycle of asynchronous jobs.
// It maps unique IDs (correlation IDs) to callbacks and handles
// automatic cleanup via timeouts and context cancellation.
type Manager[T any] struct {
	mu      sync.RWMutex
	jobs    map[uint64]*entry[T]
	counter atomic.Uint64
	closed  bool

	// capacity limits the number of concurrent jobs to prevent memory exhaustion.
	// 0 means unlimited.
	capacity int
}

// NewManager creates a new job manager instance.
// The capacity parameter limits the maximum number of concurrent jobs.
// Set capacity to 0 for unlimited jobs.
func NewManager[T any](capacity int) *Manager[T] {
	return &Manager[T]{
		jobs:     make(map[uint64]*entry[T]),
		capacity: capacity,
	}
}

// WithTimeout sets a maximum duration the job is allowed to remain pending.
// If the timeout is reached, the job is resolved with [ErrJobTimeout].
func WithTimeout[T any](timeout time.Duration) Option[T] {
	return func(c *config[T]) {
		c.timeout = timeout
	}
}

// WithContext associates a context.Context with the job.
// If the context is cancelled, the job is resolved with [ErrJobCancelled].
func WithContext[T any](ctx context.Context) Option[T] {
	return func(c *config[T]) {
		c.ctx = ctx
	}
}

// WithKeepAlive indicates if the job should persist after the first resolution.
// Useful for streaming or multi-part responses.
func WithKeepAlive[T any](keepAlive bool) Option[T] {
	return func(c *config[T]) {
		c.keepAlive = keepAlive
	}
}

// WithWait enables synchronous waiting for this job using the WaitFor method.
// Without this option, calling WaitFor on the job ID will return an error.
func WithWait[T any]() Option[T] {
	return func(c *config[T]) { c.wait = true }
}

// NextID generates a unique, monotonically increasing ID for a new job.
// This ID should be sent to the remote system to be returned in the response.
func (m *Manager[T]) NextID() uint64 {
	return m.counter.Add(1)
}

// Add registers a new job for tracking.
// If the manager is closed, capacity is reached, or the ID is already in use,
// it returns an error.
func (m *Manager[T]) Add(id uint64, cb Callback[T], opts ...Option[T]) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	cfg := defaultConfig[T]()
	for _, opt := range opts {
		opt(&cfg)
	}

	if m.closed {
		return ErrJobClosed
	}

	if m.capacity > 0 && len(m.jobs) >= m.capacity {
		return fmt.Errorf("job manager capacity reached (%d)", m.capacity)
	}

	if _, exists := m.jobs[id]; exists {
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

	// Setup Context Cancellation (Leak-free using context.AfterFunc)
	if cfg.ctx != nil && cfg.ctx != context.Background() {
		stop := context.AfterFunc(cfg.ctx, func() {
			m.Resolve(id, *new(T), ErrJobCancelled)
		})
		e.ctxStop = stop
	}

	e.keepAlive = cfg.keepAlive
	m.jobs[id] = e

	return nil
}

// Resolve marks a job as complete by providing a response or an error.
// The internal state is cleaned up immediately, and the associated callback
// is executed in a new goroutine to prevent deadlocks.
// Returns true if the job was found and resolved, false if it didn't exist
// (e.g., already timed out or resolved).
func (m *Manager[T]) Resolve(id uint64, response T, err error) bool {
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return false
	}

	e, ok := m.jobs[id]
	if !ok {
		m.mu.Unlock()
		return false
	}

	// Remove job immediately to free map slot
	if !e.keepAlive {
		delete(m.jobs, id)
	}
	m.mu.Unlock()

	// Clean up resources (timers and context watchers)
	if e.timerStop != nil {
		e.timerStop()
	}
	if e.ctxStop != nil {
		e.ctxStop()
	}

	// Unblock WaitFor calls
	if e.waitCh != nil {
		e.waitCh <- result[T]{val: response, err: err}
		close(e.waitCh)
	}

	// Trigger callback asynchronously
	if e.callback != nil {
		go func() {
			defer func() { _ = recover() }()
			e.callback(response, err)
		}()
	}

	return true
}

// Remove removes the specific job without resolving it.
// Can be used to clear jobs with keepAlive = true.
func (m *Manager[T]) Remove(id uint64) bool {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.closed {
		return false
	}

	_, ok := m.jobs[id]
	if !ok {
		return false
	}

	delete(m.jobs, id)

	return true
}

// WaitFor blocks the current goroutine until the specific job is resolved,
// the provided ctx is cancelled, or the manager is closed.
//
// IMPORTANT: The job must have been created with the WithWait() option.
//
// Example:
//
//	id := mgr.NextID()
//	mgr.Add(id, nil, jobs.WithWait[string](), jobs.WithTimeout[string](time.Second))
//	// ... send request to network ...
//	res, err := mgr.WaitFor(context.Background(), id)
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

// Close shuts down the manager and cancels all currently pending jobs
// with ErrJobClosed. Once closed, no new jobs can be added.
func (m *Manager[T]) Close() error {
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return nil
	}

	m.closed = true
	pending := m.jobs
	m.jobs = nil
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
			e.callback(*new(T), ErrJobClosed)
		}
	}

	return nil
}

// Count returns the number of currently active jobs being tracked.
func (m *Manager[T]) Count() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.jobs)
}

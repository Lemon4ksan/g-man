// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package jobs

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"strconv"
	"sync"
	"sync/atomic"
	"time"
)

var (
	// ErrJobTimeout is returned when a job exceeds its allowed execution time
	// defined by [WithTimeout].
	ErrJobTimeout = errors.New("job: request timed out")

	// ErrJobClosed is returned when the manager is shutting down and all
	// pending jobs are being canceled.
	ErrJobClosed = errors.New("job: manager closed")

	// ErrJobCancelled is returned when the context associated with the job
	// is canceled or expires.
	ErrJobCancelled = errors.New("job: context cancelled")

	// ErrJobDuplicate is returned when attempting to add a job ID that is
	// already being tracked by the manager.
	ErrJobDuplicate = errors.New("job: duplicate job ID")

	// ErrJobNotFound is returned when attempting to resolve or wait for
	// a job ID that does not exist in the manager's registry.
	ErrJobNotFound = errors.New("job: not found")

	// ErrWaitFor is returned when the WaitFor is called on a job id with no [WithWait] option.
	ErrWaitFor = errors.New("job was not created with WithWait option")
)

// Callback defines the function signature for handling completed jobs.
// The ctx parameter propagates context from the resolution stage.
// The response contains the result value, and err contains any error that
// occurred during job execution or management (timeout, cancellation, etc.).
type Callback[T any] func(ctx context.Context, response T, err error)

// Option configures a job's behavior such as timeout, context, and persistence.
type Option[T any] func(*config[T])

// CallbackStrategy defines how job callbacks are executed.
type CallbackStrategy func(fn func())

var (
	// AsyncStrategy executes the callback in a new goroutine (default behavior).
	AsyncStrategy CallbackStrategy = func(fn func()) { go fn() }

	// SyncStrategy executes the callback synchronously in the current goroutine.
	SyncStrategy CallbackStrategy = func(fn func()) { fn() }
)

// Entry represents the internal state and cleanup logic of a tracked job.
// It holds the callback, context, sync channels, and cleanup functions.
type Entry[T any] struct {
	callback  Callback[T]
	waitCh    chan Result[T] // Created only if WithWait is used
	keepAlive bool           // Keep job after execution
	strategy  CallbackStrategy
	ctx       context.Context // Store the job's context here

	// Cleanups
	timerStop func() bool // Stops the timeout timer
	ctxStop   func() bool // Stops the context watcher
}

// Result holds the result of a job execution.
type Result[T any] struct {
	val T
	err error
}

// Store defines the storage interface for managing jobs.
// It allows users to plug in custom backends (e.g., in-memory, Redis, DB).
// Custom implementations of the interface must be thread-safe.
type Store[K comparable, T any] interface {
	// Add registers a new job entry.
	Add(ctx context.Context, id K, e *Entry[T]) error

	// Get retrieves a job entry by ID.
	Get(ctx context.Context, id K) (*Entry[T], bool, error)

	// Delete removes a job entry by ID.
	Delete(ctx context.Context, id K) (bool, error)

	// Len returns the number of active jobs.
	Len(ctx context.Context) (int, error)

	// GetAll retrieves all active job entries.
	GetAll(ctx context.Context) (map[K]*Entry[T], error)
}

// Manager handles the lifecycle of asynchronous jobs.
// It maps unique IDs (correlation IDs) to callbacks and handles
// automatic cleanup via timeouts and context cancellation.
//
// Create new instances of the manager using the [NewManager] constructor.
// The manager is safe for concurrent use and manages memory allocation
// efficiently using an internal pool of job entries.
type Manager[K comparable, T any] struct {
	mu       sync.RWMutex
	store    Store[K, T]
	counter  atomic.Uint64
	closed   bool
	capacity int
	strategy CallbackStrategy

	entryPool sync.Pool
}

// NewManager creates a new job manager instance.
//
// The capacity parameter limits the maximum number of concurrent jobs
// to protect against memory exhaustion. Set capacity to 0 for unlimited jobs.
func NewManager[K comparable, T any](capacity int) *Manager[K, T] {
	m := &Manager[K, T]{
		capacity: capacity,
		store:    newMemoryStore[K, T](),
	}
	m.entryPool.New = func() any {
		return new(Entry[T])
	}

	return m
}

// NewManagerWithStore creates a new job manager instance with a custom store.
//
// The capacity parameter limits the maximum number of concurrent jobs
// to protect against memory exhaustion. Set capacity to 0 for unlimited jobs.
func NewManagerWithStore[K comparable, T any](capacity int, store Store[K, T]) *Manager[K, T] {
	m := &Manager[K, T]{
		capacity: capacity,
		store:    store,
	}
	m.entryPool.New = func() any {
		return new(Entry[T])
	}

	return m
}

// SetStore updates the store used by the manager.
func (m *Manager[K, T]) SetStore(store Store[K, T]) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if store != nil {
		m.store = store
	}
}

// SetCallbackStrategy updates the callback execution strategy used by the manager.
func (m *Manager[K, T]) SetCallbackStrategy(strategy CallbackStrategy) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if strategy != nil {
		m.strategy = strategy
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
// If the context is canceled, the job is resolved with [ErrJobCancelled].
func WithContext[T any](ctx context.Context) Option[T] {
	return func(c *config[T]) {
		c.ctx = ctx
	}
}

// WithKeepAlive indicates if the job should persist after the first resolution.
// Useful for streaming or multipart responses.
func WithKeepAlive[T any](keepAlive bool) Option[T] {
	return func(c *config[T]) {
		c.keepAlive = keepAlive
	}
}

// WithWait enables synchronous waiting for this job using the [Manager.WaitFor] method.
// Without this option, calling WaitFor on the job ID will return an [ErrWaitFor] error.
func WithWait[T any]() Option[T] {
	return func(c *config[T]) { c.wait = true }
}

// WithCallbackStrategy sets a specific callback execution strategy for this job.
func WithCallbackStrategy[T any](strategy CallbackStrategy) Option[T] {
	return func(c *config[T]) {
		c.strategy = strategy
	}
}

// NextID generates a unique, monotonically increasing ID for a new job.
// This ID should be sent to the remote system to be returned in the response.
//
// If the key type K is not supported (not an integer or string type), it returns
// the zero value of K.
func (m *Manager[K, T]) NextID() K {
	val := m.counter.Add(1)

	var zero K
	switch any(zero).(type) {
	case uint64:
		return any(val).(K)
	case int64:
		return any(int64(val)).(K)
	case uint32:
		return any(uint32(val)).(K)
	case int32:
		return any(int32(val)).(K)
	case uint:
		return any(uint(val)).(K)
	case int:
		return any(int(val)).(K)
	case string:
		return any(strconv.FormatUint(val, 10)).(K)
	default:
		return zero
	}
}

// Add registers a new job for tracking.
//
// The cb argument can be nil if you plan to wait for the job synchronously
// using the [Manager.WaitFor] method. If you pass nil, make sure to configure
// the job with the [WithWait] option.
//
// It returns [ErrJobClosed] if the manager is already closed, or [ErrJobDuplicate]
// if the provided id is already registered. If a capacity limit is configured
// and reached, it returns an error with the capacity limit details.
func (m *Manager[K, T]) Add(id K, cb Callback[T], opts ...Option[T]) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	cfg := defaultConfig[T]()
	for _, opt := range opts {
		opt(&cfg)
	}

	if m.closed {
		return ErrJobClosed
	}

	storeLen, err := m.store.Len(cfg.ctx)
	if err != nil {
		return fmt.Errorf("store len: %w", err)
	}

	if m.capacity > 0 && storeLen >= m.capacity {
		return fmt.Errorf("job manager capacity reached (%d)", m.capacity)
	}

	_, exists, err := m.store.Get(cfg.ctx, id)
	if err != nil {
		return fmt.Errorf("store get: %w", err)
	}

	if exists {
		return ErrJobDuplicate
	}

	e := m.entryPool.Get().(*Entry[T])
	e.callback = cb
	e.keepAlive = cfg.keepAlive
	e.waitCh = nil
	e.timerStop = nil
	e.ctxStop = nil
	e.strategy = cfg.strategy
	e.ctx = cfg.ctx

	if cfg.wait {
		e.waitCh = make(chan Result[T], 1)
	}

	if cfg.timeout > 0 {
		timer := time.AfterFunc(cfg.timeout, func() {
			m.Resolve(id, *new(T), ErrJobTimeout)
		})
		e.timerStop = timer.Stop
	}

	if cfg.ctx != nil && cfg.ctx != context.Background() {
		stop := context.AfterFunc(cfg.ctx, func() {
			m.Resolve(id, *new(T), ErrJobCancelled)
		})
		e.ctxStop = stop
	}

	if err := m.store.Add(cfg.ctx, id, e); err != nil {
		if e.timerStop != nil {
			e.timerStop()
		}

		if e.ctxStop != nil {
			e.ctxStop()
		}

		m.entryPool.Put(e)

		return fmt.Errorf("store add: %w", err)
	}

	return nil
}

// Resolve marks a job as complete by providing a response or an error.
//
// The internal state is cleaned up immediately unless the job was registered
// with the [WithKeepAlive] option. Any goroutine currently blocked in the
// [Manager.WaitFor] call for this job ID will be unblocked and will receive
// the response and error.
//
// If a callback was registered, it is executed according to the configured
// strategy (defaulting to asynchronously in a new goroutine) to prevent
// deadlocks in the caller thread.
//
// It returns true if the job was found and successfully resolved, or false if
// the job did not exist (e.g. it had already timed out or been resolved).
func (m *Manager[K, T]) Resolve(id K, response T, err error) bool {
	return m.ResolveContext(context.Background(), id, response, err)
}

// ResolveContext marks a job as complete by providing a response or an error using a specific context.
func (m *Manager[K, T]) ResolveContext(ctx context.Context, id K, response T, err error) bool {
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return false
	}

	e, ok, getErr := m.store.Get(ctx, id)
	if getErr != nil || !ok {
		m.mu.Unlock()
		return false
	}

	// Remove job immediately to free map slot
	if !e.keepAlive {
		_, _ = m.store.Delete(ctx, id)
	}

	wCh := e.waitCh
	e.waitCh = nil

	cb := e.callback

	m.mu.Unlock()

	// Clean up resources (timers and context watchers)
	if e.timerStop != nil {
		e.timerStop()
	}

	if e.ctxStop != nil {
		e.ctxStop()
	}

	// Unblock WaitFor calls
	if wCh != nil {
		wCh <- Result[T]{val: response, err: err}

		close(wCh)
	}

	// Trigger callback
	if cb != nil {
		cbCtx := ctx
		if cbCtx == nil {
			cbCtx = context.Background()
		}

		if e.ctx != nil && (cbCtx == nil || cbCtx == context.Background()) {
			cbCtx = e.ctx
		}

		strategy := e.strategy
		if strategy == nil {
			strategy = m.strategy
		}

		if strategy == nil {
			strategy = AsyncStrategy
		}

		strategy(func() {
			defer func() { _ = recover() }()

			cb(cbCtx, response, err)
		})
	}

	if !e.keepAlive {
		e.callback = nil
		e.timerStop = nil
		e.ctxStop = nil
		e.ctx = nil
		m.entryPool.Put(e)
	}

	return true
}

// Remove removes the specific job without resolving it or invoking its callback.
//
// This method can be used to manually clean up jobs that were registered
// with the [WithKeepAlive] option. Any blocked [Manager.WaitFor] calls for
// this job ID will be closed immediately without a result.
//
// It returns true if the job was found and removed, or false otherwise.
func (m *Manager[K, T]) Remove(id K) bool {
	return m.RemoveContext(context.Background(), id)
}

// RemoveContext removes the specific job without resolving it or invoking its callback using a specific context.
func (m *Manager[K, T]) RemoveContext(ctx context.Context, id K) bool {
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return false
	}

	e, ok, getErr := m.store.Get(ctx, id)
	if getErr != nil || !ok {
		m.mu.Unlock()
		return false
	}

	_, _ = m.store.Delete(ctx, id)
	m.mu.Unlock()

	if e.timerStop != nil {
		e.timerStop()
	}

	if e.ctxStop != nil {
		e.ctxStop()
	}

	if e.waitCh != nil {
		close(e.waitCh)
	}

	e.callback = nil
	e.waitCh = nil
	e.timerStop = nil
	e.ctxStop = nil
	e.ctx = nil
	m.entryPool.Put(e)

	return true
}

// WaitFor blocks the current goroutine until the specific job is resolved,
// the provided ctx is canceled, or the manager is closed.
//
// It returns [ErrWaitFor] if the job was registered without the [WithWait] option,
// or [ErrJobNotFound] if the job does not exist. If the context is canceled
// before the job is resolved, it returns the context error. If the manager
// is closed while waiting, it returns [ErrJobClosed].
func (m *Manager[K, T]) WaitFor(ctx context.Context, id K) (T, error) {
	m.mu.RLock()

	e, ok, err := m.store.Get(ctx, id)
	if err != nil {
		m.mu.RUnlock()
		return *new(T), err
	}

	var wCh chan Result[T]
	if ok {
		wCh = e.waitCh
	}

	m.mu.RUnlock()

	if !ok {
		return *new(T), ErrJobNotFound
	}

	if wCh == nil {
		return *new(T), ErrWaitFor
	}

	select {
	case res, ok := <-wCh:
		if !ok {
			return *new(T), ErrJobClosed
		}

		return res.val, res.err

	case <-ctx.Done():
		return *new(T), ctx.Err()
	}
}

// CancelAll cancels all the currently pending jobs.
// All active job callbacks are executed, and any blocked
// [Manager.WaitFor] calls are unblocked, receiving the provided err value.
func (m *Manager[K, T]) CancelAll(err error) {
	m.CancelAllContext(context.Background(), err)
}

// CancelAllContext cancels all the currently pending jobs using a specific context.
func (m *Manager[K, T]) CancelAllContext(ctx context.Context, err error) {
	m.mu.Lock()

	pending, getErr := m.store.GetAll(ctx)
	if getErr == nil {
		for id := range pending {
			_, _ = m.store.Delete(ctx, id)
		}
	}

	m.mu.Unlock()

	if getErr == nil {
		m.closePending(pending, err)
	}
}

// Close shuts down the manager and cancels all currently pending jobs with [ErrJobClosed].
// Once the manager is closed, no new jobs can be added via the [Manager.Add] method.
func (m *Manager[K, T]) Close() error {
	m.CancelAll(ErrJobClosed)

	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return nil
	}

	m.closed = true

	pending, getErr := m.store.GetAll(context.Background())
	if getErr == nil {
		for id := range pending {
			_, _ = m.store.Delete(context.Background(), id)
		}
	}

	m.mu.Unlock()

	if getErr == nil {
		m.closePending(pending, ErrJobClosed)
	}

	return nil
}

// Count returns the number of currently active jobs being tracked.
func (m *Manager[K, T]) Count() int {
	m.mu.RLock()
	defer m.mu.RUnlock()

	c, _ := m.store.Len(context.Background())

	return c
}

func (m *Manager[K, T]) closePending(pending map[K]*Entry[T], err error) {
	for _, e := range pending {
		if e.timerStop != nil {
			e.timerStop()
		}

		if e.ctxStop != nil {
			e.ctxStop()
		}

		if e.waitCh != nil {
			close(e.waitCh)
		}

		cb := e.callback
		if cb != nil {
			cbCtx := e.ctx
			if cbCtx == nil {
				cbCtx = context.Background()
			}

			strategy := e.strategy
			if strategy == nil {
				strategy = m.strategy
			}

			if strategy == nil {
				strategy = AsyncStrategy
			}

			strategy(func() {
				defer func() { _ = recover() }()

				cb(cbCtx, *new(T), err)
			})
		}

		e.callback = nil
		e.waitCh = nil
		e.timerStop = nil
		e.ctxStop = nil
		e.ctx = nil
		m.entryPool.Put(e)
	}
}

type config[T any] struct {
	timeout   time.Duration
	ctx       context.Context
	keepAlive bool
	wait      bool
	strategy  CallbackStrategy
}

func defaultConfig[T any]() config[T] {
	return config[T]{
		timeout: 30 * time.Second,
		ctx:     context.Background(),
	}
}

type memoryStore[K comparable, T any] struct {
	mu   sync.RWMutex
	jobs map[K]*Entry[T]
}

func newMemoryStore[K comparable, T any]() *memoryStore[K, T] {
	return &memoryStore[K, T]{
		jobs: make(map[K]*Entry[T]),
	}
}

func (s *memoryStore[K, T]) Add(ctx context.Context, id K, e *Entry[T]) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.jobs[id] = e

	return nil
}

func (s *memoryStore[K, T]) Get(ctx context.Context, id K) (*Entry[T], bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	e, ok := s.jobs[id]

	return e, ok, nil
}

func (s *memoryStore[K, T]) Delete(ctx context.Context, id K) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	_, ok := s.jobs[id]
	if ok {
		delete(s.jobs, id)
	}

	return ok, nil
}

func (s *memoryStore[K, T]) Len(ctx context.Context) (int, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.jobs), nil
}

func (s *memoryStore[K, T]) GetAll(ctx context.Context) (map[K]*Entry[T], error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	res := make(map[K]*Entry[T], len(s.jobs))
	maps.Copy(res, s.jobs)

	return res, nil
}

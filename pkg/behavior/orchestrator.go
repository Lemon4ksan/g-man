// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package behavior provides the orchestrator for managing multiple behaviors.
package behavior

import (
	"context"
	"errors"
	"sync"

	"github.com/lemon4ksan/g-man/pkg/bus"
	"github.com/lemon4ksan/g-man/pkg/log"
)

// Behavior represents a modular task or strategy that the orchestrator can run.
type Behavior interface {
	// Name returns the unique name of the behavior.
	Name() string
	// Run starts the behavior's main loop. It should block until the context is canceled
	// or an unrecoverable error occurs.
	Run(ctx context.Context) error
}

// Option is an option for the orchestrator.
type Option bus.Option[*Orchestrator]

// Orchestrator manages the lifecycle of multiple behaviors.
type Orchestrator struct {
	logger    log.Logger
	behaviors []Behavior
	mu        sync.RWMutex
	running   bool
	cancel    context.CancelFunc
	wg        sync.WaitGroup
	bus       *bus.Bus
}

// NewOrchestrator creates a new orchestrator.
func NewOrchestrator(logger log.Logger, bus *bus.Bus) *Orchestrator {
	return &Orchestrator{
		logger:    logger.With(log.Module("orchestrator")),
		behaviors: make([]Behavior, 0),
		bus:       bus,
	}
}

// Logger returns the logger of the orchestrator.
func (o *Orchestrator) Logger() log.Logger {
	return o.logger
}

// Bus returns the bus of the orchestrator.
func (o *Orchestrator) Bus() *bus.Bus {
	return o.bus
}

// Install applies the given options to the orchestrator.
func (o *Orchestrator) Install(opt ...Option) {
	for _, opt := range opt {
		opt(o)
	}
}

// Register adds a behavior to the orchestrator.
func (o *Orchestrator) Register(b Behavior) {
	o.mu.Lock()
	defer o.mu.Unlock()

	for _, existing := range o.behaviors {
		if existing.Name() == b.Name() {
			o.logger.Warn("Behavior already registered, skipping", log.String("name", b.Name()))
			return
		}
	}

	o.behaviors = append(o.behaviors, b)
}

// Start starts all registered behaviors in separate goroutines.
func (o *Orchestrator) Start(ctx context.Context) error {
	o.mu.Lock()
	defer o.mu.Unlock()

	if o.running {
		return errors.New("orchestrator is already running")
	}

	o.running = true
	runCtx, cancel := context.WithCancel(ctx)
	o.cancel = cancel

	for _, b := range o.behaviors {
		o.wg.Add(1)

		go func(beh Behavior) {
			defer o.wg.Done()

			o.logger.Info("Starting behavior", log.String("name", beh.Name()))

			if err := beh.Run(runCtx); err != nil {
				o.logger.Error("Behavior failed", log.String("name", beh.Name()), log.Err(err))
			} else {
				o.logger.Info("Behavior stopped", log.String("name", beh.Name()))
			}
		}(b)
	}

	return nil
}

// Stop stops all running behaviors and waits for them to finish.
func (o *Orchestrator) Stop() {
	o.mu.Lock()
	defer o.mu.Unlock()

	if !o.running {
		return
	}

	o.cancel()
	o.wg.Wait()
	o.running = false
}

// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package behavior provides the orchestrator for managing multiple behaviors.
package behavior

import (
	"context"

	"github.com/lemon4ksan/miyako/bus"
	"github.com/lemon4ksan/miyako/lifecycle"
	"github.com/lemon4ksan/miyako/log"

	"github.com/lemon4ksan/g-man/pkg/steam"
	"github.com/lemon4ksan/g-man/pkg/steam/module"
)

// WithModule registers behavior orchestrator module in the client.
func WithModule() steam.Option {
	return func(c *steam.Client) {
		c.RegisterModule(NewModule())
	}
}

// From returns the orchestrator module from the client.
func From(c *steam.Client) *Orchestrator {
	return steam.GetModule[*Orchestrator](c)
}

// Orchestrator wraps miyako's [lifecycle.BehaviorRunner] and acts as a Steam client [module.Module].
// It stores a shared bus and logger so that registered behaviors can access them.
type Orchestrator struct {
	*lifecycle.BehaviorRunner
	bus    *bus.Bus
	logger log.Logger
}

// NewOrchestrator creates a new orchestrator with the given bus and logger.
func NewOrchestrator(b *bus.Bus, logger log.Logger, opts ...lifecycle.Option) *Orchestrator {
	return &Orchestrator{
		BehaviorRunner: lifecycle.NewBehaviorRunner(
			append([]lifecycle.Option{lifecycle.WithLogger(logger)}, opts...)...),
		bus:    b,
		logger: logger,
	}
}

// NewModule returns an uninitialized orchestrator intended to be registered as a [module.Module].
func NewModule() *Orchestrator {
	return &Orchestrator{}
}

// Name returns the static identifier for the behavior module.
func (o *Orchestrator) Name() string { return "behavior" }

// Init configures the orchestrator using the provided Steam client context.
func (o *Orchestrator) Init(init module.InitContext) error {
	o.bus = init.Bus()
	o.logger = init.Logger().With(log.Module("behavior"))
	o.BehaviorRunner = lifecycle.NewBehaviorRunner(lifecycle.WithLogger(o.logger))

	return nil
}

// Start launches the registered behaviors.
func (o *Orchestrator) Start(ctx context.Context) error {
	return o.BehaviorRunner.Start(ctx)
}

// Close gracefully terminates all running behaviors.
func (o *Orchestrator) Close() error {
	if o.BehaviorRunner != nil {
		o.Stop()
	}

	return nil
}

// Bus returns the shared event bus.
func (o *Orchestrator) Bus() *bus.Bus { return o.bus }

// Logger returns the shared logger.
func (o *Orchestrator) Logger() log.Logger { return o.logger }

// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package behavior provides lifecycle management for automated background behaviors.
package behavior

import (
	"context"

	"github.com/lemon4ksan/miyako/bus"
	"github.com/lemon4ksan/miyako/lifecycle"
	"github.com/lemon4ksan/miyako/log"

	"github.com/lemon4ksan/g-man/pkg/steam"
	"github.com/lemon4ksan/g-man/pkg/steam/module"
)

// WithModule registers the behavior orchestrator module in the Steam client.
func WithModule() steam.Option {
	return steam.WithModule(NewModule())
}

// From retrieves the behavior Orchestrator from the Steam client.
func From(c *steam.Client) *Orchestrator {
	return steam.GetModule[*Orchestrator](c)
}

// Orchestrator wraps a miyako BehaviorRunner and adapts it to the Steam client module interface.
type Orchestrator struct {
	*lifecycle.BehaviorRunner
	bus    *bus.Bus
	logger log.Logger
}

// NewOrchestrator creates an Orchestrator with the given bus and logger.
func NewOrchestrator(b *bus.Bus, logger log.Logger, opts ...lifecycle.Option) *Orchestrator {
	return &Orchestrator{
		BehaviorRunner: lifecycle.NewBehaviorRunner(
			append([]lifecycle.Option{lifecycle.WithLogger(logger)}, opts...)...),
		bus:    b,
		logger: logger,
	}
}

// NewModule constructs an uninitialized Orchestrator module.
func NewModule() *Orchestrator {
	return &Orchestrator{
		BehaviorRunner: lifecycle.NewBehaviorRunner(),
	}
}

// Name returns module identifier "behavior".
func (o *Orchestrator) Name() string { return "behavior" }

// Register registers a behavior for execution, safely initializing the runner if uninitialized.
func (o *Orchestrator) Register(b lifecycle.Behavior) {
	if o == nil {
		return
	}

	if o.BehaviorRunner == nil {
		o.BehaviorRunner = lifecycle.NewBehaviorRunner()
	}

	o.BehaviorRunner.Register(b)
}

// Init configures the orchestrator using Steam client initialization context.
func (o *Orchestrator) Init(init module.InitContext) error {
	o.bus = init.Bus()
	o.logger = init.Logger().With(log.Module("behavior"))

	if o.BehaviorRunner == nil {
		o.BehaviorRunner = lifecycle.NewBehaviorRunner(lifecycle.WithLogger(o.logger))
	}

	return nil
}

// Start launches registered behaviors.
func (o *Orchestrator) Start(ctx context.Context) error {
	if o.BehaviorRunner == nil {
		return nil
	}

	return o.BehaviorRunner.Start(ctx)
}

// Close terminates all running behaviors.
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

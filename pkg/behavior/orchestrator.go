// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package behavior provides the orchestrator for managing multiple behaviors.
package behavior

import (
	"github.com/lemon4ksan/miyako/behavior"
	"github.com/lemon4ksan/miyako/bus"

	"github.com/lemon4ksan/g-man/pkg/log"
)

// Behavior is an alias for miyako's Behavior interface.
type Behavior = behavior.Behavior

// Orchestrator wraps miyako's behavior.Orchestrator and stores a shared bus and logger
// so that behaviors can receive them without explicit parameter passing.
type Orchestrator struct {
	*behavior.Orchestrator
	bus    *bus.Bus
	logger log.Logger
}

// NewOrchestrator creates a new orchestrator with the given bus and logger.
func NewOrchestrator(b *bus.Bus, logger log.Logger, opts ...behavior.Option) *Orchestrator {
	return &Orchestrator{
		Orchestrator: behavior.NewOrchestrator(
			append([]behavior.Option{behavior.WithLogger(&logAdapter{logger})}, opts...)...),
		bus:    b,
		logger: logger,
	}
}

// Bus returns the shared event bus.
func (o *Orchestrator) Bus() *bus.Bus { return o.bus }

// Logger returns the shared logger.
func (o *Orchestrator) Logger() log.Logger { return o.logger }

// logAdapter wraps g-man's log.Logger to implement miyako's behavior.Logger.
type logAdapter struct {
	l log.Logger
}

func (a *logAdapter) Info(msg string, args ...any)  { a.l.Info(msg, toFields(args)...) }
func (a *logAdapter) Error(msg string, args ...any) { a.l.Error(msg, toFields(args)...) }
func (a *logAdapter) Warn(msg string, args ...any)  { a.l.Warn(msg, toFields(args)...) }

func toFields(args []any) []log.Field {
	fields := make([]log.Field, 0, len(args)/2)
	for i := 0; i < len(args)-1; i += 2 {
		key, ok := args[i].(string)
		if !ok {
			continue
		}

		fields = append(fields, log.Any(key, args[i+1]))
	}

	return fields
}

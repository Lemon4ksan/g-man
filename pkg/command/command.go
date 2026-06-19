// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package command

import (
	"context"
	"reflect"

	"github.com/lemon4ksan/miyako/generic"
)

// Handler is a function that processes raw text commands.
type Handler func(ctx context.Context, args []string) (string, error)

// TypedHandler is a function that processes parsed/typed command arguments.
type TypedHandler func(ctx context.Context, args []any) (string, error)

// ArgType is represented by Go's standard reflect.Type for type safety and extensibility.
type ArgType = reflect.Type

// ArgSchema represents a command argument schema mapping.
type ArgSchema struct {
	// Name is the programmatic identifier of the argument.
	Name string
	// Type is the expected reflect.Type of the argument value.
	Type ArgType
	// Optional is true if the argument is optional and can be omitted by the caller.
	Optional bool
}

// Required defines a required argument schema using generics.
func Required[T any](name string) ArgSchema {
	return ArgSchema{Name: name, Type: reflect.TypeOf(generic.Zero[T]()), Optional: false}
}

// Optional defines an optional argument schema using generics.
func Optional[T any](name string) ArgSchema {
	return ArgSchema{Name: name, Type: reflect.TypeOf(generic.Zero[T]()), Optional: true}
}

// Command wraps handlers with its associated privilege level and validation metadata.
type Command struct {
	// Handler is the default raw string arguments execution function.
	Handler Handler
	// TypedHandler is the dynamic, slice-of-interface arguments execution function.
	TypedHandler TypedHandler
	// IsAdmin is true if execution of this command is restricted to administrators.
	IsAdmin bool
	// Description is a human-readable short text describing what the command does.
	Description string
	// ArgsSchema is the list of expected arguments mapped to their types and optionality.
	ArgsSchema []ArgSchema
	// Validate is an optional custom validation hook executed on raw string inputs before dispatch.
	Validate func(args []string) error
	// Aliases is the list of alternative command names registered.
	Aliases []string
	// IsAlias is true if this specific command entry is registered as an alias of another command.
	IsAlias bool
}

// Option defines a functional option for command registration.
type Option = generic.Option[*Command]

// WithDescription adds a descriptive text for the command.
func WithDescription(desc string) Option {
	return func(c *Command) {
		c.Description = desc
	}
}

// WithAdmin restricts execution rights to trusted callers.
func WithAdmin() Option {
	return func(c *Command) {
		c.IsAdmin = true
	}
}

// WithArgsSchema adds an automated type validation and conversion schema.
func WithArgsSchema(schema ...ArgSchema) Option {
	return func(c *Command) {
		c.ArgsSchema = schema
	}
}

// WithValidation adds a custom validation hook on raw string inputs.
func WithValidation(valFn func(args []string) error) Option {
	return func(c *Command) {
		c.Validate = valFn
	}
}

// WithAlias registers one or more command name aliases.
func WithAlias(aliases ...string) Option {
	return func(c *Command) {
		c.Aliases = aliases
	}
}

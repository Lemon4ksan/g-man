// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package command

import (
	"context"
	"encoding"
	"errors"
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"sync"

	"github.com/lemon4ksan/miyako/generic"
)

type contextKey string

// CallerKey is the context key used to store [Caller] identity details in a context.
const CallerKey contextKey = "cmd_caller"

// TransportKey is the context key used to store the transport name string in a context.
const TransportKey contextKey = "cmd_transport"

// Caller abstracts the identity executing a command.
// Use [CallerFromContext] to extract the active caller from a [context.Context].
type Caller interface {
	// ID returns a unique string representation of the identifier (such as SteamID, Discord ID, or "console").
	ID() string
	// DisplayName returns a human-readable display name of the caller.
	DisplayName() string
	// IsAdmin reports whether the caller has administrator/privilege execution rights.
	IsAdmin() bool
}

// WithCaller injects a [Caller] into the provided [context.Context].
// It returns the original context unchanged if ctx is nil.
func WithCaller(ctx context.Context, caller Caller) context.Context {
	if ctx == nil {
		return ctx
	}

	return context.WithValue(ctx, CallerKey, caller)
}

// CallerFromContext extracts a [Caller] from the provided [context.Context].
// It returns false if no caller is found or if ctx is nil.
func CallerFromContext(ctx context.Context) (Caller, bool) {
	if ctx == nil {
		return nil, false
	}

	c, ok := ctx.Value(CallerKey).(Caller)

	return c, ok
}

// WithTransport injects a transport name string into the provided [context.Context].
// It returns the original context unchanged if ctx is nil or transport is empty.
func WithTransport(ctx context.Context, transport string) context.Context {
	if ctx == nil {
		return ctx
	}

	return context.WithValue(ctx, TransportKey, transport)
}

// TransportFromContext extracts a transport name string from the provided [context.Context].
// It returns false if no transport is found or if ctx is nil.
func TransportFromContext(ctx context.Context) (string, bool) {
	if ctx == nil {
		return "", false
	}

	t, ok := ctx.Value(TransportKey).(string)

	return t, ok
}

// Handler processes raw text commands using raw string arguments.
type Handler func(ctx context.Context, args []string) (string, error)

// TypedHandler processes parsed and type-converted command arguments.
type TypedHandler func(ctx context.Context, args []any) (string, error)

// ArgType represents Go's standard [reflect.Type] for type safety and extensibility.
type ArgType = reflect.Type

// ArgSchema represents an individual command argument definition.
// Use [Required] or [Optional] to instantiate standard argument schemas.
type ArgSchema struct {
	// Name is the programmatic identifier of the argument.
	Name string
	// Type is the expected [reflect.Type] of the argument value.
	Type ArgType
	// Optional is true if the argument is optional and can be omitted by the caller.
	Optional bool
}

// Required defines a required argument schema with a specified name.
// If the name argument is empty, a default placeholder name is assigned.
func Required[T any](name string) ArgSchema {
	return ArgSchema{Name: name, Type: reflect.TypeOf(generic.Zero[T]()), Optional: false}
}

// Optional defines an optional argument schema with a specified name.
// If the name argument is empty, a default placeholder name is assigned.
func Optional[T any](name string) ArgSchema {
	return ArgSchema{Name: name, Type: reflect.TypeOf(generic.Zero[T]()), Optional: true}
}

// Command wraps execution handlers with structural privilege levels and validation metadata.
// Register commands with an engine using [Engine.Register].
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

// Option defines a functional option for configuring a [Command].
type Option = generic.Option[*Command]

// WithDescription adds a descriptive text for the [Command].
// It returns an unchanged option if desc is empty.
func WithDescription(desc string) Option {
	return func(c *Command) {
		c.Description = desc
	}
}

// WithAdmin restricts [Command] execution rights to trusted administrators.
func WithAdmin() Option {
	return func(c *Command) {
		c.IsAdmin = true
	}
}

// WithArgsSchema configures an automated type validation and conversion schema for the [Command].
// It uses [ArgSchema] instances to enforce parameter constraints.
func WithArgsSchema(schema ...ArgSchema) Option {
	return func(c *Command) {
		c.ArgsSchema = schema
	}
}

// WithValidation configures a custom validation hook executed on raw string inputs.
// The validation function returns an error if checks fail.
func WithValidation(valFn func(args []string) error) Option {
	return func(c *Command) {
		c.Validate = valFn
	}
}

// WithAlias registers alternative command names for the [Command].
func WithAlias(aliases ...string) Option {
	return func(c *Command) {
		c.Aliases = aliases
	}
}

// TypeParser defines a function that parses a raw string value into a typed Go interface.
type TypeParser func(valStr string) (any, error)

// ParseArgs parses raw interface arguments into a typed tuple of type T.
// It checks arguments against the provided [ArgSchema] slice and returns type-converted results.
// It returns an error if arguments count is less than required, or if type conversion fails.
// It will panic if the schema slice is nil.
func ParseArgs[T any](args []any, schema []ArgSchema) (T, error) {
	minRequired := 0
	for _, s := range schema {
		if !s.Optional {
			minRequired++
		}
	}

	if len(args) < minRequired {
		var zero T
		return zero, fmt.Errorf("expected at least %d arguments, got %d", minRequired, len(args))
	}

	var result T

	resultVal := reflect.ValueOf(&result).Elem()

	for i, argSchema := range schema {
		if i >= len(args) {
			if !argSchema.Optional {
				var zero T
				return zero, fmt.Errorf("missing required argument <%s>", argSchema.Name)
			}

			continue
		}

		argVal := reflect.ValueOf(args[i])
		switch {
		case argVal.Type() == argSchema.Type:
			resultVal.Field(i).Set(argVal)
		case argVal.Type().ConvertibleTo(argSchema.Type):
			resultVal.Field(i).Set(argVal.Convert(argSchema.Type))
		default:
			var zero T

			return zero, fmt.Errorf(
				"argument <%s>: cannot convert %s to %s",
				argSchema.Name,
				argVal.Type(),
				argSchema.Type,
			)
		}
	}

	return result, nil
}

// Engine coordinates registration, validation, parsing, and execution of text commands.
// Use [NewEngine] to instantiate a thread-safe coordinator.
// Custom argument types can be handled by registering parsers via [Engine.RegisterTypeParser].
type Engine struct {
	commandsMu sync.RWMutex
	commands   map[string]Command

	parsersMu sync.RWMutex
	parsers   map[reflect.Type]TypeParser
}

// NewEngine creates a new thread-safe command [Engine] instance.
func NewEngine() *Engine {
	return &Engine{
		commands: make(map[string]Command),
		parsers:  make(map[reflect.Type]TypeParser),
	}
}

// RegisterTypeParser registers a custom unmarshal function for the specified [reflect.Type].
// It will panic if the type argument t is nil.
func (e *Engine) RegisterTypeParser(t reflect.Type, parser TypeParser) {
	e.parsersMu.Lock()
	defer e.parsersMu.Unlock()

	e.parsers[t] = parser
}

// Register registers a command name and its associated execution handler with options.
// The handler parameter accepts [Handler], [TypedHandler], or dynamic custom function signatures.
// It panics if the handler has an invalid signature, or if the cmd argument is empty.
func (e *Engine) Register(cmd string, handler any, opts ...Option) {
	c := Command{}

	switch h := handler.(type) {
	case Handler:
		c.Handler = h
	case func(context.Context, []string) (string, error):
		c.Handler = h
	case TypedHandler:
		c.TypedHandler = h
	case func(context.Context, []any) (string, error):
		c.TypedHandler = h
	default:
		val := reflect.ValueOf(handler)
		if val.Kind() == reflect.Func {
			e.registerFuncDynamic(val, &c)
		}

		if c.Handler == nil && c.TypedHandler == nil {
			panic(fmt.Sprintf("command: unsupported handler signature %T for command %q", handler, cmd))
		}
	}

	generic.ApplyOptions(&c, opts...)

	e.commandsMu.Lock()

	e.commands[cmd] = c
	for _, alias := range c.Aliases {
		aliasCmd := c
		aliasCmd.IsAlias = true
		aliasCmd.Aliases = nil // prevent loops/recursion
		e.commands[alias] = aliasCmd
	}

	e.commandsMu.Unlock()
}

// UnregisterCommand removes a registered command and its aliases from the [Engine] registry.
// If the specified command name does not exist, UnregisterCommand does nothing.
func (e *Engine) UnregisterCommand(name string) {
	e.commandsMu.Lock()
	defer e.commandsMu.Unlock()

	if cmd, ok := e.commands[name]; ok {
		for _, alias := range cmd.Aliases {
			delete(e.commands, alias)
		}
	}

	delete(e.commands, name)
}

// UpdateCommandDescription modifies the short help description of a registered command.
// If the specified command name does not exist, UpdateCommandDescription does nothing.
func (e *Engine) UpdateCommandDescription(cmd, desc string) {
	e.commandsMu.Lock()
	defer e.commandsMu.Unlock()

	if c, exists := e.commands[cmd]; exists {
		c.Description = desc
		e.commands[cmd] = c
	}
}

// GetCommand retrieves a copy of a registered [Command] configuration.
// It returns false if the specified command name does not exist.
func (e *Engine) GetCommand(cmd string) (Command, bool) {
	e.commandsMu.RLock()
	defer e.commandsMu.RUnlock()

	c, exists := e.commands[cmd]

	return c, exists
}

// Commands returns a map containing all registered [Command] configurations, excluding aliases.
func (e *Engine) Commands() map[string]Command {
	e.commandsMu.RLock()
	defer e.commandsMu.RUnlock()

	res := make(map[string]Command)
	for name, c := range e.commands {
		if !c.IsAlias {
			res[name] = c
		}
	}

	return res
}

// Execute parses a command line string, validates caller permissions, and invokes the handler.
// It returns an error if cmdLine is empty, the command is unknown, the [Caller] lacks permissions,
// or if the parsed arguments fail to match the registered [ArgSchema].
// Calling Execute with a nil context will result in a panic.
func (e *Engine) Execute(ctx context.Context, cmdLine string) (string, error) {
	if len(cmdLine) == 0 {
		return "", errors.New("empty command line")
	}

	// Support ! or / prefixes optionally
	startIdx := 0
	if cmdLine[0] == '!' || cmdLine[0] == '/' {
		startIdx = 1
	}

	parts := parseCommandLine(cmdLine[startIdx:])
	if len(parts) == 0 {
		return "", errors.New("empty command name")
	}

	cmdName := parts[0]
	args := parts[1:]

	e.commandsMu.RLock()
	cmd, exists := e.commands[cmdName]
	e.commandsMu.RUnlock()

	if !exists {
		return "", fmt.Errorf("unknown command %q", cmdName)
	}

	// Check admin authorization
	if cmd.IsAdmin {
		caller, ok := CallerFromContext(ctx)
		if !ok || !caller.IsAdmin() {
			return "", errors.New("unauthorized command execution")
		}
	}

	// Validate custom rules on raw inputs
	if cmd.Validate != nil {
		if err := cmd.Validate(args); err != nil {
			return "", err
		}
	}

	// Parse schema arguments
	var parsedArgs []any
	if len(cmd.ArgsSchema) > 0 {
		var err error

		parsedArgs, err = e.parseSchemaArgs(args, cmd.ArgsSchema)
		if err != nil {
			return "", err
		}
	}

	if cmd.TypedHandler != nil {
		return cmd.TypedHandler(ctx, parsedArgs)
	}

	if cmd.Handler != nil {
		return cmd.Handler(ctx, args)
	}

	return "", errors.New("command missing executable handler")
}

func (e *Engine) parseSchemaArgs(rawArgs []string, schema []ArgSchema) ([]any, error) {
	parsed := make([]any, len(schema))

	for i, argSchema := range schema {
		if i >= len(rawArgs) {
			if !argSchema.Optional {
				return nil, fmt.Errorf("missing required argument <%s>", argSchema.Name)
			}

			parsed[i] = nil

			continue
		}

		valStr := rawArgs[i]

		var (
			val any
			err error
		)

		e.parsersMu.RLock()
		customParser, hasParser := e.parsers[argSchema.Type]
		e.parsersMu.RUnlock()

		if hasParser {
			val, err = customParser(valStr)
		} else {
			ptrType := reflect.PointerTo(argSchema.Type)
			switch {
			case ptrType.Implements(reflect.TypeFor[encoding.TextUnmarshaler]()):
				ptr := reflect.New(argSchema.Type)
				unmarshaler := ptr.Interface().(encoding.TextUnmarshaler)

				err = unmarshaler.UnmarshalText([]byte(valStr))
				if err == nil {
					val = ptr.Elem().Interface()
				}

			default:
				switch argSchema.Type.Kind() {
				case reflect.String:
					val = valStr
				case reflect.Int:
					var intVal int

					intVal, err = strconv.Atoi(valStr)
					val = intVal
				case reflect.Float64:
					var floatVal float64

					floatVal, err = strconv.ParseFloat(valStr, 64)
					val = floatVal
				case reflect.Uint64:
					var uintVal uint64

					uintVal, err = strconv.ParseUint(valStr, 10, 64)
					val = uintVal
				case reflect.Bool:
					var boolVal bool

					boolVal, err = strconv.ParseBool(valStr)
					val = boolVal
				default:
					return nil, fmt.Errorf("unsupported argument type %s", argSchema.Type.String())
				}
			}
		}

		if err != nil {
			typeName := argSchema.Type.Name()
			if typeName == "" {
				typeName = argSchema.Type.String()
			}

			return nil, fmt.Errorf("argument <%s> must be of type %s (got %q)", argSchema.Name, typeName, valStr)
		}

		parsed[i] = val
	}

	return parsed, nil
}

func (e *Engine) registerFuncDynamic(val reflect.Value, c *Command) {
	typ := val.Type()

	if typ.NumOut() != 2 ||
		typ.Out(0).Kind() != reflect.String ||
		!typ.Out(1).Implements(reflect.TypeFor[error]()) ||
		typ.NumIn() < 1 ||
		typ.In(0) != reflect.TypeFor[context.Context]() {
		panic(fmt.Sprintf("command: invalid signature for command %+v", c))
	}

	if typ.NumIn() == 2 && typ.In(1) == reflect.TypeFor[[]string]() {
		c.Handler = func(ctx context.Context, args []string) (string, error) {
			res := val.Call([]reflect.Value{
				reflect.ValueOf(ctx),
				reflect.ValueOf(args),
			})

			var err error
			if !res[1].IsNil() {
				err = res[1].Interface().(error)
			}

			return res[0].String(), err
		}

		return
	}

	if typ.NumIn() == 2 && typ.In(1) == reflect.TypeFor[[]any]() {
		c.TypedHandler = func(ctx context.Context, args []any) (string, error) {
			res := val.Call([]reflect.Value{
				reflect.ValueOf(ctx),
				reflect.ValueOf(args),
			})

			var err error
			if !res[1].IsNil() {
				err = res[1].Interface().(error)
			}

			return res[0].String(), err
		}

		return
	}

	numParams := typ.NumIn() - 1

	c.ArgsSchema = make([]ArgSchema, numParams)
	for i := range numParams {
		paramType := typ.In(i + 1)
		optional := false
		underlyingType := paramType

		if paramType.Kind() == reflect.Pointer {
			optional = true
			underlyingType = paramType.Elem()
		}

		c.ArgsSchema[i] = ArgSchema{
			Name:     fmt.Sprintf("arg%d", i+1),
			Type:     underlyingType,
			Optional: optional,
		}
	}

	c.TypedHandler = func(ctx context.Context, parsedArgs []any) (string, error) {
		inValues := make([]reflect.Value, typ.NumIn())
		inValues[0] = reflect.ValueOf(ctx)

		for i := range numParams {
			paramType := typ.In(i + 1)

			var argVal any
			if i < len(parsedArgs) {
				argVal = parsedArgs[i]
			}

			if argVal == nil {
				inValues[i+1] = reflect.Zero(paramType)
				continue
			}

			valOf := reflect.ValueOf(argVal)
			if paramType.Kind() == reflect.Pointer {
				ptr := reflect.New(paramType.Elem())
				switch {
				case valOf.Type().AssignableTo(paramType.Elem()):
					ptr.Elem().Set(valOf)
				case valOf.Type().ConvertibleTo(paramType.Elem()):
					ptr.Elem().Set(valOf.Convert(paramType.Elem()))
				default:
					return "", fmt.Errorf("cannot assign %s to %s", valOf.Type(), paramType.Elem())
				}

				inValues[i+1] = ptr
			} else {
				if valOf.Type().AssignableTo(paramType) {
					inValues[i+1] = valOf
				} else {
					inValues[i+1] = valOf.Convert(paramType)
				}
			}
		}

		res := val.Call(inValues)

		var err error
		if !res[1].IsNil() {
			err = res[1].Interface().(error)
		}

		return res[0].String(), err
	}
}

func parseCommandLine(line string) []string {
	var (
		args    []string
		current strings.Builder
	)

	inQuotes := false
	inSingleQuotes := false
	escaped := false

	for _, r := range line {
		if escaped {
			current.WriteRune(r)

			escaped = false

			continue
		}

		if r == '\\' {
			escaped = true
			continue
		}

		if r == '"' && !inSingleQuotes {
			inQuotes = !inQuotes
			continue
		}

		if r == '\'' && !inQuotes {
			inSingleQuotes = !inSingleQuotes
			continue
		}

		if (r == ' ' || r == '\t' || r == '\r' || r == '\n') && !inQuotes && !inSingleQuotes {
			if current.Len() > 0 {
				args = append(args, current.String())
				current.Reset()
			}

			continue
		}

		current.WriteRune(r)
	}

	if current.Len() > 0 {
		args = append(args, current.String())
	}

	return args
}

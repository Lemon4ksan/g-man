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

// TypeParser represents a function that parses a string into a specific type.
type TypeParser func(valStr string) (any, error)

// ParseArgs is a generic helper that parses command arguments into a typed tuple.
// It validates that the provided arguments match the expected schema and returns
// a typed result or an error.
func ParseArgs[T any](args []any, schema []ArgSchema) (T, error) {
	if len(args) < len(schema) {
		var zero T
		return zero, fmt.Errorf("expected %d arguments, got %d", len(schema), len(args))
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

// Engine coordinates registration, validation, parsing and dispatch of commands.
//
// Create new instances of the engine using the [NewEngine] constructor.
// The engine is safe for concurrent use and supports custom type parsers registered
// via [Engine.RegisterTypeParser].
type Engine struct {
	commandsMu sync.RWMutex
	commands   map[string]Command

	parsersMu sync.RWMutex
	parsers   map[reflect.Type]TypeParser
}

// NewEngine creates a new instance of the command Engine.
func NewEngine() *Engine {
	return &Engine{
		commands: make(map[string]Command),
		parsers:  make(map[reflect.Type]TypeParser),
	}
}

// RegisterTypeParser registers a custom type unmarshaler function for typical schemas.
func (e *Engine) RegisterTypeParser(t reflect.Type, parser TypeParser) {
	e.parsersMu.Lock()
	defer e.parsersMu.Unlock()

	e.parsers[t] = parser
}

// Register registers a command with functional options.
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

// UnregisterCommand removes a command from the registry.
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

// UpdateCommandDescription dynamically modifies the description metadata of a registered command.
func (e *Engine) UpdateCommandDescription(cmd, desc string) {
	e.commandsMu.Lock()
	defer e.commandsMu.Unlock()

	if c, exists := e.commands[cmd]; exists {
		c.Description = desc
		e.commands[cmd] = c
	}
}

// GetCommand retrieves a copy of a registered command and its metadata.
func (e *Engine) GetCommand(cmd string) (Command, bool) {
	e.commandsMu.RLock()
	defer e.commandsMu.RUnlock()

	c, exists := e.commands[cmd]

	return c, exists
}

// Commands returns all registered commands (excluding aliases).
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

// Execute parses a command line string, checks permissions, validates and invokes the command handler.
//
// It returns an error if the command line is empty, if the command name is unknown,
// if an administrator command is executed by a non-administrator [Caller],
// or if the parsed arguments fail to match the registered [ArgSchema].
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

	// Verify basic command handler requirements:
	// - Must return (string, error)
	// - Must accept at least one argument: context.Context
	if typ.NumOut() != 2 ||
		typ.Out(0).Kind() != reflect.String ||
		!typ.Out(1).Implements(reflect.TypeFor[error]()) ||
		typ.NumIn() < 1 ||
		typ.In(0) != reflect.TypeFor[context.Context]() {
		panic(fmt.Sprintf("command: invalid signature for command %+v", c))
	}

	// Case 1: Raw string handler: func(context.Context, []string) (string, error)
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

	// Case 2: Unified typed slice handler: func(context.Context, []any) (string, error)
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

	// Case 3: Advanced custom signature handler: func(ctx context.Context, arg1 T1, arg2 T2, ...)
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

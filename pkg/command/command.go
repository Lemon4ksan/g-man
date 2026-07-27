// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package command coordinates parsing, schema validation, authorization, and execution of text commands.
package command

import (
	"context"
	"encoding"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"sync"

	"github.com/lemon4ksan/miyako/generic"

	"github.com/lemon4ksan/g-man/internal/bytesconv"
)

type contextKey string

const (
	CallerKey    contextKey = "cmd_caller"
	TransportKey contextKey = "cmd_transport"
)

var (
	// ErrEmptyCommandLine is returned when executing an empty input line.
	ErrEmptyCommandLine = errors.New("command: empty command line")
	// ErrEmptyCommandName is returned when no command token can be extracted from input.
	ErrEmptyCommandName = errors.New("command: empty command name")
	// ErrUnauthorized is returned when a non-admin caller attempts to execute an admin command.
	ErrUnauthorized = errors.New("command: unauthorized command execution")
	// ErrMissingHandler is returned when a registered command lacks an executable handler function.
	ErrMissingHandler = errors.New("command: command missing executable handler")
)

// Caller abstracts the identity executing a command.
type Caller interface {
	ID() string
	DisplayName() string
	IsAdmin() bool
}

// WithCaller injects a Caller into context.
func WithCaller(ctx context.Context, caller Caller) context.Context {
	if ctx == nil {
		return ctx
	}

	return context.WithValue(ctx, CallerKey, caller)
}

// CallerFromContext extracts a Caller from context.
func CallerFromContext(ctx context.Context) (Caller, bool) {
	if ctx == nil {
		return nil, false
	}

	c, ok := ctx.Value(CallerKey).(Caller)

	return c, ok
}

// WithTransport injects transport label metadata into context.
func WithTransport(ctx context.Context, transport string) context.Context {
	if ctx == nil {
		return ctx
	}

	return context.WithValue(ctx, TransportKey, transport)
}

// TransportFromContext extracts transport label metadata from context.
func TransportFromContext(ctx context.Context) (string, bool) {
	if ctx == nil {
		return "", false
	}

	t, ok := ctx.Value(TransportKey).(string)

	return t, ok
}

// Handler processes raw string argument slices.
type Handler func(ctx context.Context, args []string) (string, error)

// TypedHandler processes parsed interface argument slices.
type TypedHandler func(ctx context.Context, args []any) (string, error)

// ArgType alias for reflect.Type.
type ArgType = reflect.Type

// ArgSchema defines command argument type requirements and optionality.
type ArgSchema struct {
	Name     string
	Type     ArgType
	Optional bool
}

// Required constructs a required ArgSchema for generic type T.
func Required[T any](name string) ArgSchema {
	return ArgSchema{Name: name, Type: reflect.TypeOf(generic.Zero[T]()), Optional: false}
}

// Optional constructs an optional ArgSchema for generic type T.
func Optional[T any](name string) ArgSchema {
	return ArgSchema{Name: name, Type: reflect.TypeOf(generic.Zero[T]()), Optional: true}
}

// Command stores execution handlers, privilege scopes, and parameter schemas for a command.
type Command struct {
	Handler      Handler
	TypedHandler TypedHandler
	IsAdmin      bool
	Description  string
	ArgsSchema   []ArgSchema
	Validate     func(args []string) error
	Aliases      []string
	IsAlias      bool
}

// Option configures a Command structure.
type Option = generic.Option[*Command]

// WithDescription sets command description text.
func WithDescription(desc string) Option {
	return func(c *Command) { c.Description = desc }
}

// WithAdmin marks command execution as restricted to administrators.
func WithAdmin() Option {
	return func(c *Command) { c.IsAdmin = true }
}

// WithArgsSchema configures argument type schemas for automated validation.
func WithArgsSchema(schema ...ArgSchema) Option {
	return func(c *Command) { c.ArgsSchema = schema }
}

// WithValidation sets custom raw input validation hooks.
func WithValidation(valFn func(args []string) error) Option {
	return func(c *Command) { c.Validate = valFn }
}

// WithAlias registers alternative command trigger aliases.
func WithAlias(aliases ...string) Option {
	return func(c *Command) { c.Aliases = aliases }
}

// TypeParser parses raw argument strings into custom Go types.
type TypeParser func(valStr string) (any, error)

// ParseArgs parses dynamic interface slices into target struct representations based on schema definitions.
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

// Engine manages registration, parsing, permissions verification, and execution of text commands.
//
// Thread Safety:
//   - Safe for concurrent registration and execution.
type Engine struct {
	commandsMu sync.RWMutex
	commands   map[string]Command

	parsersMu sync.RWMutex
	parsers   map[reflect.Type]TypeParser
}

// NewEngine constructs a thread-safe Engine.
func NewEngine() *Engine {
	return &Engine{
		commands: make(map[string]Command),
		parsers:  make(map[reflect.Type]TypeParser),
	}
}

// RegisterTypeParser registers custom parsing logic for reflect.Type.
func (e *Engine) RegisterTypeParser(t reflect.Type, parser TypeParser) {
	e.parsersMu.Lock()
	defer e.parsersMu.Unlock()

	e.parsers[t] = parser
}

// Register adds a command handler and its aliases to the engine.
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
		aliasCmd.Aliases = nil
		e.commands[alias] = aliasCmd
	}

	e.commandsMu.Unlock()
}

// UnregisterCommand removes a registered command and its associated aliases.
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

// UpdateCommandDescription updates short help text for a registered command.
func (e *Engine) UpdateCommandDescription(cmd, desc string) {
	e.commandsMu.Lock()
	defer e.commandsMu.Unlock()

	if c, exists := e.commands[cmd]; exists {
		c.Description = desc
		e.commands[cmd] = c
	}
}

// GetCommand retrieves a registered Command metadata copy.
func (e *Engine) GetCommand(cmd string) (Command, bool) {
	e.commandsMu.RLock()
	defer e.commandsMu.RUnlock()

	c, exists := e.commands[cmd]

	return c, exists
}

// Commands returns all primary registered commands (excluding aliases).
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

// Execute parses raw command lines, verifies admin privileges, converts schema arguments, and invokes handlers.
func (e *Engine) Execute(ctx context.Context, cmdLine string) (string, error) {
	if len(cmdLine) == 0 {
		return "", ErrEmptyCommandLine
	}

	startIdx := 0
	if cmdLine[0] == '!' || cmdLine[0] == '/' {
		startIdx = 1
	}

	var stackArgs [16]string

	parts := ParseCommandLineInto(cmdLine[startIdx:], stackArgs[:0])
	if len(parts) == 0 {
		return "", ErrEmptyCommandName
	}

	cmdName := parts[0]
	args := parts[1:]

	e.commandsMu.RLock()
	cmd, exists := e.commands[cmdName]
	e.commandsMu.RUnlock()

	if !exists {
		return "", fmt.Errorf("unknown command %q", cmdName)
	}

	if cmd.IsAdmin {
		caller, ok := CallerFromContext(ctx)
		if !ok || !caller.IsAdmin() {
			return "", ErrUnauthorized
		}
	}

	if cmd.Validate != nil {
		if err := cmd.Validate(args); err != nil {
			return "", err
		}
	}

	var parsedArgs []any
	if len(cmd.ArgsSchema) > 0 {
		var err error

		parsedArgs, err = e.ParseSchemaArgs(args, cmd.ArgsSchema)
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

	return "", ErrMissingHandler
}

// ParseSchemaArgs parses raw string arguments against schema type expectations.
func (e *Engine) ParseSchemaArgs(rawArgs []string, schema []ArgSchema) ([]any, error) {
	var (
		stackParsed [8]any
		parsed      []any
	)

	if len(schema) <= len(stackParsed) {
		parsed = stackParsed[:len(schema)]
	} else {
		parsed = make([]any, len(schema))
	}

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

				err = unmarshaler.UnmarshalText(bytesconv.S2B(valStr))
				if err == nil {
					val = ptr.Elem().Interface()
				}

			case argSchema.Type.Implements(reflect.TypeFor[encoding.TextUnmarshaler]()):
				ptr := reflect.New(argSchema.Type).Elem()
				unmarshaler := ptr.Interface().(encoding.TextUnmarshaler)

				err = unmarshaler.UnmarshalText(bytesconv.S2B(valStr))
				if err == nil {
					val = ptr.Interface()
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

var cmdBuilderPool = sync.Pool{
	New: func() any { return new(strings.Builder) },
}

type argsBuffer struct {
	Slice []string
}

var argsSlicePool = sync.Pool{
	New: func() any {
		return &argsBuffer{
			Slice: make([]string, 0, 16),
		}
	},
}

func releaseArgsSlice(buf *argsBuffer) {
	if buf == nil {
		return
	}

	buf.Slice = buf.Slice[:0]
	argsSlicePool.Put(buf)
}

// ParseCommandLineInto parses line into dst slice without allocating heap slices for standard quotes and escapes.
func ParseCommandLineInto(line string, dst []string) []string {
	if len(line) == 0 {
		return dst[:0]
	}

	if !strings.ContainsAny(line, `"'`+"\\") {
		return append(dst[:0], strings.Fields(line)...)
	}

	args := dst[:0]
	inQuotes := false
	inSingleQuotes := false
	escaped := false

	buf := cmdBuilderPool.Get().(*strings.Builder)

	buf.Reset()
	defer cmdBuilderPool.Put(buf)

	hasSpecial := false
	tokenStart := -1

	for i := range len(line) {
		c := line[i]

		if escaped {
			buf.WriteByte(c)

			escaped = false

			continue
		}

		if c == '\\' {
			if !hasSpecial {
				hasSpecial = true

				buf.Grow(len(line))

				if tokenStart != -1 {
					buf.WriteString(line[tokenStart:i])
				}
			}

			escaped = true

			continue
		}

		if c == '"' && !inSingleQuotes {
			if !hasSpecial {
				hasSpecial = true

				buf.Grow(len(line))

				if tokenStart != -1 {
					buf.WriteString(line[tokenStart:i])
				}
			}

			inQuotes = !inQuotes

			if tokenStart == -1 {
				tokenStart = i
			}

			continue
		}

		if c == '\'' && !inQuotes {
			if !hasSpecial {
				hasSpecial = true

				buf.Grow(len(line))

				if tokenStart != -1 {
					buf.WriteString(line[tokenStart:i])
				}
			}

			inSingleQuotes = !inSingleQuotes

			if tokenStart == -1 {
				tokenStart = i
			}

			continue
		}

		if (c == ' ' || c == '\t' || c == '\r' || c == '\n') && !inQuotes && !inSingleQuotes {
			if hasSpecial {
				if buf.Len() > 0 {
					args = append(args, buf.String())
					buf.Reset()
				}

				hasSpecial = false
			} else if tokenStart != -1 {
				args = append(args, line[tokenStart:i])
			}

			tokenStart = -1

			continue
		}

		if tokenStart == -1 {
			tokenStart = i
		}

		if hasSpecial {
			buf.WriteByte(c)
		}
	}

	if hasSpecial {
		if buf.Len() > 0 {
			args = append(args, buf.String())
		}
	} else if tokenStart != -1 && len(line) > tokenStart {
		args = append(args, line[tokenStart:])
	}

	return args
}

// ParseCommandLine parses input command lines into a slice using pooled buffer objects.
func ParseCommandLine(line string) []string {
	if len(line) == 0 {
		return nil
	}

	argsPtr := argsSlicePool.Get().(*argsBuffer)
	res := ParseCommandLineInto(line, argsPtr.Slice[:0])
	out := slices.Clone(res)

	releaseArgsSlice(argsPtr)

	return out
}

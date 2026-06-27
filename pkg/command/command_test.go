// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package command

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type mockCaller struct {
	id      string
	name    string
	isAdmin bool
}

func (c mockCaller) ID() string          { return c.id }
func (c mockCaller) DisplayName() string { return c.name }
func (c mockCaller) IsAdmin() bool       { return c.isAdmin }

type customTextType struct {
	val string
}

func (c *customTextType) UnmarshalText(text []byte) error {
	if string(text) == "error" {
		return errors.New("unmarshal error")
	}

	c.val = string(text)

	return nil
}

func TestContextHelpers(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	_, ok := CallerFromContext(ctx)
	assert.False(t, ok)

	_, ok = TransportFromContext(ctx)
	assert.False(t, ok)

	caller := mockCaller{id: "user1", name: "Bob", isAdmin: false}
	ctx = WithCaller(ctx, caller)
	c, ok := CallerFromContext(ctx)
	assert.True(t, ok)
	assert.Equal(t, caller, c)

	ctx = WithTransport(ctx, "grpc")
	trans, ok := TransportFromContext(ctx)
	assert.True(t, ok)
	assert.Equal(t, "grpc", trans)
}

func TestCommandOptions(t *testing.T) {
	t.Parallel()

	e := NewEngine()
	validateFn := func(args []string) error { return nil }
	schema := []ArgSchema{Required[int]("num")}

	e.Register("testopt", func(ctx context.Context, args []string) (string, error) {
		return "", nil
	},
		WithDescription("test desc"),
		WithAdmin(),
		WithArgsSchema(schema...),
		WithValidation(validateFn),
		WithAlias("to1", "to2"),
	)

	cmd, ok := e.GetCommand("testopt")
	require.True(t, ok)
	assert.Equal(t, "test desc", cmd.Description)
	assert.True(t, cmd.IsAdmin)
	assert.Equal(t, schema, cmd.ArgsSchema)
	assert.NotNil(t, cmd.Validate)
	assert.Equal(t, []string{"to1", "to2"}, cmd.Aliases)

	aliasCmd, ok := e.GetCommand("to1")
	require.True(t, ok)
	assert.True(t, aliasCmd.IsAlias)
	assert.Nil(t, aliasCmd.Aliases)
}

func TestParseArgs(t *testing.T) {
	t.Parallel()

	schema := []ArgSchema{
		Required[int]("Num"),
		Required[string]("Str"),
		Optional[bool]("Flag"),
	}

	type TestArgs struct {
		Num  int
		Str  string
		Flag bool
	}

	// Success: exact type match
	res, err := ParseArgs[TestArgs]([]any{10, "hello", true}, schema)
	require.NoError(t, err)
	assert.Equal(t, TestArgs{Num: 10, Str: "hello", Flag: true}, res)

	// Success: convertible types
	type FloatArg struct {
		Val float64
	}

	resFloat, err := ParseArgs[FloatArg]([]any{float32(3.14)}, []ArgSchema{Required[float64]("Val")})
	require.NoError(t, err)
	assert.InDelta(t, 3.14, resFloat.Val, 1e-5)

	// Fail: too few arguments
	_, err = ParseArgs[TestArgs]([]any{}, schema)
	assert.ErrorContains(t, err, "expected at least 2 arguments")

	// Fail: non-convertible type
	_, err = ParseArgs[TestArgs]([]any{10, true, true}, schema)
	assert.ErrorContains(t, err, "cannot convert bool to string")

	// Success: optional argument omitted
	resOpt, err := ParseArgs[TestArgs]([]any{10, "hello"}, schema)
	require.NoError(t, err)
	assert.Equal(t, TestArgs{Num: 10, Str: "hello", Flag: false}, resOpt)

	// Fail: missing required argument (optional first, required second)
	schemaBad := []ArgSchema{
		Optional[int]("Opt"),
		Required[string]("Req"),
	}

	type BadArgs struct {
		Opt int
		Req string
	}

	_, err = ParseArgs[BadArgs]([]any{10}, schemaBad)
	assert.ErrorContains(t, err, "missing required argument <Req>")
}

func TestEngine_BasicRegistrationAndExecution(t *testing.T) {
	t.Parallel()

	e := NewEngine()

	// Test 1: Simple CommandHandler registration
	e.Register("ping", func(ctx context.Context, args []string) (string, error) {
		return "pong", nil
	})

	res, err := e.Execute(context.Background(), "!ping")
	require.NoError(t, err)
	assert.Equal(t, "pong", res)

	// Test 2: Unknown command error
	_, err = e.Execute(context.Background(), "/unknown")
	assert.ErrorContains(t, err, "unknown command \"unknown\"")
}

func TestEngine_AdminAuthorization(t *testing.T) {
	t.Parallel()

	e := NewEngine()

	e.Register("shutdown", func(ctx context.Context, args []string) (string, error) {
		return "stopping", nil
	}, WithAdmin())

	// Test 1: Executing without caller in context
	_, err := e.Execute(context.Background(), "!shutdown")
	assert.ErrorContains(t, err, "unauthorized command execution")

	// Test 2: Executing with non-admin caller
	ctxUser := WithCaller(context.Background(), mockCaller{id: "123", name: "Bob", isAdmin: false})
	_, err = e.Execute(ctxUser, "!shutdown")
	assert.ErrorContains(t, err, "unauthorized command execution")

	// Test 3: Executing with admin caller
	ctxAdmin := WithCaller(context.Background(), mockCaller{id: "456", name: "Alice", isAdmin: true})
	res, err := e.Execute(ctxAdmin, "!shutdown")
	require.NoError(t, err)
	assert.Equal(t, "stopping", res)
}

func TestEngine_DynamicReflectionRegistration(t *testing.T) {
	t.Parallel()

	e := NewEngine()

	// Register dynamic custom signature with typical types
	e.Register("math", func(ctx context.Context, a int, b float64, msg string) (string, error) {
		caller, ok := CallerFromContext(ctx)
		if ok && caller.ID() == "admin" {
			return "Access granted", nil
		}

		return "", errors.New("forbidden")
	})

	// Test 1: Successful typed parsing
	ctx := WithCaller(context.Background(), mockCaller{id: "admin", name: "Alice", isAdmin: true})
	res, err := e.Execute(ctx, "math 10 2.5 hello")
	require.NoError(t, err)
	assert.Equal(t, "Access granted", res)

	// Test 2: Invalid argument types
	_, err = e.Execute(ctx, "math ten 2.5 hello")
	assert.ErrorContains(t, err, "argument <arg1> must be of type int (got \"ten\")")
}

func TestEngine_CustomTypeParser(t *testing.T) {
	t.Parallel()

	e := NewEngine()

	type CustomID struct {
		Value string
	}

	// Register a type parser for CustomID
	e.RegisterTypeParser(reflect.TypeOf(CustomID{}), func(valStr string) (any, error) {
		if valStr == "invalid" {
			return nil, errors.New("cannot parse")
		}

		return CustomID{Value: valStr}, nil
	})

	e.Register("lookup", func(ctx context.Context, id CustomID) (string, error) {
		return "Found ID: " + id.Value, nil
	})

	// Test 1: Valid custom type parsing
	res, err := e.Execute(context.Background(), "!lookup my-id")
	require.NoError(t, err)
	assert.Equal(t, "Found ID: my-id", res)

	// Test 2: Custom parser error propagation
	_, err = e.Execute(context.Background(), "!lookup invalid")
	assert.ErrorContains(t, err, "argument <arg1> must be of type CustomID (got \"invalid\")")
}

func TestEngine_Execute_EdgeCases(t *testing.T) {
	t.Parallel()

	e := NewEngine()

	// Empty command line
	_, err := e.Execute(context.Background(), "")
	assert.ErrorContains(t, err, "empty command line")

	// Empty command name after prefix
	_, err = e.Execute(context.Background(), "!")
	assert.ErrorContains(t, err, "empty command name")

	_, err = e.Execute(context.Background(), "/")
	assert.ErrorContains(t, err, "empty command name")

	// Support prefix-less commands
	e.Register("hello", func(ctx context.Context, args []string) (string, error) {
		return "world", nil
	})
	res, err := e.Execute(context.Background(), "hello")
	require.NoError(t, err)
	assert.Equal(t, "world", res)
}

func TestEngine_CommandOperations(t *testing.T) {
	t.Parallel()

	e := NewEngine()

	e.Register("cmd1", func(ctx context.Context, args []string) (string, error) {
		return "res1", nil
	}, WithAlias("c1a", "c1b"), WithDescription("desc1"))

	e.Register("cmd2", func(ctx context.Context, args []string) (string, error) {
		return "res2", nil
	})

	// List commands (excluding aliases)
	cmds := e.Commands()
	assert.Len(t, cmds, 2)
	assert.Contains(t, cmds, "cmd1")
	assert.Contains(t, cmds, "cmd2")
	assert.NotContains(t, cmds, "c1a")

	// Update description
	e.UpdateCommandDescription("cmd1", "new desc")
	cmd1, ok := e.GetCommand("cmd1")
	require.True(t, ok)
	assert.Equal(t, "new desc", cmd1.Description)

	// Unregister command and its aliases
	e.UnregisterCommand("cmd1")
	_, ok = e.GetCommand("cmd1")
	assert.False(t, ok)
	_, ok = e.GetCommand("c1a")
	assert.False(t, ok)

	// Ensure cmd2 still exists
	_, ok = e.GetCommand("cmd2")
	assert.True(t, ok)
}

func TestEngine_Validation(t *testing.T) {
	t.Parallel()

	e := NewEngine()

	e.Register("validate", func(ctx context.Context, args []string) (string, error) {
		return "ok", nil
	}, WithValidation(func(args []string) error {
		if len(args) == 0 {
			return errors.New("need at least 1 arg")
		}

		return nil
	}))

	// Validation fails
	_, err := e.Execute(context.Background(), "validate")
	assert.ErrorContains(t, err, "need at least 1 arg")

	// Validation succeeds
	res, err := e.Execute(context.Background(), "validate 1")
	require.NoError(t, err)
	assert.Equal(t, "ok", res)
}

func TestEngine_Register_Panics(t *testing.T) {
	t.Parallel()

	e := NewEngine()

	// Unsupported handler type (not a function, e.g. int)
	assert.Panics(t, func() {
		e.Register("bad", 123)
	})

	// Invalid return parameter count
	assert.Panics(t, func() {
		e.Register("bad", func(ctx context.Context) string { return "" })
	})

	// First return param is not string
	assert.Panics(t, func() {
		e.Register("bad", func(ctx context.Context) (int, error) { return 0, nil })
	})

	// Second return param is not error
	assert.Panics(t, func() {
		e.Register("bad", func(ctx context.Context) (string, string) { return "", "" })
	})

	// Input parameters count < 1
	assert.Panics(t, func() {
		e.Register("bad", func() (string, error) { return "", nil })
	})

	// First input parameter is not context.Context
	assert.Panics(t, func() {
		e.Register("bad", func(s string) (string, error) { return "", nil })
	})
}

func TestEngine_DynamicHandlers(t *testing.T) {
	t.Parallel()

	e := NewEngine()

	// Case 1: func(context.Context, []string) (string, error) via dynamic reflection
	type RawFunc func(context.Context, []string) (string, error)

	var rawFn RawFunc = func(ctx context.Context, args []string) (string, error) {
		return "raw:" + args[0], nil
	}
	e.Register("raw", rawFn)
	res, err := e.Execute(context.Background(), "raw hello")
	require.NoError(t, err)
	assert.Equal(t, "raw:hello", res)

	// Case 2: func(context.Context, []any) (string, error) via dynamic reflection
	type TypedSliceFunc func(context.Context, []any) (string, error)

	var typedSliceFn TypedSliceFunc = func(ctx context.Context, args []any) (string, error) {
		return "typed_slice", nil
	}
	e.Register("typed_slice", typedSliceFn)
	res, err = e.Execute(context.Background(), "typed_slice")
	require.NoError(t, err)
	assert.Equal(t, "typed_slice", res)

	// Case 3: Advanced custom signature with Pointers and conversions
	e.Register("custom_ptr", func(ctx context.Context, val *int, ratio *float64) (string, error) {
		res := "values:"
		if val != nil {
			res += fmt.Sprintf(" val=%d", *val)
		} else {
			res += " val=nil"
		}

		if ratio != nil {
			res += fmt.Sprintf(" ratio=%.2f", *ratio)
		} else {
			res += " ratio=nil"
		}

		return res, nil
	})

	// Pointer success (both provided)
	res, err = e.Execute(context.Background(), "custom_ptr 42 1.5")
	require.NoError(t, err)
	assert.Equal(t, "values: val=42 ratio=1.50", res)

	// Pointer success (optional omitted)
	res, err = e.Execute(context.Background(), "custom_ptr")
	require.NoError(t, err)
	assert.Equal(t, "values: val=nil ratio=nil", res)

	// Test conversion failure or assignability error in custom dynamic handler
	cmd, ok := e.GetCommand("custom_ptr")
	require.True(t, ok)

	// Call TypedHandler directly with completely invalid type to trigger conversion error
	_, err = cmd.TypedHandler(context.Background(), []any{"not-an-int", 1.5})
	assert.ErrorContains(t, err, "cannot assign string to int")
}

func TestEngine_DirectHandlers(t *testing.T) {
	t.Parallel()

	e := NewEngine()

	// Register a Handler directly
	var h Handler = func(ctx context.Context, args []string) (string, error) {
		return "handler", nil
	}
	e.Register("h", h)
	res, err := e.Execute(context.Background(), "h")
	require.NoError(t, err)
	assert.Equal(t, "handler", res)

	// Register a TypedHandler directly
	var th TypedHandler = func(ctx context.Context, args []any) (string, error) {
		return "typed", nil
	}
	e.Register("th", th)
	res, err = e.Execute(context.Background(), "th")
	require.NoError(t, err)
	assert.Equal(t, "typed", res)
}

func TestEngine_DynamicHandlers_ValueHandling(t *testing.T) {
	t.Parallel()

	e := NewEngine()

	// Register custom signature with non-pointer parameter
	e.Register("custom_val", func(ctx context.Context, val int) (string, error) {
		return fmt.Sprintf("val=%d", val), nil
	})
	cmdVal, ok := e.GetCommand("custom_val")
	require.True(t, ok)

	// Call TypedHandler directly with a nil value for non-pointer int
	res, err := cmdVal.TypedHandler(context.Background(), []any{nil})
	require.NoError(t, err)
	assert.Equal(t, "val=0", res)

	// Call TypedHandler with float32 to float64 conversion
	e.Register("convert", func(ctx context.Context, val float64) (string, error) {
		return fmt.Sprintf("val=%.2f", val), nil
	})
	cmdConvert, ok := e.GetCommand("convert")
	require.True(t, ok)

	// Call TypedHandler with float32 (convertible to float64 but not assignable)
	res, err = cmdConvert.TypedHandler(context.Background(), []any{float32(3.14)})
	require.NoError(t, err)
	assert.Contains(t, res, "val=3.14")
}

func TestEngine_Execute_MissingHandlers(t *testing.T) {
	t.Parallel()

	e := NewEngine()
	e.commandsMu.Lock()
	e.commands["noop"] = Command{
		Description: "no handlers",
	}
	e.commandsMu.Unlock()

	_, err := e.Execute(context.Background(), "noop")
	assert.ErrorContains(t, err, "command missing executable handler")
}

func TestEngine_ParseSchemaArgs(t *testing.T) {
	t.Parallel()

	e := NewEngine()

	// UnmarshalText implementation
	e.Register("unmarshal", func(ctx context.Context, custom customTextType) (string, error) {
		return "unmarshal:" + custom.val, nil
	})

	// Success TextUnmarshaler
	res, err := e.Execute(context.Background(), "unmarshal hello")
	require.NoError(t, err)
	assert.Equal(t, "unmarshal:hello", res)

	// Fail TextUnmarshaler
	_, err = e.Execute(context.Background(), "unmarshal error")
	assert.ErrorContains(t, err, "must be of type customTextType")

	// Primitives parsing success & failure
	e.Register("primitives", func(ctx context.Context, s string, i int, f float64, u uint64, b bool) (string, error) {
		return fmt.Sprintf("%s|%d|%.1f|%d|%t", s, i, f, u, b), nil
	})

	res, err = e.Execute(context.Background(), "primitives str 123 45.6 789 true")
	require.NoError(t, err)
	assert.Equal(t, "str|123|45.6|789|true", res)

	// Fail Int
	_, err = e.Execute(context.Background(), "primitives str bad 45.6 789 true")
	assert.ErrorContains(t, err, "must be of type int (got \"bad\")")

	// Fail Float64
	_, err = e.Execute(context.Background(), "primitives str 123 bad 789 true")
	assert.ErrorContains(t, err, "must be of type float64 (got \"bad\")")

	// Fail Uint64
	_, err = e.Execute(context.Background(), "primitives str 123 45.6 bad true")
	assert.ErrorContains(t, err, "must be of type uint64 (got \"bad\")")

	// Fail Bool
	_, err = e.Execute(context.Background(), "primitives str 123 45.6 789 bad")
	assert.ErrorContains(t, err, "must be of type bool (got \"bad\")")

	// Missing required argument
	_, err = e.Execute(context.Background(), "primitives str 123")
	assert.ErrorContains(t, err, "missing required argument <arg3>")

	// Unsupported argument type (no custom parser and not a primitive/unmarshaler)
	type UnsupportedType struct{ Val int }

	e.Register("unsupported", func(ctx context.Context, val UnsupportedType) (string, error) {
		return "", nil
	})
	_, err = e.Execute(context.Background(), "unsupported anything")
	assert.ErrorContains(t, err, "unsupported argument type command.UnsupportedType")

	// Anonymous struct fallback for type name (Type.Name() == "")
	anonType := reflect.TypeOf(struct{ Val int }{})
	e.RegisterTypeParser(anonType, func(valStr string) (any, error) {
		return nil, errors.New("parse error")
	})
	e.Register("anon", func(ctx context.Context, val struct{ Val int }) (string, error) {
		return "", nil
	})
	_, err = e.Execute(context.Background(), "anon bad")
	assert.ErrorContains(t, err, "must be of type struct { Val int }")
}

func TestParseCommandLine(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		input    string
		expected []string
	}{
		{`cmd arg1 arg2`, []string{"cmd", "arg1", "arg2"}},
		{`cmd "arg1 with spaces" arg2`, []string{"cmd", "arg1 with spaces", "arg2"}},
		{`cmd 'arg1 with single quotes' arg2`, []string{"cmd", "arg1 with single quotes", "arg2"}},
		{`cmd "escaped \" quotes"`, []string{"cmd", `escaped " quotes`}},
		{`cmd 'escaped \' single'`, []string{"cmd", "escaped ' single"}},
		{`cmd escaped\ space`, []string{"cmd", "escaped space"}},
		{`cmd escaped\tspace`, []string{"cmd", "escapedtspace"}},
		{"cmd \t\r\n multiple   spaces", []string{"cmd", "multiple", "spaces"}},
		{`cmd "unclosed quote`, []string{"cmd", "unclosed quote"}},
		{`cmd 'unclosed single`, []string{"cmd", "unclosed single"}},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.input, func(t *testing.T) {
			t.Parallel()

			res := parseCommandLine(tc.input)
			assert.Equal(t, tc.expected, res)
		})
	}
}

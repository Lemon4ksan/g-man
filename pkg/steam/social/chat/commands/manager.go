// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package commands

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/lemon4ksan/miyako/sync/limiter"
	"golang.org/x/time/rate"

	"github.com/lemon4ksan/g-man/pkg/command"
	"github.com/lemon4ksan/g-man/pkg/log"
	"github.com/lemon4ksan/g-man/pkg/steam"
	"github.com/lemon4ksan/g-man/pkg/steam/id"
	"github.com/lemon4ksan/g-man/pkg/steam/module"
	"github.com/lemon4ksan/g-man/pkg/steam/social/chat"
)

// ModuleName is the unique string identifier of the chat command manager.
const ModuleName = "chat_commands"

// WithModule returns a [steam.Option] that registers a [Manager] in the client module registry.
func WithModule() steam.Option {
	return steam.WithModule(NewManager())
}

// From retrieves the registered [Manager] instance from the specified [steam.Client].
// It returns nil if the manager is not registered, or if the client is nil.
func From(c *steam.Client) *Manager {
	return steam.GetModule[*Manager](c)
}

type (
	// CommandHandler processes legacy string commands from a sender.
	CommandHandler func(ctx context.Context, senderID uint64, args []string) (string, error)

	// TypedHandler processes parsed command arguments from a sender.
	TypedHandler func(ctx context.Context, senderID uint64, args []any) (string, error)
)

type (
	// ArgType represents Go's standard reflect type for command argument mapping.
	ArgType = command.ArgType
	// ArgSchema represents an individual command argument definition.
	ArgSchema = command.ArgSchema
)

// Required defines a required argument schema using generics.
// If the name argument is empty, a default placeholder name is assigned.
func Required[T any](name string) ArgSchema {
	return command.Required[T](name)
}

// Optional defines an optional argument schema using generics.
// If the name argument is empty, a default placeholder name is assigned.
func Optional[T any](name string) ArgSchema {
	return command.Optional[T](name)
}

// Command holds metadata and execution functions for a registered chat command.
// It provides backwards compatibility with legacy client structures.
type Command struct {
	// Handler is the default raw string arguments execution function.
	Handler CommandHandler
	// TypedHandler is the dynamic, slice-of-interface arguments execution function.
	TypedHandler TypedHandler
	// IsAdmin is true if execution of this command is restricted to trusted administrators.
	IsAdmin bool
	// Description is a short help text describing what the command does.
	Description string
	// ArgsSchema is the list of expected arguments mapped to their types.
	ArgsSchema []ArgSchema
	// Validate is an optional custom validation hook executed on raw string inputs.
	Validate func(args []string) error
	// Aliases is the list of alternative command names registered.
	Aliases []string
	// IsAlias is true if this entry is registered as an alias of another command.
	IsAlias bool
}

// CommandOption defines a functional option for configuring a command.
type CommandOption = command.Option

// WithDescription configures a descriptive help text for a command.
func WithDescription(desc string) CommandOption {
	return command.WithDescription(desc)
}

// WithAdmin configures a command to be executable only by trusted administrators.
func WithAdmin() CommandOption {
	return command.WithAdmin()
}

// WithArgsSchema configures an automated type validation and conversion schema for a command.
func WithArgsSchema(schema ...ArgSchema) CommandOption {
	return command.WithArgsSchema(schema...)
}

// WithValidation configures a custom validation function on raw string inputs.
func WithValidation(valFn func(args []string) error) CommandOption {
	return command.WithValidation(valFn)
}

// WithAlias configures one or more alternative aliases for a command.
func WithAlias(aliases ...string) CommandOption {
	return command.WithAlias(aliases...)
}

// ChatSender defines the contract for sending outgoing chat messages.
type ChatSender interface {
	// SendMessage transmits a text chat message to the specified Steam user.
	SendMessage(ctx context.Context, steamID uint64, text string) error
}

// Registry defines the registration and management contract for chat commands.
type Registry interface {
	// Register registers a command name and its execution handler with options.
	Register(cmd string, handler any, opts ...CommandOption)
	// UpdateCommandDescription modifies the help description of a registered command.
	UpdateCommandDescription(cmd, desc string)
}

// SteamCaller represents the identity of a Steam user executing a chat command.
// It implements the [command.Caller] interface for command authorization checks.
type SteamCaller struct {
	steamID uint64
	isAdmin bool
}

// ID returns the Steam ID of the caller formatted as a string.
func (c SteamCaller) ID() string { return strconv.FormatUint(c.steamID, 10) }

// DisplayName returns an empty string as Steam callers do not have static display names.
func (c SteamCaller) DisplayName() string { return "" }

// IsAdmin reports whether the caller has administrator execution privileges.
func (c SteamCaller) IsAdmin() bool { return c.isAdmin }

// Manager coordinates registration, authorization, and asynchronous dispatch of chat commands.
// It wraps a universal [command.Engine] instance for backward compatibility.
type Manager struct {
	module.Base

	// Underlying universal command engine
	engine *command.Engine

	// Dependencies
	chat ChatSender

	// Trusted/Admin SteamIDs
	trustedMu sync.RWMutex
	trusted   map[uint64]bool

	// Per-user rate limiting
	limiter *limiter.KeyedLimiter[uint64]
}

// NewManager creates a new [Manager] instance.
// It registers custom type parsers to handle Steam [id.ID] arguments.
func NewManager() *Manager {
	engine := command.NewEngine()

	// Register specific type parser for Steam id.ID
	engine.RegisterTypeParser(reflect.TypeFor[id.ID](), func(valStr string) (any, error) {
		parsedID := id.Parse(valStr)
		if parsedID == id.InvalidID || !parsedID.IsValid() {
			return nil, errors.New("invalid SteamID format")
		}

		return parsedID, nil
	})

	return &Manager{
		Base:    module.New(ModuleName),
		engine:  engine,
		trusted: make(map[uint64]bool),
		limiter: limiter.NewKeyedLimiter[uint64](rate.Limit(2), 5, 1*time.Hour),
	}
}

// Init resolves external dependencies and registers built-in command metadata.
// It will panic if the provided [module.InitContext] is nil.
func (m *Manager) Init(init module.InitContext) error {
	if err := m.Base.Init(init); err != nil {
		return err
	}

	// Resolve the underlying low-level chat transport dependency
	chatMod := init.Module(chat.ModuleName)
	if chatMod == nil {
		return errors.New("commands: low-level chat module dependency is missing")
	}

	chatClient, ok := chatMod.(ChatSender)
	if !ok {
		return errors.New("commands: module resolved as 'chat' does not implement ChatSender")
	}

	m.chat = chatClient

	// Register ready-made help command with h alias
	m.Register("help", m.handleHelpCommand,
		WithDescription("Lists all registered commands and their usage"),
		WithAlias("h"),
	)

	m.Logger.Info("Universal chat commands manager adapter initialized successfully")

	return nil
}

// Start activates background routines and triggers event subscriptions.
// It will panic if the provided [context.Context] is nil.
func (m *Manager) Start(ctx context.Context) error {
	if err := m.Base.Start(ctx); err != nil {
		return err
	}

	m.Go(func(ctx context.Context) {
		m.eventLoop(ctx)
	})

	return nil
}

// Register registers a command name and its execution handler.
// It automatically detects legacy signatures using sender ID and wraps them to match the universal engine.
// It will panic if the command name is empty or if the handler signature is unsupported.
func (m *Manager) Register(cmd string, handler any, opts ...CommandOption) {
	val := reflect.ValueOf(handler)
	typ := val.Type()

	// Check if this is a legacy signature: func(context.Context, uint64, ...) (string, error)
	if typ.Kind() == reflect.Func && typ.NumIn() >= 2 &&
		typ.In(0) == reflect.TypeFor[context.Context]() &&
		typ.In(1) == reflect.TypeFor[uint64]() {
		// Handle legacy signatures by wrapping them
		switch {
		case typ.NumIn() == 3 && typ.In(2) == reflect.TypeFor[[]string]():
			// Case A: func(context.Context, uint64, []string) (string, error)
			wrapped := func(ctx context.Context, args []string) (string, error) {
				var senderID uint64
				if caller, ok := command.CallerFromContext(ctx); ok {
					senderID, _ = strconv.ParseUint(caller.ID(), 10, 64)
				}

				res := val.Call([]reflect.Value{
					reflect.ValueOf(ctx),
					reflect.ValueOf(senderID),
					reflect.ValueOf(args),
				})

				var err error
				if !res[1].IsNil() {
					err = res[1].Interface().(error)
				}

				return res[0].String(), err
			}
			m.engine.Register(cmd, wrapped, opts...)

		case typ.NumIn() == 3 && typ.In(2) == reflect.TypeFor[[]any]():
			// Case B: func(context.Context, uint64, []any) (string, error)
			wrapped := func(ctx context.Context, args []any) (string, error) {
				var senderID uint64
				if caller, ok := command.CallerFromContext(ctx); ok {
					senderID, _ = strconv.ParseUint(caller.ID(), 10, 64)
				}

				res := val.Call([]reflect.Value{
					reflect.ValueOf(ctx),
					reflect.ValueOf(senderID),
					reflect.ValueOf(args),
				})

				var err error
				if !res[1].IsNil() {
					err = res[1].Interface().(error)
				}

				return res[0].String(), err
			}
			m.engine.Register(cmd, wrapped, opts...)

		default:
			// Case C: func(ctx context.Context, senderID uint64, arg1, arg2, argN) (string, error)
			inTypes := []reflect.Type{reflect.TypeFor[context.Context]()}
			for i := 2; i < typ.NumIn(); i++ {
				inTypes = append(inTypes, typ.In(i))
			}

			outTypes := []reflect.Type{reflect.TypeFor[string](), reflect.TypeFor[error]()}
			newFuncType := reflect.FuncOf(inTypes, outTypes, false)

			wrappedVal := reflect.MakeFunc(newFuncType, func(args []reflect.Value) []reflect.Value {
				ctx := args[0].Interface().(context.Context)

				var senderID uint64
				if caller, ok := command.CallerFromContext(ctx); ok {
					senderID, _ = strconv.ParseUint(caller.ID(), 10, 64)
				}

				callArgs := make([]reflect.Value, typ.NumIn())
				callArgs[0] = args[0]

				callArgs[1] = reflect.ValueOf(senderID)
				for i := 2; i < typ.NumIn(); i++ {
					callArgs[i] = args[i-1]
				}

				return val.Call(callArgs)
			})
			m.engine.Register(cmd, wrappedVal.Interface(), opts...)
		}
	} else {
		// Native universal signature, register directly
		m.engine.Register(cmd, handler, opts...)
	}
}

// IsAdminCommand reports whether the specified command is restricted to administrators.
func (m *Manager) IsAdminCommand(name string) bool {
	cmd, ok := m.engine.GetCommand(name)
	return ok && cmd.IsAdmin
}

// UnregisterCommand removes a registered command and its aliases from the registry.
func (m *Manager) UnregisterCommand(name string) {
	m.engine.UnregisterCommand(name)
}

// UpdateCommandDescription modifies the help description of a registered command.
func (m *Manager) UpdateCommandDescription(cmd, desc string) {
	m.engine.UpdateCommandDescription(cmd, desc)
}

// SetTrustedSteamIDs updates the set of trusted SteamIDs allowed to execute administrator commands.
// It ignores strings that cannot be parsed as valid 64-bit unsigned integers.
func (m *Manager) SetTrustedSteamIDs(ids []string) {
	m.trustedMu.Lock()
	defer m.trustedMu.Unlock()

	m.trusted = make(map[uint64]bool)
	for _, idStr := range ids {
		if val, err := strconv.ParseUint(idStr, 10, 64); err == nil {
			m.trusted[val] = true
		}
	}
}

// IsTrusted reports whether the specified SteamID belongs to a trusted administrator.
func (m *Manager) IsTrusted(steamID uint64) bool {
	m.trustedMu.RLock()
	defer m.trustedMu.RUnlock()
	return m.trusted[steamID]
}

// GetCommand retrieves a copy of a registered [Command] configuration.
// It returns false if the specified command name does not exist.
func (m *Manager) GetCommand(cmd string) (Command, bool) {
	c, exists := m.engine.GetCommand(cmd)
	if !exists {
		return Command{}, false
	}

	return Command{
		Handler:      nil,
		TypedHandler: nil,
		IsAdmin:      c.IsAdmin,
		Description:  c.Description,
		ArgsSchema:   c.ArgsSchema,
		Validate:     c.Validate,
		Aliases:      c.Aliases,
		IsAlias:      c.IsAlias,
	}, true
}

// Close closes the underlying rate limiter and releases allocated system resources.
func (m *Manager) Close() error {
	return m.limiter.Close()
}

// eventLoop handles event-driven command parsing by subscribing to chat.MessageEvent.
func (m *Manager) eventLoop(ctx context.Context) {
	sub := m.Bus.Subscribe(&chat.MessageEvent{})
	defer sub.Unsubscribe()

	for {
		select {
		case <-ctx.Done():
			return
		case ev := <-sub.C():
			mev, ok := ev.(*chat.MessageEvent)
			if !ok {
				continue
			}

			msgText := mev.Message
			if len(msgText) == 0 || (msgText[0] != '!' && msgText[0] != '/') {
				continue
			}

			// We need the command name first to check admin permissions and rate limit
			// before invoking Execute
			startIdx := 0
			if msgText[0] == '!' || msgText[0] == '/' {
				startIdx = 1
			}

			// Parse command line into parts to find command name
			parts := parseCommandLine(msgText[startIdx:])
			if len(parts) == 0 {
				continue
			}

			cmdName := parts[0]

			cmd, exists := m.engine.GetCommand(cmdName)
			if !exists {
				continue
			}

			trusted := m.IsTrusted(mev.SenderID)

			// Apply per-user rate limiting (bypass for trusted administrators)
			if !trusted {
				allowed, err := m.limiter.Allow(mev.SenderID)
				if err != nil || !allowed {
					m.Logger.WarnContext(
						mev.Context(),
						"Rate limit exceeded or error occurred for user",
						log.String("command", cmdName),
						log.Uint64("sender", mev.SenderID),
						log.Err(err),
					)

					if m.chat != nil {
						_ = m.chat.SendMessage(
							ctx,
							mev.SenderID,
							"Error: You are sending commands too fast. Please slow down.",
						)
					}

					continue
				}
			}

			if cmd.IsAdmin && !trusted {
				m.Logger.WarnContext(
					mev.Context(),
					"Unauthorized command execution attempt",
					log.String("command", cmdName),
					log.Uint64("sender", mev.SenderID),
				)

				if m.chat != nil {
					_ = m.chat.SendMessage(ctx, mev.SenderID, "Error: You are not authorized to execute this command.")
				}

				continue
			}

			m.Go(func(ctx context.Context) {
				if corrID, ok := log.CorrelationID(mev.Context()); ok {
					ctx = log.WithCorrelationID(ctx, corrID)
				}

				caller := SteamCaller{
					steamID: mev.SenderID,
					isAdmin: trusted,
				}
				cmdCtx := command.WithCaller(ctx, caller)
				cmdCtx = command.WithTransport(cmdCtx, "steam_chat")

				response, err := m.engine.Execute(cmdCtx, msgText)
				if err != nil {
					m.Logger.ErrorContext(
						cmdCtx,
						"Chat command execution failed",
						log.String("command", cmdName),
						log.Err(err),
					)

					if m.chat != nil {
						_ = m.chat.SendMessage(cmdCtx, mev.SenderID, fmt.Sprintf("Error: %v", err))
					}
				} else if response != "" {
					if m.chat != nil {
						_ = m.chat.SendMessage(cmdCtx, mev.SenderID, response)
					}
				}
			})
		}
	}
}

func (m *Manager) handleHelpCommand(ctx context.Context, args []string) (string, error) {
	caller, ok := command.CallerFromContext(ctx)
	trusted := ok && caller.IsAdmin()

	var list []helpInfo
	for name, c := range m.engine.Commands() {
		if c.IsAlias {
			continue
		}

		if trusted || !c.IsAdmin {
			list = append(list, helpInfo{
				Name:        name,
				IsAdmin:     c.IsAdmin,
				ArgsSchema:  c.ArgsSchema,
				Description: c.Description,
				Aliases:     c.Aliases,
			})
		}
	}

	sort.Slice(list, func(i, j int) bool {
		return list[i].Name < list[j].Name
	})

	var sb strings.Builder
	sb.WriteString("Available Commands:\n")

	for _, item := range list {
		if len(item.Aliases) > 0 {
			var formattedAliases []string
			for _, a := range item.Aliases {
				formattedAliases = append(formattedAliases, "!"+a)
			}

			fmt.Fprintf(&sb, "- !%s (aliases: %s)", item.Name, strings.Join(formattedAliases, ", "))
		} else {
			fmt.Fprintf(&sb, "- !%s", item.Name)
		}

		for _, arg := range item.ArgsSchema {
			typeName := arg.Type.Name()
			if typeName == "" {
				typeName = arg.Type.String()
			}

			if arg.Optional {
				fmt.Fprintf(&sb, " [<%s:%s>]", arg.Name, typeName)
			} else {
				fmt.Fprintf(&sb, " <%s:%s>", arg.Name, typeName)
			}
		}

		if item.IsAdmin {
			sb.WriteString(" [Admin]")
		}

		if item.Description != "" {
			fmt.Fprintf(&sb, ": %s", item.Description)
		}

		sb.WriteString("\n")
	}

	return sb.String(), nil
}

type helpInfo struct {
	Name        string
	IsAdmin     bool
	ArgsSchema  []ArgSchema
	Description string
	Aliases     []string
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

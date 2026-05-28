// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package commands provides a decoupled chat command manager for the Steam chat module.
//
// It automatically hooks into chat events, enforces administrator scopes using [SteamCaller],
// applies per-user rate limiting, and dispatches executed results back to the user via Steam chat.
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

	"golang.org/x/time/rate"

	"github.com/lemon4ksan/g-man/pkg/command"
	"github.com/lemon4ksan/g-man/pkg/log"
	"github.com/lemon4ksan/g-man/pkg/steam"
	"github.com/lemon4ksan/g-man/pkg/steam/id"
	"github.com/lemon4ksan/g-man/pkg/steam/module"
	"github.com/lemon4ksan/g-man/pkg/steam/social/chat"
)

// ModuleName is the unique identifier for the command manager module.
const ModuleName = "chat_commands"

type (
	// CommandHandler is the function signature for legacy command handlers.
	CommandHandler func(ctx context.Context, senderID uint64, args []string) (string, error)

	// TypedHandler is the function signature for typed command handlers.
	TypedHandler func(ctx context.Context, senderID uint64, args []any) (string, error)
)

type (
	// ArgType is an alias for command.ArgType to preserve backward compatibility.
	ArgType = command.ArgType
	// ArgSchema is an alias for command.ArgSchema to preserve backward compatibility.
	ArgSchema = command.ArgSchema
)

// Required defines a required argument schema for a command using generics.
func Required[T any](name string) ArgSchema {
	return command.Required[T](name)
}

// Optional defines an optional argument schema for a command using generics.
func Optional[T any](name string) ArgSchema {
	return command.Optional[T](name)
}

// Command wraps metadata for backward compatibility (GetCommand returns this type)
type Command struct {
	Handler      CommandHandler
	TypedHandler TypedHandler
	IsAdmin      bool
	Description  string
	ArgsSchema   []ArgSchema
	Validate     func(args []string) error
	Aliases      []string
	IsAlias      bool
}

// CommandOption defines a functional option for configuring a command.
type CommandOption = command.Option

// WithDescription sets the description of the command.
func WithDescription(desc string) CommandOption {
	return command.WithDescription(desc)
}

// WithAdmin sets the command as an admin command.
func WithAdmin() CommandOption {
	return command.WithAdmin()
}

// WithArgsSchema sets the argument schema for the command.
func WithArgsSchema(schema ...ArgSchema) CommandOption {
	return command.WithArgsSchema(schema...)
}

// WithValidation sets the validation function for the command.
func WithValidation(valFn func(args []string) error) CommandOption {
	return command.WithValidation(valFn)
}

// WithAlias sets the aliases for the command.
func WithAlias(aliases ...string) CommandOption {
	return command.WithAlias(aliases...)
}

// ChatSender defines the interface required to send chat messages.
type ChatSender interface {
	SendMessage(ctx context.Context, steamID uint64, text string) error
}

// Registry defines a decoupled, minimal interface for registering and managing chat commands.
type Registry interface {
	Register(cmd string, handler any, opts ...CommandOption)
	UpdateCommandDescription(cmd, desc string)
}

// SteamCaller implements [command.Caller] for the Steam chat transport.
type SteamCaller struct {
	steamID uint64
	isAdmin bool
}

// ID returns the Steam ID of the caller as a string.
func (c SteamCaller) ID() string { return strconv.FormatUint(c.steamID, 10) }

// DisplayName returns the display name of the caller.
func (c SteamCaller) DisplayName() string { return "" }

// IsAdmin returns whether the caller is an admin.
func (c SteamCaller) IsAdmin() bool { return c.isAdmin }

// Manager coordinates registration, authorization, and async dispatch of chat commands.
// It wraps the universal command engine for backwards compatibility.
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
	limiterMu sync.RWMutex
	limiters  map[uint64]*rate.Limiter
}

// NewManager creates a new instance of the command manager.
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
		Base:     module.New(ModuleName),
		engine:   engine,
		trusted:  make(map[uint64]bool),
		limiters: make(map[uint64]*rate.Limiter),
	}
}

// WithModule returns a steam.Option that registers the command manager in the client.
func WithModule() steam.Option {
	return func(c *steam.Client) {
		c.RegisterModule(NewManager())
	}
}

// From returns the command manager from the client.
func From(c *steam.Client) *Manager {
	return steam.GetModule[*Manager](c)
}

// Init resolves dependencies and registers command metadata.
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

// Start launches the background event subscription worker.
func (m *Manager) Start(ctx context.Context) error {
	if err := m.Base.Start(ctx); err != nil {
		return err
	}

	m.Go(func(ctx context.Context) {
		m.eventLoop(ctx)
	})

	return nil
}

// Register registers a command with functional options.
// It automatically detects legacy signatures taking senderID uint64 as the second argument
// and wraps them dynamically to fit the new universal command engine.
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
			// Case C: func(ctx context.Context, senderID uint64, a1 T1, a2 T2, ...) (string, error)
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

// IsAdminCommand returns whether the given command is an admin command.
func (m *Manager) IsAdminCommand(name string) bool {
	cmd, ok := m.engine.GetCommand(name)
	return ok && cmd.IsAdmin
}

// UnregisterCommand removes a command from the registry along with all its registered aliases.
func (m *Manager) UnregisterCommand(name string) {
	m.engine.UnregisterCommand(name)
}

// UpdateCommandDescription dynamically modifies the description metadata of a registered command.
func (m *Manager) UpdateCommandDescription(cmd, desc string) {
	m.engine.UpdateCommandDescription(cmd, desc)
}

// SetTrustedSteamIDs updates the set of trusted SteamIDs that can execute admin commands.
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

// IsTrusted checks if a SteamID is currently in the trusted administrators set.
func (m *Manager) IsTrusted(steamID uint64) bool {
	m.trustedMu.RLock()
	defer m.trustedMu.RUnlock()
	return m.trusted[steamID]
}

// GetCommand retrieves a copy of a registered command and its metadata.
func (m *Manager) GetCommand(cmd string) (Command, bool) {
	c, exists := m.engine.GetCommand(cmd)
	if !exists {
		return Command{}, false
	}

	return Command{
		IsAdmin:     c.IsAdmin,
		Description: c.Description,
		ArgsSchema:  c.ArgsSchema,
		Validate:    c.Validate,
		Aliases:     c.Aliases,
		IsAlias:     c.IsAlias,
	}, true
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
				limiter := m.getLimiter(mev.SenderID)
				if !limiter.Allow() {
					m.Logger.WarnContext(
						mev.Context(),
						"Rate limit exceeded for user",
						log.String("command", cmdName),
						log.Uint64("sender", mev.SenderID),
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

// getLimiter retrieves or creates a per-user rate limiter.
func (m *Manager) getLimiter(senderID uint64) *rate.Limiter {
	m.limiterMu.RLock()
	limiter, exists := m.limiters[senderID]
	m.limiterMu.RUnlock()

	if exists {
		return limiter
	}

	m.limiterMu.Lock()
	defer m.limiterMu.Unlock()

	// Double-check under write lock
	limiter, exists = m.limiters[senderID]
	if !exists {
		// Default: 2 tokens per second, with a burst buffer of 5
		limiter = rate.NewLimiter(rate.Limit(2), 5)
		m.limiters[senderID] = limiter
	}

	return limiter
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

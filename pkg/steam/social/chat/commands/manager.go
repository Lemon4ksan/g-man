// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package commands provides an event-driven command manager for parsing, rate-limiting, and executing chat commands over Steam chat.
package commands

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/lemon4ksan/miyako/log"
	"github.com/lemon4ksan/miyako/sync/limiter"
	"golang.org/x/time/rate"

	"github.com/lemon4ksan/g-man/pkg/command"
	"github.com/lemon4ksan/g-man/pkg/steam"
	"github.com/lemon4ksan/g-man/pkg/steam/id"
	"github.com/lemon4ksan/g-man/pkg/steam/module"
	"github.com/lemon4ksan/g-man/pkg/steam/social/chat"
)

const ModuleName = "chat_commands"

// WithModule registers a Manager module in the client.
func WithModule() steam.Option {
	return steam.WithModule(NewManager())
}

// From retrieves the Manager module instance from the client.
func From(c *steam.Client) *Manager {
	return steam.GetModule[*Manager](c)
}

type (
	CommandHandler func(ctx context.Context, senderID uint64, args []string) (string, error)

	TypedHandler func(ctx context.Context, senderID uint64, args []any) (string, error)

	StickerHandler func(ctx context.Context, ev *chat.StickerEvent) error

	ReactionHandler func(ctx context.Context, ev *chat.ReactionEvent) error
)

type (
	ArgType = command.ArgType

	ArgSchema = command.ArgSchema
)

func Required[T any](name string) ArgSchema {
	return command.Required[T](name)
}

func Optional[T any](name string) ArgSchema {
	return command.Optional[T](name)
}

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

type CommandOption = command.Option

func WithDescription(desc string) CommandOption {
	return command.WithDescription(desc)
}

func WithAdmin() CommandOption {
	return command.WithAdmin()
}

func WithArgsSchema(schema ...ArgSchema) CommandOption {
	return command.WithArgsSchema(schema...)
}

func WithValidation(valFn func(args []string) error) CommandOption {
	return command.WithValidation(valFn)
}

func WithAlias(aliases ...string) CommandOption {
	return command.WithAlias(aliases...)
}

type ChatSender interface {
	SendMessage(ctx context.Context, steamID uint64, text string) error
}

type Registry interface {
	Register(cmd string, handler any, opts ...CommandOption)
	UpdateCommandDescription(cmd, desc string)
}

// SteamCaller adapts a Steam user identity for command authorization checks.
type SteamCaller struct {
	steamID uint64
	isAdmin bool
}

func (c SteamCaller) ID() string          { return strconv.FormatUint(c.steamID, 10) }
func (c SteamCaller) DisplayName() string { return "" }
func (c SteamCaller) IsAdmin() bool       { return c.isAdmin }

var (
	// ErrInvalidSteamIDFormat indicates a string parameter could not be parsed as a valid SteamID.
	ErrInvalidSteamIDFormat = errors.New("invalid SteamID format")
	// ErrChatModuleMissing indicates the low-level chat module dependency was absent.
	ErrChatModuleMissing = errors.New("commands: low-level chat module dependency is missing")
	// ErrChatSenderMismatch indicates module 'chat' does not satisfy ChatSender.
	ErrChatSenderMismatch = errors.New("commands: module resolved as 'chat' does not implement ChatSender")
)

// Manager handles command registrations, per-user rate limiting, admin access control, and execution loops.
//
// Thread Safety:
//   - Safe for concurrent use across all methods.
type Manager struct {
	module.Base

	engine        *command.Engine
	chat          ChatSender
	conversations *command.ConversationManager

	trustedMu sync.RWMutex
	trusted   map[uint64]bool

	limiter *limiter.KeyedLimiter[uint64]

	eventsMu         sync.RWMutex
	stickerHandlers  []StickerHandler
	reactionHandlers []ReactionHandler
}

// NewManager constructs a Manager instance with custom id.ID parsers, rate limiters, and FSM.
func NewManager() *Manager {
	engine := command.NewEngine()

	engine.RegisterTypeParser(reflect.TypeFor[id.ID](), func(valStr string) (any, error) {
		parsedID := id.Parse(valStr)
		if parsedID == id.InvalidID || !parsedID.IsValid() {
			return nil, ErrInvalidSteamIDFormat
		}

		return parsedID, nil
	})

	return &Manager{
		Base:          module.New(ModuleName),
		engine:        engine,
		conversations: command.NewConversationManager(15 * time.Minute),
		trusted:       make(map[uint64]bool),
		limiter:       limiter.NewKeyedLimiter[uint64](rate.Limit(2), 5, 1*time.Hour),
	}
}

func (m *Manager) Init(init module.InitContext) error {
	if err := m.Base.Init(init); err != nil {
		return err
	}

	chatMod := init.Module(chat.ModuleName)
	if chatMod == nil {
		return ErrChatModuleMissing
	}

	chatClient, ok := chatMod.(ChatSender)
	if !ok {
		return ErrChatSenderMismatch
	}

	m.chat = chatClient

	m.Register("help", m.handleHelpCommand,
		WithDescription("Lists all registered commands and their usage"),
		WithAlias("h"),
	)

	m.Logger.Info("Universal chat commands manager adapter initialized successfully")

	return nil
}

func (m *Manager) Start(ctx context.Context) error {
	if err := m.Base.Start(ctx); err != nil {
		return err
	}

	m.Go(func(ctx context.Context) {
		m.eventLoop(ctx)
	})

	return nil
}

// Register registers command handlers, adapting legacy sender-ID function signatures automatically.
func (m *Manager) Register(cmd string, handler any, opts ...CommandOption) {
	val := reflect.ValueOf(handler)
	typ := val.Type()

	if typ.Kind() == reflect.Func && typ.NumIn() >= 2 &&
		typ.In(0) == reflect.TypeFor[context.Context]() &&
		typ.In(1) == reflect.TypeFor[uint64]() {
		switch {
		case typ.NumIn() == 3 && typ.In(2) == reflect.TypeFor[[]string]():
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
		m.engine.Register(cmd, handler, opts...)
	}
}

// IsAdminCommand reports whether command is restricted to administrators.
func (m *Manager) IsAdminCommand(name string) bool {
	cmd, ok := m.engine.GetCommand(name)

	return ok && cmd.IsAdmin
}

// UnregisterCommand removes a command and its registered aliases.
func (m *Manager) UnregisterCommand(name string) {
	m.engine.UnregisterCommand(name)
}

// UpdateCommandDescription modifies the help description of a command.
func (m *Manager) UpdateCommandDescription(cmd, desc string) {
	m.engine.UpdateCommandDescription(cmd, desc)
}

// SetTrustedSteamIDs updates the set of SteamIDs authorized to execute administrator commands.
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

// IsTrusted reports whether steamID belongs to a trusted administrator.
func (m *Manager) IsTrusted(steamID uint64) bool {
	m.trustedMu.RLock()
	defer m.trustedMu.RUnlock()

	return m.trusted[steamID]
}

// GetCommand retrieves a registered Command structure.
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

// Engine returns the underlying command Engine instance.
func (m *Manager) Engine() *command.Engine {
	return m.engine
}

// Use appends global command middlewares to the engine execution pipeline.
func (m *Manager) Use(mw ...command.Middleware) {
	m.engine.Use(mw...)
}

// Conversations returns the Manager's ConversationManager (FSM) instance.
func (m *Manager) Conversations() *command.ConversationManager {
	return m.conversations
}

// OnSticker registers a callback for incoming sticker events.
func (m *Manager) OnSticker(handler StickerHandler) {
	m.eventsMu.Lock()
	defer m.eventsMu.Unlock()

	m.stickerHandlers = append(m.stickerHandlers, handler)
}

// OnReaction registers a callback for incoming emoji reaction events.
func (m *Manager) OnReaction(handler ReactionHandler) {
	m.eventsMu.Lock()
	defer m.eventsMu.Unlock()

	m.reactionHandlers = append(m.reactionHandlers, handler)
}

func (m *Manager) Close() error {
	return m.limiter.Close()
}

func (m *Manager) eventLoop(ctx context.Context) {
	subMsg := m.Bus.Subscribe(&chat.MessageEvent{})
	defer subMsg.Unsubscribe()

	subSticker := m.Bus.Subscribe(&chat.StickerEvent{})
	defer subSticker.Unsubscribe()

	subReaction := m.Bus.Subscribe(&chat.ReactionEvent{})
	defer subReaction.Unsubscribe()

	for {
		select {
		case <-ctx.Done():
			return

		case ev := <-subSticker.C():
			sev, ok := ev.(*chat.StickerEvent)
			if !ok {
				continue
			}

			m.eventsMu.RLock()
			handlers := slices.Clone(m.stickerHandlers)
			m.eventsMu.RUnlock()

			for _, h := range handlers {
				if err := h(ctx, sev); err != nil {
					m.Logger.ErrorContext(ctx, "Sticker handler failed", log.Err(err))
				}
			}

		case ev := <-subReaction.C():
			rev, ok := ev.(*chat.ReactionEvent)
			if !ok {
				continue
			}

			m.eventsMu.RLock()
			handlers := slices.Clone(m.reactionHandlers)
			m.eventsMu.RUnlock()

			for _, h := range handlers {
				if err := h(ctx, rev); err != nil {
					m.Logger.ErrorContext(ctx, "Reaction handler failed", log.Err(err))
				}
			}

		case ev := <-subMsg.C():
			mev, ok := ev.(*chat.MessageEvent)
			if !ok {
				continue
			}

			msgText := mev.Message

			if m.conversations != nil {
				senderIDStr := strconv.FormatUint(mev.SenderID, 10)

				handled, response, err := m.conversations.HandleInput(ctx, senderIDStr, msgText)
				if handled {
					if err != nil {
						m.Logger.ErrorContext(
							ctx,
							"FSM conversation error",
							log.Uint64("sender", mev.SenderID),
							log.Err(err),
						)

						if m.chat != nil && response != "" {
							_ = m.chat.SendMessage(ctx, mev.SenderID, response)
						}
					} else if response != "" && m.chat != nil {
						_ = m.chat.SendMessage(ctx, mev.SenderID, response)
					}

					continue
				}
			}

			if len(msgText) == 0 || (msgText[0] != '!' && msgText[0] != '/') {
				continue
			}

			startIdx := 0
			if msgText[0] == '!' || msgText[0] == '/' {
				startIdx = 1
			}

			parts := command.ParseCommandLine(msgText[startIdx:])
			if len(parts) == 0 {
				continue
			}

			cmdName := parts[0]

			cmd, exists := m.engine.GetCommand(cmdName)
			if !exists {
				continue
			}

			trusted := m.IsTrusted(mev.SenderID)

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

func (m *Manager) handleHelpCommand(ctx context.Context, _ []string) (string, error) {
	caller, ok := command.CallerFromContext(ctx)
	isAdmin := ok && caller.IsAdmin()

	commands := m.engine.Commands()
	if len(commands) == 0 {
		return "Available Commands:\n(No commands registered)", nil
	}

	var sb strings.Builder
	sb.WriteString("Available Commands:\n")

	for name, cmd := range commands {
		if cmd.IsAdmin && !isAdmin {
			continue
		}

		sb.WriteString("- !")
		sb.WriteString(name)

		if len(cmd.Aliases) > 0 {
			sb.WriteString(" (aliases: !")
			sb.WriteString(strings.Join(cmd.Aliases, ", !"))
			sb.WriteString(")")
		}

		for _, schema := range cmd.ArgsSchema {
			sb.WriteString(" ")

			if schema.Optional {
				sb.WriteString("[<")
			} else {
				sb.WriteString("<")
			}

			sb.WriteString(schema.Name)

			if schema.Type != nil {
				sb.WriteString(":")
				sb.WriteString(schema.Type.Name())
			}

			if schema.Optional {
				sb.WriteString(">]")
			} else {
				sb.WriteString(">")
			}
		}

		if cmd.Description != "" {
			sb.WriteString(": ")
			sb.WriteString(cmd.Description)
		}

		sb.WriteString("\n")
	}

	return strings.TrimRight(sb.String(), "\n"), nil
}

// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package commands

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
	"unsafe"

	"github.com/lemon4ksan/miyako/bus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lemon4ksan/g-man/pkg/command"
	"github.com/lemon4ksan/g-man/pkg/log"
	pb "github.com/lemon4ksan/g-man/pkg/protobuf/steam"
	"github.com/lemon4ksan/g-man/pkg/steam/id"
	"github.com/lemon4ksan/g-man/pkg/steam/module"
	"github.com/lemon4ksan/g-man/pkg/steam/social/chat"
	"github.com/lemon4ksan/g-man/pkg/steam/social/friends"
	"github.com/lemon4ksan/g-man/test/mock"
)

const (
	BotSteamID   = uint64(76561198000000001)
	AdminSteamID = uint64(76561198000000002)
	UserSteamID  = uint64(76561198000000003)
)

type dummyModule struct {
	module.Base
}

type dummyEvent struct {
	bus.BaseEvent
}

func populateMockFriends(friendsMgr *friends.Manager, friendID id.ID, name string) {
	val := reflect.ValueOf(friendsMgr).Elem()
	for i := 0; i < val.NumField(); i++ {
		field := val.Field(i)
		fieldType := field.Type()

		settableField := reflect.NewAt(fieldType, unsafe.Pointer(field.UnsafeAddr())).Elem()

		if fieldType.Kind() == reflect.Map {
			keyType := fieldType.Key()
			if keyType == reflect.TypeOf(id.ID(0)) || keyType == reflect.TypeOf(uint64(0)) {
				if settableField.IsNil() {
					settableField.Set(reflect.MakeMap(fieldType))
				}

				var keyVal reflect.Value
				if keyType == reflect.TypeOf(id.ID(0)) {
					keyVal = reflect.ValueOf(friendID)
				} else {
					keyVal = reflect.ValueOf(friendID.Uint64())
				}

				elemType := fieldType.Elem()
				if elemType.Kind() == reflect.Pointer {
					structType := elemType.Elem()
					if structType.Kind() == reflect.Struct {
						if _, ok := structType.FieldByName("PlayerName"); ok {
							newStruct := reflect.New(structType)
							newStruct.Elem().FieldByName("PlayerName").SetString(name)

							for k := 0; k < structType.NumField(); k++ {
								f := structType.Field(k)

								fName := strings.ToLower(f.Name)
								if fName == "steamid" || fName == "id" {
									newStruct.Elem().Field(k).Set(reflect.ValueOf(friendID).Convert(f.Type))
								}

								if strings.Contains(fName, "relation") {
									newStruct.Elem().Field(k).Set(reflect.ValueOf(3).Convert(f.Type))
								}
							}

							settableField.SetMapIndex(keyVal, newStruct)
						}
					}
				} else if elemType.ConvertibleTo(reflect.TypeOf(int(0))) {
					settableField.SetMapIndex(keyVal, reflect.ValueOf(3).Convert(elemType))
				}
			}
		}

		if fieldType.Kind() == reflect.Slice {
			elemType := fieldType.Elem()
			switch {
			case elemType == reflect.TypeFor[id.ID]():
				newSlice := reflect.Append(settableField, reflect.ValueOf(friendID))
				settableField.Set(newSlice)
			case elemType == reflect.TypeFor[uint64]():
				newSlice := reflect.Append(settableField, reflect.ValueOf(friendID.Uint64()))
				settableField.Set(newSlice)
			case elemType.Kind() == reflect.Pointer:
				structType := elemType.Elem()
				if structType.Kind() == reflect.Struct {
					if _, ok := structType.FieldByName("PlayerName"); ok {
						newStruct := reflect.New(structType)
						newStruct.Elem().FieldByName("PlayerName").SetString(name)

						for k := 0; k < structType.NumField(); k++ {
							f := structType.Field(k)

							fName := strings.ToLower(f.Name)
							if fName == "steamid" || fName == "id" {
								newStruct.Elem().Field(k).Set(reflect.ValueOf(friendID).Convert(f.Type))
							}

							if strings.Contains(fName, "relation") {
								newStruct.Elem().Field(k).Set(reflect.ValueOf(3).Convert(f.Type))
							}
						}

						newSlice := reflect.Append(settableField, newStruct)
						settableField.Set(newSlice)
					}
				}
			}
		}
	}
}

func setContextOnEvent(ev *chat.MessageEvent, ctx context.Context) {
	val := reflect.ValueOf(ev).Elem()
	for i := 0; i < val.NumField(); i++ {
		field := val.Field(i)

		fieldType := field.Type()
		if fieldType == reflect.TypeFor[context.Context]() {
			settableField := reflect.NewAt(fieldType, unsafe.Pointer(field.UnsafeAddr())).Elem()
			settableField.Set(reflect.ValueOf(ctx))
			return
		}

		if field.Kind() == reflect.Struct {
			for j := 0; j < field.NumField(); j++ {
				subField := field.Field(j)

				subFieldType := subField.Type()
				if subFieldType == reflect.TypeFor[context.Context]() {
					settableField := reflect.NewAt(subFieldType, unsafe.Pointer(subField.UnsafeAddr())).Elem()
					settableField.Set(reflect.ValueOf(ctx))
					return
				}
			}
		}
	}
}

func waitForCalls(t *testing.T, ictx *mock.InitContext, expectedCount int) {
	t.Helper()
	require.Eventually(t, func() bool {
		return ictx.MockService().CallsCount() >= expectedCount
	}, 2*time.Second, 5*time.Millisecond)
}

func setupTest(t *testing.T, ctx context.Context) (*chat.Chat, *Manager, *mock.InitContext) {
	t.Helper()

	chatMod := chat.New()
	cmdMgr := NewManager()

	ictx := mock.NewInitContext()
	ictx.SetModule("chat", chatMod)
	ictx.SetModule("chat_commands", cmdMgr)

	require.NoError(t, chatMod.Init(ictx))
	require.NoError(t, cmdMgr.Init(ictx))

	require.NoError(t, chatMod.Start(ctx))
	require.NoError(t, cmdMgr.Start(ctx))

	time.Sleep(50 * time.Millisecond)

	return chatMod, cmdMgr, ictx
}

func TestCommandManager_Init(t *testing.T) {
	t.Parallel()

	t.Run("missing_chat_dependency", func(t *testing.T) {
		t.Parallel()

		m := NewManager()
		ictx := mock.NewInitContext()
		err := m.Init(ictx)
		assert.ErrorContains(t, err, "low-level chat module dependency is missing")
	})

	t.Run("incorrect_chat_type", func(t *testing.T) {
		t.Parallel()

		m := NewManager()
		ictx := mock.NewInitContext()
		fakeMod := &dummyModule{Base: module.New("chat")}
		ictx.SetModule("chat", fakeMod)

		err := m.Init(ictx)
		assert.ErrorContains(t, err, "does not implement ChatSender")
	})

	t.Run("success", func(t *testing.T) {
		t.Parallel()

		m := NewManager()
		ictx := mock.NewInitContext()
		chatMod := chat.New()
		ictx.SetModule("chat", chatMod)

		err := m.Init(ictx)
		assert.NoError(t, err)
	})
}

func TestCommandManager_Registration(t *testing.T) {
	t.Parallel()

	t.Run("register_command", func(t *testing.T) {
		t.Parallel()

		m := NewManager()

		called := false
		m.Register("test", func(ctx context.Context, senderID uint64, args []string) (string, error) {
			called = true
			return "response", nil
		})

		cmd, exists := m.GetCommand("test")
		assert.True(t, exists)
		assert.NotNil(t, cmd)

		caller := SteamCaller{steamID: 123}
		ctx := command.WithCaller(t.Context(), caller)
		res, err := m.engine.Execute(ctx, "!test")
		assert.NoError(t, err)
		assert.Equal(t, "response", res)
		assert.True(t, called)
	})

	t.Run("command_validation_field", func(t *testing.T) {
		t.Parallel()

		m := NewManager()

		valErr := errors.New("invalid validation")
		m.Register("test_val", func(ctx context.Context, senderID uint64, args []string) (string, error) {
			return "ok", nil
		}, WithValidation(func(args []string) error {
			return valErr
		}))

		cmd, exists := m.GetCommand("test_val")
		require.True(t, exists)
		assert.ErrorIs(t, cmd.Validate(nil), valErr)
	})
}

func TestCommandManager_TrustedSteamIDs(t *testing.T) {
	t.Parallel()

	m := NewManager()

	m.SetTrustedSteamIDs([]string{"76561198000000002", "76561198000000004", "invalid_id"})

	assert.True(t, m.IsTrusted(76561198000000002))
	assert.True(t, m.IsTrusted(76561198000000004))
	assert.False(t, m.IsTrusted(76561198000000003))
	assert.False(t, m.IsTrusted(0))
}

func TestCommandManager_EventRouting(t *testing.T) {
	t.Parallel()

	t.Run("execute_public_command_success", func(t *testing.T) {
		t.Parallel()
		_, cmdMgr, ictx := setupTest(t, t.Context())
		eb := ictx.Bus()

		cmdMgr.Register("ping", func(ctx context.Context, senderID uint64, args []string) (string, error) {
			return "pong", nil
		})

		eb.Publish(&chat.MessageEvent{
			SenderID: UserSteamID,
			Message:  "!ping",
		})

		waitForCalls(t, ictx, 1)

		req := &pb.CFriendMessages_SendMessage_Request{}
		ictx.MockService().GetLastCall(req)
		assert.Equal(t, UserSteamID, req.GetSteamid())
		assert.Equal(t, "pong", req.GetMessage())
	})

	t.Run("execute_admin_command_success", func(t *testing.T) {
		t.Parallel()
		_, cmdMgr, ictx := setupTest(t, t.Context())
		eb := ictx.Bus()

		cmdMgr.SetTrustedSteamIDs([]string{"76561198000000002"})
		cmdMgr.Register(
			"restart",
			func(ctx context.Context, senderID uint64, args []string) (string, error) {
				return "restarting...", nil
			},
			WithAdmin(),
		)

		eb.Publish(&chat.MessageEvent{
			SenderID: AdminSteamID,
			Message:  "!restart",
		})

		waitForCalls(t, ictx, 1)

		req := &pb.CFriendMessages_SendMessage_Request{}
		ictx.MockService().GetLastCall(req)
		assert.Equal(t, AdminSteamID, req.GetSteamid())
		assert.Equal(t, "restarting...", req.GetMessage())
	})

	t.Run("execute_admin_command_unauthorized", func(t *testing.T) {
		t.Parallel()
		_, cmdMgr, ictx := setupTest(t, t.Context())
		eb := ictx.Bus()

		cmdMgr.SetTrustedSteamIDs([]string{"76561198000000002"})
		cmdMgr.Register(
			"restart",
			func(ctx context.Context, senderID uint64, args []string) (string, error) {
				return "restarting...", nil
			},
			WithAdmin(),
		)

		eb.Publish(&chat.MessageEvent{
			SenderID: UserSteamID,
			Message:  "/restart",
		})

		waitForCalls(t, ictx, 1)

		req := &pb.CFriendMessages_SendMessage_Request{}
		ictx.MockService().GetLastCall(req)
		assert.Equal(t, UserSteamID, req.GetSteamid())
		assert.Contains(t, req.GetMessage(), "You are not authorized")
	})

	t.Run("execute_command_failure_output", func(t *testing.T) {
		t.Parallel()
		_, cmdMgr, ictx := setupTest(t, t.Context())
		eb := ictx.Bus()

		cmdMgr.Register("fail", func(ctx context.Context, senderID uint64, args []string) (string, error) {
			return "", errors.New("something went wrong")
		})

		eb.Publish(&chat.MessageEvent{
			SenderID: UserSteamID,
			Message:  "!fail",
		})

		waitForCalls(t, ictx, 1)

		req := &pb.CFriendMessages_SendMessage_Request{}
		ictx.MockService().GetLastCall(req)
		assert.Equal(t, UserSteamID, req.GetSteamid())
		assert.Contains(t, req.GetMessage(), "Error: something went wrong")
	})

	t.Run("ignore_unregistered_command", func(t *testing.T) {
		t.Parallel()
		_, _, ictx := setupTest(t, t.Context())
		eb := ictx.Bus()

		eb.Publish(&chat.MessageEvent{
			SenderID: UserSteamID,
			Message:  "!unknown",
		})

		time.Sleep(150 * time.Millisecond)

		assert.Equal(
			t, 0, ictx.MockService().CallsCount(),
			"should be no unified service calls since the command is unregistered",
		)
	})

	t.Run("message_routing_filter_edge_cases", func(t *testing.T) {
		t.Parallel()
		_, cmdMgr, ictx := setupTest(t, t.Context())
		eb := ictx.Bus()

		cmdMgr.Register("ping", func(ctx context.Context, senderID uint64, args []string) (string, error) {
			return "pong", nil
		})

		eb.Publish(&dummyEvent{})
		eb.Publish(&chat.MessageEvent{SenderID: UserSteamID, Message: ""})
		eb.Publish(&chat.MessageEvent{SenderID: UserSteamID, Message: "ping"})
		eb.Publish(&chat.MessageEvent{SenderID: UserSteamID, Message: "!"})

		time.Sleep(100 * time.Millisecond)
		assert.Equal(t, 0, ictx.MockService().CallsCount())
	})

	t.Run("execute_command_empty_response", func(t *testing.T) {
		t.Parallel()
		_, cmdMgr, ictx := setupTest(t, t.Context())
		eb := ictx.Bus()

		cmdMgr.Register("empty", func(ctx context.Context, senderID uint64, args []string) (string, error) {
			return "", nil
		})

		eb.Publish(&chat.MessageEvent{
			SenderID: UserSteamID,
			Message:  "!empty",
		})

		time.Sleep(100 * time.Millisecond)
		assert.Equal(t, 0, ictx.MockService().CallsCount())
	})

	t.Run("correlation_id_propagation", func(t *testing.T) {
		t.Parallel()
		_, cmdMgr, ictx := setupTest(t, t.Context())
		eb := ictx.Bus()

		var (
			mu             sync.Mutex
			capturedCorrID string
		)

		cmdMgr.Register("corr", func(ctx context.Context, senderID uint64, args []string) (string, error) {
			mu.Lock()
			if id, ok := log.CorrelationID(ctx); ok {
				capturedCorrID = id
			}

			mu.Unlock()

			return "ok", nil
		})

		ev := &chat.MessageEvent{
			SenderID: UserSteamID,
			Message:  "!corr",
		}

		ctxWithCorr := log.WithCorrelationID(t.Context(), "my-correlation-id")
		setContextOnEvent(ev, ctxWithCorr)

		eb.Publish(ev)

		waitForCalls(t, ictx, 1)

		mu.Lock()
		corr := capturedCorrID
		mu.Unlock()
		assert.Equal(t, "my-correlation-id", corr)
	})
}

func TestCommandManager_HelpCommand(t *testing.T) {
	t.Parallel()

	_, cmdMgr, ictx := setupTest(t, t.Context())
	eb := ictx.Bus()

	cmdMgr.Register("ping", func(ctx context.Context, senderID uint64, args []string) (string, error) {
		return "pong", nil
	}, WithDescription("Simple alive check"))

	cmdMgr.Register("add", func(ctx context.Context, senderID uint64, args []string) (string, error) {
		return "added", nil
	}, WithDescription("Adds two numbers"), WithArgsSchema(
		Required[int]("a"),
		Optional[int]("b"),
	))

	eb.Publish(&chat.MessageEvent{
		SenderID: UserSteamID,
		Message:  "!help",
	})

	waitForCalls(t, ictx, 1)

	req := &pb.CFriendMessages_SendMessage_Request{}
	ictx.MockService().GetLastCall(req)
	assert.Equal(t, UserSteamID, req.GetSteamid())
	helpText := req.GetMessage()
	assert.Contains(t, helpText, "Available Commands:")
	assert.Contains(t, helpText, "- !help (aliases: !h): Lists all registered commands and their usage")
	assert.Contains(t, helpText, "- !ping: Simple alive check")
	assert.Contains(t, helpText, "- !add <a:int> [<b:int>]: Adds two numbers")
}

func TestCommandManager_TypedArguments(t *testing.T) {
	t.Parallel()

	t.Run("valid_types_parsing", func(t *testing.T) {
		t.Parallel()
		_, cmdMgr, ictx := setupTest(t, t.Context())
		eb := ictx.Bus()

		cmdMgr.Register("math", func(ctx context.Context, senderID uint64, args []any) (string, error) {
			a := args[0].(int)
			b := args[1].(float64)
			c := args[2].(bool)

			return fmt.Sprintf("result: %d + %.1f, ok: %t", a, b, c), nil
		}, WithArgsSchema(
			Required[int]("a"),
			Required[float64]("b"),
			Required[bool]("c"),
		))

		eb.Publish(&chat.MessageEvent{
			SenderID: UserSteamID,
			Message:  "!math 10 5.5 true",
		})

		waitForCalls(t, ictx, 1)

		req := &pb.CFriendMessages_SendMessage_Request{}
		ictx.MockService().GetLastCall(req)
		assert.Equal(t, UserSteamID, req.GetSteamid())
		assert.Equal(t, "result: 10 + 5.5, ok: true", req.GetMessage())
	})

	t.Run("invalid_types_parsing_error", func(t *testing.T) {
		t.Parallel()
		_, cmdMgr, ictx := setupTest(t, t.Context())
		eb := ictx.Bus()

		cmdMgr.Register("math", func(ctx context.Context, senderID uint64, args []any) (string, error) {
			return "ok", nil
		}, WithArgsSchema(
			Required[int]("a"),
		))

		eb.Publish(&chat.MessageEvent{
			SenderID: UserSteamID,
			Message:  "!math abc",
		})

		waitForCalls(t, ictx, 1)

		req := &pb.CFriendMessages_SendMessage_Request{}
		ictx.MockService().GetLastCall(req)
		assert.Equal(t, UserSteamID, req.GetSteamid())
		assert.Contains(t, req.GetMessage(), "must be of type int")
	})

	t.Run("missing_required_argument_error", func(t *testing.T) {
		t.Parallel()
		_, cmdMgr, ictx := setupTest(t, t.Context())
		eb := ictx.Bus()

		cmdMgr.Register("math", func(ctx context.Context, senderID uint64, args []any) (string, error) {
			return "ok", nil
		}, WithArgsSchema(
			Required[int]("a"),
		))

		eb.Publish(&chat.MessageEvent{
			SenderID: UserSteamID,
			Message:  "!math",
		})

		waitForCalls(t, ictx, 1)

		req := &pb.CFriendMessages_SendMessage_Request{}
		ictx.MockService().GetLastCall(req)
		assert.Equal(t, UserSteamID, req.GetSteamid())
		assert.Contains(t, req.GetMessage(), "missing required argument")
	})
}

func TestCommandManager_SteamIDParsing(t *testing.T) {
	t.Parallel()

	t.Run("parse_valid_64_bit_steamid", func(t *testing.T) {
		t.Parallel()
		_, cmdMgr, ictx := setupTest(t, t.Context())
		eb := ictx.Bus()

		cmdMgr.Register("invite", func(ctx context.Context, senderID uint64, target id.ID) (string, error) {
			return fmt.Sprintf("parsed: %d", target.Uint64()), nil
		}, WithArgsSchema(
			Required[id.ID]("targetID"),
		))

		eb.Publish(&chat.MessageEvent{
			SenderID: UserSteamID,
			Message:  "!invite 76561198000000002",
		})

		waitForCalls(t, ictx, 1)

		req := &pb.CFriendMessages_SendMessage_Request{}
		ictx.MockService().GetLastCall(req)
		assert.Equal(t, UserSteamID, req.GetSteamid())
		assert.Equal(t, "parsed: 76561198000000002", req.GetMessage())
	})

	t.Run("parse_valid_steam3_format", func(t *testing.T) {
		t.Parallel()
		_, cmdMgr, ictx := setupTest(t, t.Context())
		eb := ictx.Bus()

		cmdMgr.Register("invite", func(ctx context.Context, senderID uint64, target id.ID) (string, error) {
			return fmt.Sprintf("parsed: %d", target.Uint64()), nil
		}, WithArgsSchema(
			Required[id.ID]("targetID"),
		))

		eb.Publish(&chat.MessageEvent{
			SenderID: UserSteamID,
			Message:  "!invite [U:1:12345]",
		})

		waitForCalls(t, ictx, 1)

		req := &pb.CFriendMessages_SendMessage_Request{}
		ictx.MockService().GetLastCall(req)
		assert.Equal(t, "parsed: 76561197960278073", req.GetMessage())
	})

	t.Run("parse_valid_steam2_format", func(t *testing.T) {
		t.Parallel()
		_, cmdMgr, ictx := setupTest(t, t.Context())
		eb := ictx.Bus()

		cmdMgr.Register("invite", func(ctx context.Context, senderID uint64, target id.ID) (string, error) {
			return fmt.Sprintf("parsed: %d", target.Uint64()), nil
		}, WithArgsSchema(
			Required[id.ID]("targetID"),
		))

		eb.Publish(&chat.MessageEvent{
			SenderID: UserSteamID,
			Message:  "!invite STEAM_0:0:12345",
		})

		waitForCalls(t, ictx, 1)

		req := &pb.CFriendMessages_SendMessage_Request{}
		ictx.MockService().GetLastCall(req)
		assert.Equal(t, "parsed: 76561197960290418", req.GetMessage())
	})

	t.Run("parse_invalid_steamid_error", func(t *testing.T) {
		t.Parallel()
		_, cmdMgr, ictx := setupTest(t, t.Context())
		eb := ictx.Bus()

		cmdMgr.Register("invite", func(ctx context.Context, senderID uint64, target id.ID) (string, error) {
			return "ok", nil
		}, WithArgsSchema(
			Required[id.ID]("targetID"),
		))

		eb.Publish(&chat.MessageEvent{
			SenderID: UserSteamID,
			Message:  "!invite abc",
		})

		waitForCalls(t, ictx, 1)

		req := &pb.CFriendMessages_SendMessage_Request{}
		ictx.MockService().GetLastCall(req)
		assert.Equal(t, UserSteamID, req.GetSteamid())
		assert.Contains(t, req.GetMessage(), "must be of type ID")
	})
}

func TestCommandManager_ArgumentsWithSpaces(t *testing.T) {
	t.Parallel()

	t.Run("double_quotes_and_escapes", func(t *testing.T) {
		t.Parallel()
		_, cmdMgr, ictx := setupTest(t, t.Context())
		eb := ictx.Bus()

		var (
			mu                           sync.Mutex
			receivedUser, receivedReason string
		)

		cmdMgr.Register("warn", func(ctx context.Context, senderID uint64, user, reason string) (string, error) {
			mu.Lock()
			receivedUser = user
			receivedReason = reason
			mu.Unlock()

			return fmt.Sprintf("warned: %s for %s", user, reason), nil
		}, WithArgsSchema(
			Required[string]("user"),
			Required[string]("reason"),
		))

		eb.Publish(&chat.MessageEvent{
			SenderID: UserSteamID,
			Message:  `!warn "User Name" "Spamming in chat"`,
		})

		waitForCalls(t, ictx, 1)

		mu.Lock()
		user := receivedUser
		reason := receivedReason
		mu.Unlock()
		assert.Equal(t, "User Name", user)
		assert.Equal(t, "Spamming in chat", reason)

		ictx.MockService().ClearCalls()

		eb.Publish(&chat.MessageEvent{
			SenderID: UserSteamID,
			Message:  `!warn "User \"Cool\" Name" "Spamming"`,
		})

		waitForCalls(t, ictx, 1)

		mu.Lock()
		user = receivedUser
		reason = receivedReason
		mu.Unlock()
		assert.Equal(t, `User "Cool" Name`, user)
		assert.Equal(t, "Spamming", reason)
	})

	t.Run("single_quotes_and_whitespace", func(t *testing.T) {
		t.Parallel()
		_, cmdMgr, ictx := setupTest(t, t.Context())
		eb := ictx.Bus()

		var (
			mu                           sync.Mutex
			receivedUser, receivedReason string
		)

		cmdMgr.Register("warn", func(ctx context.Context, senderID uint64, user, reason string) (string, error) {
			mu.Lock()
			receivedUser = user
			receivedReason = reason
			mu.Unlock()

			return "ok", nil
		}, WithArgsSchema(
			Required[string]("user"),
			Required[string]("reason"),
		))

		eb.Publish(&chat.MessageEvent{
			SenderID: UserSteamID,
			Message:  "!warn\t'User\rName'\n'Reason\twith\nnewlines'",
		})

		waitForCalls(t, ictx, 1)

		mu.Lock()
		user := receivedUser
		reason := receivedReason
		mu.Unlock()
		assert.Equal(t, "User\rName", user)
		assert.Equal(t, "Reason\twith\nnewlines", reason)
	})
}

func TestCommandManager_RateLimiter(t *testing.T) {
	t.Parallel()

	t.Run("non_admin_rate_limited", func(t *testing.T) {
		t.Parallel()
		_, cmdMgr, ictx := setupTest(t, t.Context())
		eb := ictx.Bus()

		cmdMgr.Register("ping", func(ctx context.Context, senderID uint64) (string, error) {
			return "", nil
		})

		for range 10 {
			eb.Publish(&chat.MessageEvent{
				SenderID: UserSteamID,
				Message:  "!ping",
			})
			time.Sleep(10 * time.Millisecond)
		}

		waitForCalls(t, ictx, 1)

		req := &pb.CFriendMessages_SendMessage_Request{}

		assert.NotEmpty(t, ictx.MockService().CallsCount())

		if ictx.MockService().GetLastCall(req) != nil {
			assert.Contains(t, req.GetMessage(), "too fast")
		} else {
			t.Fatal("Expected to retrieve a rate limit call from mock service")
		}
	})

	t.Run("admin_bypasses_rate_limiting", func(t *testing.T) {
		t.Parallel()
		_, cmdMgr, ictx := setupTest(t, t.Context())
		eb := ictx.Bus()

		cmdMgr.SetTrustedSteamIDs([]string{strconv.FormatUint(AdminSteamID, 10)})

		cmdMgr.Register("ping", func(ctx context.Context, senderID uint64) (string, error) {
			return "", nil
		})

		for range 10 {
			eb.Publish(&chat.MessageEvent{
				SenderID: AdminSteamID,
				Message:  "!ping",
			})
			time.Sleep(10 * time.Millisecond)
		}

		time.Sleep(150 * time.Millisecond)

		assert.Equal(
			t, 0, ictx.MockService().CallsCount(),
			"Admin should bypass rate limiting completely, making 0 calls",
		)
	})
}

func TestCommandManager_Aliases(t *testing.T) {
	t.Parallel()

	_, cmdMgr, ictx := setupTest(t, t.Context())
	eb := ictx.Bus()

	var (
		mu           sync.Mutex
		receivedUser string
	)

	cmdMgr.Register("warn", func(ctx context.Context, senderID uint64, user string) (string, error) {
		mu.Lock()
		receivedUser = user
		mu.Unlock()

		return "", nil
	}, WithArgsSchema(
		Required[string]("user"),
	), WithAlias("w", "wrn"))

	eb.Publish(&chat.MessageEvent{
		SenderID: UserSteamID,
		Message:  "!w Bob",
	})

	require.Eventually(t, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return receivedUser == "Bob"
	}, 2*time.Second, 5*time.Millisecond)

	mu.Lock()
	user := receivedUser
	mu.Unlock()
	assert.Equal(t, "Bob", user)

	ictx.MockService().ClearCalls()

	eb.Publish(&chat.MessageEvent{
		SenderID: UserSteamID,
		Message:  "!wrn Alice",
	})

	require.Eventually(t, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return receivedUser == "Alice"
	}, 2*time.Second, 5*time.Millisecond)

	mu.Lock()
	user = receivedUser
	mu.Unlock()
	assert.Equal(t, "Alice", user)

	ictx.MockService().ClearCalls()

	eb.Publish(&chat.MessageEvent{
		SenderID: UserSteamID,
		Message:  "!help",
	})

	waitForCalls(t, ictx, 1)

	req := &pb.CFriendMessages_SendMessage_Request{}
	require.True(t, ictx.MockService().GetLastCall(req) != nil)

	helpMessage := req.GetMessage()
	assert.Contains(t, helpMessage, "- !help (aliases: !h)")
	assert.Contains(t, helpMessage, "- !warn (aliases: !w, !wrn) <user:string>")
	assert.NotContains(t, helpMessage, "- !w ")
	assert.NotContains(t, helpMessage, "- !wrn ")
	assert.NotContains(t, helpMessage, "- !h ")
}

func TestCommandManager_UniversalSignature(t *testing.T) {
	t.Parallel()

	_, cmdMgr, ictx := setupTest(t, t.Context())
	eb := ictx.Bus()

	cmdMgr.Register("create", func(ctx context.Context, name string, count int) (string, error) {
		return fmt.Sprintf("Created %d instances of %s", count, name), nil
	})

	eb.Publish(&chat.MessageEvent{
		SenderID: UserSteamID,
		Message:  "!create widget 42",
	})

	waitForCalls(t, ictx, 1)

	req := &pb.CFriendMessages_SendMessage_Request{}
	require.True(t, ictx.MockService().GetLastCall(req) != nil)
	assert.Equal(t, "Created 42 instances of widget", req.GetMessage())
}

func TestCommandManager_ExtraEdgeCases(t *testing.T) {
	t.Parallel()

	caller := SteamCaller{steamID: 76561198000000001, isAdmin: true}
	assert.Equal(t, "76561198000000001", caller.ID())
	assert.Equal(t, "", caller.DisplayName())
	assert.True(t, caller.IsAdmin())

	optSchema := Optional[int]("testOpt")
	assert.True(t, optSchema.Optional)
	assert.Equal(t, "testOpt", optSchema.Name)

	m := NewManager()
	assert.False(t, m.IsAdminCommand("non_existent"))

	m.Register("admin_cmd", func(ctx context.Context) (string, error) {
		return "admin", nil
	}, WithAdmin())
	assert.True(t, m.IsAdminCommand("admin_cmd"))

	m.UpdateCommandDescription("admin_cmd", "updated admin cmd")
	cmd, exists := m.GetCommand("admin_cmd")
	assert.True(t, exists)
	assert.Equal(t, "updated admin cmd", cmd.Description)

	m.UnregisterCommand("admin_cmd")
	_, exists = m.GetCommand("admin_cmd")
	assert.False(t, exists)

	assert.NoError(t, m.Close())
}

func TestRegisterBuiltinCommands(t *testing.T) {
	t.Parallel()

	t.Run("status_builtin_command", func(t *testing.T) {
		t.Parallel()

		cmdMgr := NewManager()
		RegisterBuiltinCommands(cmdMgr, nil, time.Now().Add(-10*time.Second))

		res, err := cmdMgr.engine.Execute(t.Context(), "!status")
		require.NoError(t, err)
		assert.Contains(t, res, "Bot is online. Uptime:")
	})

	t.Run("steamid_builtin_command_nil_manager", func(t *testing.T) {
		t.Parallel()

		cmdMgr := NewManager()
		RegisterBuiltinCommands(cmdMgr, nil, time.Now())

		_, err := cmdMgr.engine.Execute(t.Context(), "!steamid test")
		assert.ErrorContains(t, err, "friends module not available")
	})

	t.Run("steamid_builtin_command_no_matches", func(t *testing.T) {
		t.Parallel()

		cmdMgr := NewManager()
		friendsMgr := &friends.Manager{}
		RegisterBuiltinCommands(cmdMgr, friendsMgr, time.Now())

		res, err := cmdMgr.engine.Execute(t.Context(), "!steamid test")
		require.NoError(t, err)
		assert.Equal(t, "No matching users found.", res)
	})

	t.Run("steamid_builtin_command_matching_users_lte_10", func(t *testing.T) {
		t.Parallel()

		cmdMgr := NewManager()
		friendsMgr := &friends.Manager{}

		friendID := id.ID(76561198000000001)
		populateMockFriends(friendsMgr, friendID, "Alice_test")

		RegisterBuiltinCommands(cmdMgr, friendsMgr, time.Now())

		res, err := cmdMgr.engine.Execute(t.Context(), "!steamid Alice")
		require.NoError(t, err)
		assert.Contains(t, res, "Found 1 match(es):")
		assert.Contains(t, res, "Alice_test")
	})

	t.Run("steamid_builtin_command_matching_users_gt_10", func(t *testing.T) {
		t.Parallel()

		cmdMgr := NewManager()
		friendsMgr := &friends.Manager{}

		for i := range 11 {
			friendID := id.ID(76561198000000000 + uint64(i))
			populateMockFriends(friendsMgr, friendID, fmt.Sprintf("Alice_test_%d", i))
		}

		RegisterBuiltinCommands(cmdMgr, friendsMgr, time.Now())

		res, err := cmdMgr.engine.Execute(t.Context(), "!steamid Alice")
		require.NoError(t, err)
		assert.Contains(t, res, "Found 11 matches (showing first 10):")
	})

	t.Run("profile_builtin_command_nil_manager", func(t *testing.T) {
		t.Parallel()

		cmdMgr := NewManager()
		RegisterBuiltinCommands(cmdMgr, nil, time.Now())

		_, err := cmdMgr.engine.Execute(t.Context(), "!profile 76561198000000001")
		assert.ErrorContains(t, err, "friends module not available")
	})

	t.Run("profile_builtin_command_no_cached_data", func(t *testing.T) {
		t.Parallel()

		cmdMgr := NewManager()
		friendsMgr := &friends.Manager{}
		RegisterBuiltinCommands(cmdMgr, friendsMgr, time.Now())

		res, err := cmdMgr.engine.Execute(t.Context(), "!profile 76561198000000001")
		require.NoError(t, err)
		assert.Contains(t, res, "No cached data for 76561198000000001")
	})

	t.Run("profile_builtin_command_cached_data_exists", func(t *testing.T) {
		t.Parallel()

		cmdMgr := NewManager()
		friendsMgr := &friends.Manager{}

		friendID := id.ID(76561198000000001)
		populateMockFriends(friendsMgr, friendID, "Alice")

		RegisterBuiltinCommands(cmdMgr, friendsMgr, time.Now())

		res, err := cmdMgr.engine.Execute(t.Context(), "!profile 76561198000000001")
		require.NoError(t, err)
		assert.Contains(t, res, "Profile: Alice")
		assert.Contains(t, res, "SteamID: 76561198000000001")
	})
}

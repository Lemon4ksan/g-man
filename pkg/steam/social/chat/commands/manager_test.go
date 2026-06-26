// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package commands

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	pb "github.com/lemon4ksan/g-man/pkg/protobuf/steam"
	"github.com/lemon4ksan/g-man/pkg/steam/id"
	smod "github.com/lemon4ksan/g-man/pkg/steam/module"
	"github.com/lemon4ksan/g-man/pkg/steam/social/chat"
	testmodule "github.com/lemon4ksan/g-man/test/mock"
)

const (
	BotSteamID   = uint64(76561198000000001)
	AdminSteamID = uint64(76561198000000002)
	UserSteamID  = uint64(76561198000000003)
)

type dummyModule struct {
	smod.Base
}

func waitForCalls(t *testing.T, ictx *testmodule.InitContext, expectedCount int) {
	t.Helper()
	require.Eventually(t, func() bool {
		return ictx.MockService().CallsCount() >= expectedCount
	}, 2*time.Second, 5*time.Millisecond)
}

func TestCommandManager_Init(t *testing.T) {
	t.Run("Missing Chat Dependency", func(t *testing.T) {
		m := NewManager()
		ictx := testmodule.NewInitContext() // empty, no modules
		err := m.Init(ictx)
		assert.ErrorContains(t, err, "low-level chat module dependency is missing")
	})

	t.Run("Incorrect Chat Type", func(t *testing.T) {
		m := NewManager()
		ictx := testmodule.NewInitContext()
		// Register a fake module under "chat" with wrong type
		fakeMod := &dummyModule{Base: smod.New("chat")}
		ictx.SetModule("chat", fakeMod)

		err := m.Init(ictx)
		assert.ErrorContains(t, err, "does not implement ChatSender")
	})

	t.Run("Success", func(t *testing.T) {
		m := NewManager()
		ictx := testmodule.NewInitContext()
		chatMod := chat.New()
		ictx.SetModule("chat", chatMod)

		err := m.Init(ictx)
		assert.NoError(t, err)
	})
}

func TestCommandManager_Registration(t *testing.T) {
	m := NewManager()

	handler := func(ctx context.Context, senderID uint64, args []string) (string, error) {
		return "ok", nil
	}

	m.Register("ping", handler)
	m.Register("shutdown", handler, WithAdmin())

	cmd, exists := m.GetCommand("ping")
	assert.True(t, exists)
	assert.False(t, cmd.IsAdmin)

	cmd, exists = m.GetCommand("shutdown")
	assert.True(t, exists)
	assert.True(t, cmd.IsAdmin)

	_, exists = m.GetCommand("invalid")
	assert.False(t, exists)
}

func TestCommandManager_TrustedSteamIDs(t *testing.T) {
	m := NewManager()

	m.SetTrustedSteamIDs([]string{"76561198000000002", "76561198000000004", "invalid_id"})

	assert.True(t, m.IsTrusted(76561198000000002))
	assert.True(t, m.IsTrusted(76561198000000004))
	assert.False(t, m.IsTrusted(76561198000000003)) // not trusted
	assert.False(t, m.IsTrusted(0))
}

func setupTest(t *testing.T, ctx context.Context) (*chat.Chat, *Manager, *testmodule.InitContext) {
	t.Helper()

	chatMod := chat.New()
	cmdMgr := NewManager()

	ictx := testmodule.NewInitContext()
	ictx.SetModule("chat", chatMod)
	ictx.SetModule("chat_commands", cmdMgr)

	require.NoError(t, chatMod.Init(ictx))
	require.NoError(t, cmdMgr.Init(ictx))

	require.NoError(t, chatMod.Start(ctx))
	require.NoError(t, cmdMgr.Start(ctx))

	// Allow goroutine scheduler to start the eventLoop and register the subscription
	time.Sleep(50 * time.Millisecond)

	return chatMod, cmdMgr, ictx
}

func TestCommandManager_EventRouting(t *testing.T) {
	t.Run("Execute Public Command Success", func(t *testing.T) {
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

	t.Run("Execute Admin Command Success", func(t *testing.T) {
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

	t.Run("Execute Admin Command Unauthorized", func(t *testing.T) {
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

	t.Run("Execute Command Failure Output", func(t *testing.T) {
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

	t.Run("Ignore Unregistered Command", func(t *testing.T) {
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
}

func TestCommandManager_HelpCommand(t *testing.T) {
	_, cmdMgr, ictx := setupTest(t, t.Context())
	eb := ictx.Bus()

	cmdMgr.Register("ping", func(ctx context.Context, senderID uint64, args []string) (string, error) {
		return "pong", nil
	}, WithDescription("Simple alive check"))

	cmdMgr.Register("add", func(ctx context.Context, senderID uint64, args []string) (string, error) {
		return "added", nil
	}, WithDescription("Adds two numbers"), WithArgsSchema(
		Required[int]("a"),
		Required[int]("b"),
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
	assert.Contains(t, helpText, "- !add <a:int> <b:int>: Adds two numbers")
}

func TestCommandManager_TypedArguments(t *testing.T) {
	t.Run("Valid Types Parsing", func(t *testing.T) {
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

	t.Run("Invalid Types Parsing Error", func(t *testing.T) {
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

	t.Run("Missing Required Argument Error", func(t *testing.T) {
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
	t.Run("Parse Valid 64-bit SteamID", func(t *testing.T) {
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

	t.Run("Parse Valid Steam3 Format", func(t *testing.T) {
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

	t.Run("Parse Valid Steam2 Format", func(t *testing.T) {
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

	t.Run("Parse Invalid SteamID Error", func(t *testing.T) {
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

	// Verify escaping works inside quotes
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
}

func TestCommandManager_RateLimiter(t *testing.T) {
	ctx := t.Context()

	t.Run("Non-admin rate limited", func(t *testing.T) {
		_, cmdMgr, ictx := setupTest(t, ctx)
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

		assert.NotEmpty(t, ictx.MockService().CallsCount(), "Should have received rate limit responses")

		if ictx.MockService().GetLastCall(req) != nil {
			assert.Contains(t, req.GetMessage(), "too fast")
		} else {
			t.Fatal("Expected to retrieve a rate limit call from mock service")
		}
	})

	t.Run("Admin bypasses rate limiting", func(t *testing.T) {
		_, cmdMgr, ictx := setupTest(t, ctx)
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

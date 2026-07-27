// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/lemon4ksan/miyako/log"

	"github.com/lemon4ksan/g-man/pkg/steam"
	"github.com/lemon4ksan/g-man/pkg/steam/id"
	"github.com/lemon4ksan/g-man/pkg/steam/protocol/enums"
	"github.com/lemon4ksan/g-man/pkg/steam/social/chat"
	"github.com/lemon4ksan/g-man/pkg/steam/social/chat/commands"
	"github.com/lemon4ksan/g-man/pkg/steam/social/friends"
	trading "github.com/lemon4ksan/g-man/pkg/trading/web"
)

var ErrNegativeAmount = errors.New("chatbot: withdrawal amount must be a positive number")

// ChatBot links Steam friend and chat events with the command engine.
type ChatBot struct {
	client *steam.Client
	logger log.Logger
}

// NewChatBot constructs a ChatBot instance.
func NewChatBot(client *steam.Client, logger log.Logger) *ChatBot {
	return &ChatBot{
		client: client,
		logger: logger.With(log.Module("admin_bot")),
	}
}

// RegisterCommands registers builtin and admin commands on the client command engine.
func (bot *ChatBot) RegisterCommands() {
	friendsMgr := friends.From(bot.client)
	cmdManager := commands.From(bot.client)

	commands.RegisterBuiltinCommands(cmdManager, friendsMgr, time.Now())

	cmdManager.Register("withdraw", bot.handleWithdraw,
		commands.WithDescription("Requests a transfer of funds to the specified SteamID"),
		commands.WithArgsSchema(
			commands.Required[id.ID]("target_id"),
			commands.Required[float64]("amount"),
		),
		commands.WithAdmin(),
	)

	cmdManager.Register("approve", bot.handleApprove,
		commands.WithDescription("Forcibly approves an active trade offer by ID"),
		commands.WithArgsSchema(
			commands.Required[uint64]("offer_id"),
		),
		commands.WithAdmin(),
	)
}

// ListenEvents subscribes to relationship events and handles incoming friend requests.
func (bot *ChatBot) ListenEvents(ctx context.Context) {
	sub := bot.client.Bus().Subscribe(
		&friends.RelationshipChangedEvent{},
	)

	go func() {
		defer sub.Unsubscribe()

		for {
			select {
			case <-ctx.Done():
				return

			case ev, ok := <-sub.C():
				if !ok {
					return
				}

				if e, ok := ev.(*friends.RelationshipChangedEvent); ok {
					bot.handleFriendRequest(ctx, e)
				}
			}
		}
	}()
}

func (bot *ChatBot) handleFriendRequest(ctx context.Context, e *friends.RelationshipChangedEvent) {
	friendsMgr := friends.From(bot.client)

	if e.New == enums.EFriendRelationship_RequestInitiator {
		bot.logger.Info("Received incoming friend request", log.String("steam_id", e.SteamID.String()))

		err := friendsMgr.AcceptFriendRequestWeb(ctx, e.SteamID)
		if err != nil {
			bot.logger.Error("Failed to accept friend request", log.Err(err))
			return
		}

		_ = chat.From(bot.client).SendMessage(
			ctx, e.SteamID.Uint64(),
			"Hello! I am a trading bot. Type !help for a list of commands.",
		)
	}
}

func (bot *ChatBot) handleWithdraw(_ context.Context, senderID uint64, args []any) (string, error) {
	targetID := args[0].(id.ID)
	amount := args[1].(float64)

	if amount <= 0 {
		return "", ErrNegativeAmount
	}

	bot.logger.Warn("Withdraw executed by admin",
		log.Uint64("admin_id", senderID),
		log.String("target_id", targetID.String()),
		log.Float64("amount", amount),
	)

	return fmt.Sprintf(
		"✅ Withdrawal transaction of %0.2f has been successfully queued for %s",
		amount, targetID.String(),
	), nil
}

func (bot *ChatBot) handleApprove(ctx context.Context, senderID uint64, args []any) (string, error) {
	offerID := args[0].(uint64)

	err := trading.From(bot.client).AcceptOffer(ctx, offerID)
	if err != nil {
		return "", fmt.Errorf("failed to approve trade #%d: %w", offerID, err)
	}

	bot.logger.Info("Admin approved trade manual", log.Uint64("admin", senderID), log.Uint64("offer", offerID))

	return fmt.Sprintf("✅ Trade #%d has been successfully confirmed and sent for verification.", offerID), nil
}

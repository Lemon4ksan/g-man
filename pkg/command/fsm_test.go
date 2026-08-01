// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package command_test

import (
	"context"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lemon4ksan/g-man/pkg/command"
)

func TestConversationManager_DialogFlow(t *testing.T) {
	cm := command.NewConversationManager(1 * time.Minute)

	// Step 1: Ask amount
	cm.RegisterState(
		"WAITING_AMOUNT",
		func(ctx context.Context, input string, session *command.Session) (string, string, error) {
			amount, err := strconv.Atoi(input)
			if err != nil {
				return "WAITING_AMOUNT", "Invalid number. How many keys do you want to buy?", nil //nolint:nilerr
			}

			session.Set("amount", amount)

			return "WAITING_CONFIRM", "Confirm purchase of " + strconv.Itoa(amount) + " keys? (yes/no)", nil
		},
	)

	// Step 2: Confirm
	cm.RegisterState(
		"WAITING_CONFIRM",
		func(ctx context.Context, input string, session *command.Session) (string, string, error) {
			if input == "yes" {
				amount, _ := session.Get("amount")
				return "", "Purchase of " + strconv.Itoa(amount.(int)) + " keys completed!", nil
			}

			return "", "Purchase cancelled.", nil
		},
	)

	userID := "12345678"

	// Start dialog
	cm.SetState(userID, "WAITING_AMOUNT", nil)

	// Step 1 input: invalid
	handled, resp, err := cm.HandleInput(context.Background(), userID, "abc")
	require.NoError(t, err)
	assert.True(t, handled)
	assert.Equal(t, "Invalid number. How many keys do you want to buy?", resp)

	// Step 1 input: valid number 5
	handled, resp, err = cm.HandleInput(context.Background(), userID, "5")
	require.NoError(t, err)
	assert.True(t, handled)
	assert.Equal(t, "Confirm purchase of 5 keys? (yes/no)", resp)

	// Step 2 input: confirm
	handled, resp, err = cm.HandleInput(context.Background(), userID, "yes")
	require.NoError(t, err)
	assert.True(t, handled)
	assert.Equal(t, "Purchase of 5 keys completed!", resp)

	// Dialog should now be finished
	_, exists := cm.GetState(userID)
	assert.False(t, exists)
}

func TestConversationManager_Cancel(t *testing.T) {
	cm := command.NewConversationManager(1 * time.Minute)

	userID := "87654321"
	cm.SetState(userID, "SOME_STATE", nil)

	handled, resp, err := cm.HandleInput(context.Background(), userID, "!cancel")
	require.NoError(t, err)
	assert.True(t, handled)
	assert.Equal(t, "Conversation cancelled.", resp)

	_, exists := cm.GetState(userID)
	assert.False(t, exists)
}

func TestConversationManager_TTL(t *testing.T) {
	cm := command.NewConversationManager(10 * time.Millisecond)

	userID := "ttl_user"
	cm.SetState(userID, "STATE_1", nil)

	time.Sleep(20 * time.Millisecond)

	handled, _, _ := cm.HandleInput(context.Background(), userID, "hello")
	assert.False(t, handled)
}

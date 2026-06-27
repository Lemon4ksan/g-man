// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package service

import (
	"encoding/json"
	"io"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/emptypb"

	"github.com/lemon4ksan/g-man/pkg/steam/protocol/enums"
)

func TestUnifiedTarget(t *testing.T) {
	t.Parallel()

	t.Run("Formatting and Setters", func(t *testing.T) {
		t.Parallel()

		target := &UnifiedTarget{
			Interface: "Player",
			Method:    "GetNickname",
			Version:   1,
			IsService: true,
		}

		assert.Equal(t, "Player.GetNickname#1", target.String())
		assert.Equal(t, target.String(), target.ObjectName())
		assert.Equal(t, "POST", target.HTTPMethod(), "Should default to POST")

		target.SetHTTPMethod("GET")
		assert.Equal(t, "GET", target.HTTPMethod())

		target.SetVersion(2)
		assert.Equal(t, 2, target.Version)
	})

	t.Run("HTTPPath Logic", func(t *testing.T) {
		t.Parallel()

		tests := []struct {
			name      string
			iface     string
			isService bool
			expected  string
		}{
			{
				name:      "Add I and Service",
				iface:     "Player",
				isService: true,
				expected:  "IPlayerService/Get/v1",
			},
			{
				name:      "Already has I, add Service",
				iface:     "IInventory",
				isService: true,
				expected:  "IInventoryService/Get/v1",
			},
			{
				name:      "Add I, already has Service",
				iface:     "ChatService",
				isService: true,
				expected:  "IChatService/Get/v1",
			},
			{
				name:      "Not a Service",
				iface:     "PublishedFile",
				isService: false,
				expected:  "IPublishedFile/Get/v1",
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				u := &UnifiedTarget{
					Interface: tt.iface,
					Method:    "Get",
					Version:   1,
					IsService: tt.isService,
				}
				assert.Equal(t, tt.expected, u.HTTPPath())
			})
		}
	})

	t.Run("EMsg Branching", func(t *testing.T) {
		t.Parallel()

		u := &UnifiedTarget{}
		assert.Equal(t, enums.EMsg_ServiceMethodCallFromClient, u.EMsg(true))
		assert.Equal(t, enums.EMsg_ServiceMethodCallFromClientNonAuthed, u.EMsg(false))
	})
}

func TestNewUnifiedRequest(t *testing.T) {
	t.Parallel()

	t.Run("Nil Message", func(t *testing.T) {
		t.Parallel()

		req, err := NewUnifiedRequest("POST", "I", "M", 1, nil)
		require.NoError(t, err)

		bodyBytes, _ := io.ReadAll(req.Body)
		assert.Empty(t, bodyBytes)
	})

	t.Run("Proto Message", func(t *testing.T) {
		t.Parallel()

		msg := &emptypb.Empty{}
		req, err := NewUnifiedRequest("POST", "I", "M", 1, msg)
		require.NoError(t, err)

		expected, _ := proto.Marshal(msg)
		actual, _ := io.ReadAll(req.Body)
		assert.Equal(t, expected, actual)
	})

	t.Run("Byte Slice", func(t *testing.T) {
		t.Parallel()

		raw := []byte{0xDE, 0xAD}
		req, err := NewUnifiedRequest("POST", "I", "M", 1, raw)
		require.NoError(t, err)

		actual, _ := io.ReadAll(req.Body)
		assert.Equal(t, raw, actual)
	})

	t.Run("JSON Struct", func(t *testing.T) {
		t.Parallel()

		data := struct{ ID int }{ID: 10}
		req, err := NewUnifiedRequest("POST", "I", "M", 1, data)
		require.NoError(t, err)

		expected, _ := json.Marshal(data)
		actual, _ := io.ReadAll(req.Body)
		assert.Equal(t, expected, actual)
	})

	t.Run("JSON Error", func(t *testing.T) {
		t.Parallel()

		_, err := NewUnifiedRequest("POST", "I", "M", 1, make(chan int))
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to encode unified body")
	})
}

func TestWebAPITarget(t *testing.T) {
	t.Parallel()

	w := &WebAPITarget{
		HttpMethod: "GET",
		Interface:  "ISteamUser",
		Method:     "GetPlayer",
		Version:    2,
	}

	assert.Equal(t, "ISteamUser/GetPlayer", w.String())
	assert.Equal(t, "GET", w.HTTPMethod())
	assert.Equal(t, "ISteamUser/GetPlayer/v2", w.HTTPPath())
	assert.Equal(t, "ISteamUser/GetPlayer", w.ObjectName())

	w.SetHTTPMethod("POST")
	assert.Equal(t, "POST", w.HttpMethod)

	w.SetVersion(3)
	assert.Equal(t, 3, w.Version)
}

func TestLegacyTarget(t *testing.T) {
	t.Parallel()

	t.Run("NewLegacyRequest Success", func(t *testing.T) {
		t.Parallel()

		msg := &emptypb.Empty{}
		req, err := NewLegacyRequest(enums.EMsg_ClientLogon, msg)
		require.NoError(t, err)

		target := req.Target().(*LegacyTarget)
		assert.Equal(t, enums.EMsg_ClientLogon, target.EMsg(true))
		assert.Equal(t, enums.EMsg_ClientLogon.String(), target.String())
		assert.Empty(t, target.ObjectName())

		expected, _ := proto.Marshal(msg)
		actual, _ := io.ReadAll(req.Body)
		assert.Equal(t, expected, actual)
	})

	t.Run("NewLegacyRequest Nil Message", func(t *testing.T) {
		t.Parallel()

		req, err := NewLegacyRequest(enums.EMsg_ClientLogon, nil)
		require.NoError(t, err)

		bodyBytes, _ := io.ReadAll(req.Body)
		assert.Empty(t, bodyBytes)
	})
}

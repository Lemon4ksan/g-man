// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package id_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lemon4ksan/g-man/pkg/steam/id"
	"github.com/lemon4ksan/g-man/test/mock"
)

func TestEnums(t *testing.T) {
	t.Run("Universe Stringer", func(t *testing.T) {
		assert.Equal(t, "Invalid", id.UniverseInvalid.String())
		assert.Equal(t, "Public", id.UniversePublic.String())
		assert.Equal(t, "Beta", id.UniverseBeta.String())
		assert.Equal(t, "Internal", id.UniverseInternal.String())
		assert.Equal(t, "Dev", id.UniverseDev.String())
		assert.Equal(t, "Universe(100)", id.Universe(100).String())
	})

	t.Run("AccountType Stringer", func(t *testing.T) {
		assert.Equal(t, "Invalid", id.AccountTypeInvalid.String())
		assert.Equal(t, "Individual", id.AccountTypeIndividual.String())
		assert.Equal(t, "Multiseat", id.AccountTypeMultiseat.String())
		assert.Equal(t, "GameServer", id.AccountTypeGameServer.String())
		assert.Equal(t, "AnonGameServer", id.AccountTypeAnonGameServer.String())
		assert.Equal(t, "Pending", id.AccountTypePending.String())
		assert.Equal(t, "ContentServer", id.AccountTypeContentServer.String())
		assert.Equal(t, "Clan", id.AccountTypeClan.String())
		assert.Equal(t, "Chat", id.AccountTypeChat.String())
		assert.Equal(t, "ConsoleUser", id.AccountTypeConsoleUser.String())
		assert.Equal(t, "AnonUser", id.AccountTypeAnonUser.String())
		assert.Equal(t, "AccountType(100)", id.AccountType(100).String())
	})
}

func TestID_Basics(t *testing.T) {
	raw := uint64(76561198044393456)
	sid := id.New(raw)

	assert.Equal(t, raw, sid.Uint64())
	assert.Equal(t, "76561198044393456", sid.String())
	assert.Equal(t, uint32(84127728), sid.AccountID())
	assert.Equal(t, uint32(1), sid.Instance())
	assert.Equal(t, id.UniversePublic, sid.Universe())
	assert.Equal(t, id.AccountTypeIndividual, sid.Type())

	// FromAccountID
	assert.Equal(t, sid, id.FromAccountID(84127728))
}

func TestParse(t *testing.T) {
	expected := id.ID(76561198044393456)

	tests := []struct {
		name  string
		input string
		want  id.ID
	}{
		{"Empty", "", id.InvalidID},
		{"Steam64", "76561198044393456", expected},
		{"Steam2_0", "STEAM_0:0:42063864", expected},
		{"Steam2_1", "STEAM_0:1:42063864", id.ID(76561198044393457)},
		{"Steam3", "[U:1:84127728]", expected},
		{"Steam3WithInstance", "[U:1:84127728:1]", expected},
		{"Garbage", "abc-123", id.InvalidID},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, id.Parse(tt.input))
		})
	}
}

func TestID_IsValid(t *testing.T) {
	assert.True(t, id.ID(76561198044393456).IsValid())
	assert.False(t, id.InvalidID.IsValid())
	// Test boundary of Universe
	assert.False(t, id.ID(uint64(id.UniverseInvalid)<<56).IsValid())
	// Test boundary of AccountType
	assert.False(t, id.ID(uint64(id.UniversePublic)<<56).IsValid())
}

func TestID_Formatting(t *testing.T) {
	sid := id.ID(76561198044393456)
	assert.Equal(t, "STEAM_0:0:42063864", sid.Steam2())
	assert.Equal(t, "[U:1:84127728]", sid.Steam3())
}

func TestID_JSON(t *testing.T) {
	sid := id.ID(76561198044393456)

	t.Run("Marshal", func(t *testing.T) {
		data, err := json.Marshal(sid)
		require.NoError(t, err)
		assert.Equal(t, `"76561198044393456"`, string(data))
	})

	t.Run("Unmarshal Success", func(t *testing.T) {
		var out id.ID
		// String
		require.NoError(t, json.Unmarshal([]byte(`"76561198044393456"`), &out))
		assert.Equal(t, sid, out)
		// Number
		require.NoError(t, json.Unmarshal([]byte(`76561198044393456`), &out))
		assert.Equal(t, sid, out)
	})

	t.Run("Unmarshal Null/Empty", func(t *testing.T) {
		var out id.ID
		require.NoError(t, json.Unmarshal([]byte(`null`), &out))
		assert.Equal(t, id.InvalidID, out)

		// Manual check of UnmarshalJSON with empty slice
		err := out.UnmarshalJSON([]byte{})
		assert.NoError(t, err)
		assert.Equal(t, id.InvalidID, out)
	})

	t.Run("Unmarshal Error", func(t *testing.T) {
		var out id.ID

		err := json.Unmarshal([]byte(`"not-a-number"`), &out)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "invalid json value")
	})
}

func TestResolve(t *testing.T) {
	ctx := context.Background()
	mock := mock.NewServiceMock()

	t.Run("Valid Direct ID", func(t *testing.T) {
		sid, err := id.Resolve(ctx, mock, " 76561198044393456 ")
		require.NoError(t, err)
		assert.Equal(t, id.ID(76561198044393456), sid)
	})

	t.Run("Profile URL with ID", func(t *testing.T) {
		sid, err := id.Resolve(ctx, mock, "https://steamcommunity.com/profiles/76561198044393456")
		require.NoError(t, err)
		assert.Equal(t, id.ID(76561198044393456), sid)
	})

	t.Run("Vanity URL Success", func(t *testing.T) {
		mock.SetJSONResponse("ISteamUser", "ResolveVanityURL", map[string]any{
			"response": map[string]any{
				"success": 1,
				"steamid": "76561198044393456",
			},
		})
		sid, err := id.Resolve(ctx, mock, "steamcommunity.com/id/lemon4ksan")
		require.NoError(t, err)
		assert.Equal(t, id.ID(76561198044393456), sid)
	})

	t.Run("Vanity URL slug is ID", func(t *testing.T) {
		// This tests the branch: if id := Parse(slug); id.IsValid() { return id, nil }
		sid, err := id.Resolve(ctx, mock, "https://steamcommunity.com/id/76561198044393456")
		require.NoError(t, err)
		assert.Equal(t, id.ID(76561198044393456), sid)
	})

	t.Run("Invalid Format Error", func(t *testing.T) {
		_, err := id.Resolve(ctx, mock, "google.com")
		assert.Error(t, err)
		assert.Equal(t, "steamid: invalid input format", err.Error())
	})
}

func TestResolveVanityURL(t *testing.T) {
	ctx := context.Background()

	t.Run("WebAPI Error", func(t *testing.T) {
		mock := mock.NewServiceMock()
		mock.ResponseErrs["ISteamUser/ResolveVanityURL"] = errors.New("network fail")
		_, err := id.ResolveVanityURL(ctx, mock, "test")
		assert.Error(t, err)
		assert.Equal(t, "network fail", err.Error())
	})

	t.Run("Steam Success False", func(t *testing.T) {
		mock := mock.NewServiceMock()
		mock.SetJSONResponse("ISteamUser", "ResolveVanityURL", map[string]any{
			"response": map[string]any{
				"success": 42,
				"message": "No match found",
			},
		})
		_, err := id.ResolveVanityURL(ctx, mock, "test")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "could not resolve vanity URL")
		assert.Contains(t, err.Error(), "success=42")
	})
}

func TestParseTradeURL(t *testing.T) {
	t.Run("empty URL", func(t *testing.T) {
		_, _, err := id.ParseTradeURL("")
		assert.Error(t, err)
		assert.Equal(t, "trade url is empty", err.Error())
	})

	t.Run("invalid URL syntax", func(t *testing.T) {
		_, _, err := id.ParseTradeURL("http://[fe80::%d]/")
		assert.Error(t, err)
	})

	t.Run("missing partner parameter", func(t *testing.T) {
		_, _, err := id.ParseTradeURL("https://steamcommunity.com/tradeoffer/new/?token=abc")
		assert.Error(t, err)
		assert.Equal(t, "missing partner parameter in trade URL", err.Error())
	})

	t.Run("invalid partner integer", func(t *testing.T) {
		_, _, err := id.ParseTradeURL("https://steamcommunity.com/tradeoffer/new/?partner=not-an-int&token=abc")
		assert.Error(t, err)
	})

	t.Run("valid trade URL", func(t *testing.T) {
		partnerID, token, err := id.ParseTradeURL("https://steamcommunity.com/tradeoffer/new/?partner=12345&token=abc")
		assert.NoError(t, err)
		assert.Equal(t, "abc", token)
		assert.True(t, partnerID.IsValid())
		assert.Equal(t, uint32(12345), partnerID.AccountID())
	})
}

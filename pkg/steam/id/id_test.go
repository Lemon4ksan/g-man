// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package id_test

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lemon4ksan/g-man/pkg/steam/id"
	"github.com/lemon4ksan/g-man/test/mock"
)

func TestEnums(t *testing.T) {
	t.Parallel()

	t.Run("universe_stringer", func(t *testing.T) {
		t.Parallel()
		assert.Equal(t, "Invalid", id.UniverseInvalid.String())
		assert.Equal(t, "Public", id.UniversePublic.String())
		assert.Equal(t, "Beta", id.UniverseBeta.String())
		assert.Equal(t, "Internal", id.UniverseInternal.String())
		assert.Equal(t, "Dev", id.UniverseDev.String())
		assert.Equal(t, "Universe(100)", id.Universe(100).String())
	})

	t.Run("account_type_stringer", func(t *testing.T) {
		t.Parallel()
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
	t.Parallel()

	raw := uint64(76561198044393456)
	sid := id.New(raw)

	assert.Equal(t, raw, sid.Uint64())
	assert.Equal(t, "76561198044393456", sid.String())
	assert.Equal(t, uint32(84127728), sid.AccountID())
	assert.Equal(t, uint32(1), sid.Instance())
	assert.Equal(t, id.UniversePublic, sid.Universe())
	assert.Equal(t, id.AccountTypeIndividual, sid.Type())

	assert.Equal(t, sid, id.FromAccountID(84127728))
}

func TestParse(t *testing.T) {
	t.Parallel()

	expected := id.ID(76561198044393456)

	tests := []struct {
		name  string
		input string
		want  id.ID
	}{
		{"empty", "", id.InvalidID},
		{"steam64", "76561198044393456", expected},
		{"steam2_0", "STEAM_0:0:42063864", expected},
		{"steam2_1", "STEAM_0:1:42063864", id.ID(76561198044393457)},
		{"steam3", "[U:1:84127728]", expected},
		{"steam3_with_instance", "[U:1:84127728:1]", expected},
		{"garbage", "abc-123", id.InvalidID},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, id.Parse(tt.input))
		})
	}
}

func TestID_IsValid(t *testing.T) {
	t.Parallel()

	assert.True(t, id.ID(76561198044393456).IsValid())
	assert.False(t, id.InvalidID.IsValid())
	assert.False(t, id.ID(uint64(id.UniverseInvalid)<<56).IsValid())
	assert.False(t, id.ID(uint64(id.UniversePublic)<<56).IsValid())
}

func TestID_Formatting(t *testing.T) {
	t.Parallel()

	sid := id.ID(76561198044393456)
	assert.Equal(t, "STEAM_0:0:42063864", sid.Steam2())
	assert.Equal(t, "[U:1:84127728]", sid.Steam3())
}

func TestID_JSON(t *testing.T) {
	t.Parallel()

	sid := id.ID(76561198044393456)

	t.Run("marshal", func(t *testing.T) {
		t.Parallel()

		data, err := json.Marshal(sid)
		require.NoError(t, err)
		assert.Equal(t, `"76561198044393456"`, string(data))
	})

	t.Run("unmarshal_success", func(t *testing.T) {
		t.Parallel()

		var out id.ID
		require.NoError(t, json.Unmarshal([]byte(`"76561198044393456"`), &out))
		assert.Equal(t, sid, out)

		require.NoError(t, json.Unmarshal([]byte(`76561198044393456`), &out))
		assert.Equal(t, sid, out)
	})

	t.Run("unmarshal_null_empty", func(t *testing.T) {
		t.Parallel()

		var out id.ID
		require.NoError(t, json.Unmarshal([]byte(`null`), &out))
		assert.Equal(t, id.InvalidID, out)

		err := out.UnmarshalJSON([]byte{})
		assert.NoError(t, err)
		assert.Equal(t, id.InvalidID, out)
	})

	t.Run("unmarshal_error", func(t *testing.T) {
		t.Parallel()

		var out id.ID

		err := json.Unmarshal([]byte(`"not-a-number"`), &out)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "invalid json value")
	})
}

func TestResolve(t *testing.T) {
	t.Parallel()

	t.Run("valid_direct_id", func(t *testing.T) {
		t.Parallel()

		mockSvc := mock.NewServiceMock()
		sid, err := id.Resolve(t.Context(), mockSvc, " 76561198044393456 ")
		require.NoError(t, err)
		assert.Equal(t, id.ID(76561198044393456), sid)
	})

	t.Run("profile_url_with_id", func(t *testing.T) {
		t.Parallel()

		mockSvc := mock.NewServiceMock()
		sid, err := id.Resolve(t.Context(), mockSvc, "https://steamcommunity.com/profiles/76561198044393456")
		require.NoError(t, err)
		assert.Equal(t, id.ID(76561198044393456), sid)
	})

	t.Run("vanity_url_success", func(t *testing.T) {
		t.Parallel()

		mockSvc := mock.NewServiceMock()
		mockSvc.SetJSONResponse("ISteamUser", "ResolveVanityURL", map[string]any{
			"response": map[string]any{
				"success": 1,
				"steamid": "76561198044393456",
			},
		})
		sid, err := id.Resolve(t.Context(), mockSvc, "steamcommunity.com/id/lemon4ksan")
		require.NoError(t, err)
		assert.Equal(t, id.ID(76561198044393456), sid)
	})

	t.Run("vanity_url_slug_is_id", func(t *testing.T) {
		t.Parallel()

		mockSvc := mock.NewServiceMock()
		sid, err := id.Resolve(t.Context(), mockSvc, "https://steamcommunity.com/id/76561198044393456")
		require.NoError(t, err)
		assert.Equal(t, id.ID(76561198044393456), sid)
	})

	t.Run("invalid_format_error", func(t *testing.T) {
		t.Parallel()

		mockSvc := mock.NewServiceMock()
		_, err := id.Resolve(t.Context(), mockSvc, "google.com")
		assert.Error(t, err)
		assert.Equal(t, "steamid: invalid input format", err.Error())
	})
}

func TestResolveVanityURL(t *testing.T) {
	t.Parallel()

	t.Run("webapi_error", func(t *testing.T) {
		t.Parallel()

		mockSvc := mock.NewServiceMock()
		mockSvc.ResponseErrs["ISteamUser/ResolveVanityURL"] = errors.New("network fail")
		_, err := id.ResolveVanityURL(t.Context(), mockSvc, "test")
		assert.Error(t, err)
		assert.Equal(t, "network fail", err.Error())
	})

	t.Run("steam_success_false", func(t *testing.T) {
		t.Parallel()

		mockSvc := mock.NewServiceMock()
		mockSvc.SetJSONResponse("ISteamUser", "ResolveVanityURL", map[string]any{
			"response": map[string]any{
				"success": 42,
				"message": "No match found",
			},
		})
		_, err := id.ResolveVanityURL(t.Context(), mockSvc, "test")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "could not resolve vanity URL")
		assert.Contains(t, err.Error(), "success=42")
	})
}

func TestParseTradeURL(t *testing.T) {
	t.Parallel()

	t.Run("empty_url", func(t *testing.T) {
		t.Parallel()

		_, _, err := id.ParseTradeURL("")
		assert.Error(t, err)
		assert.Equal(t, "trade url is empty", err.Error())
	})

	t.Run("invalid_url_syntax", func(t *testing.T) {
		t.Parallel()

		_, _, err := id.ParseTradeURL("http://[fe80::%d]/")
		assert.Error(t, err)
	})

	t.Run("missing_partner_parameter", func(t *testing.T) {
		t.Parallel()

		_, _, err := id.ParseTradeURL("https://steamcommunity.com/tradeoffer/new/?token=abc")
		assert.Error(t, err)
		assert.Equal(t, "missing partner parameter in trade URL", err.Error())
	})

	t.Run("invalid_partner_integer", func(t *testing.T) {
		t.Parallel()

		_, _, err := id.ParseTradeURL("https://steamcommunity.com/tradeoffer/new/?partner=not-an-int&token=abc")
		assert.Error(t, err)
	})

	t.Run("valid_trade_url", func(t *testing.T) {
		t.Parallel()

		partnerID, token, err := id.ParseTradeURL("https://steamcommunity.com/tradeoffer/new/?partner=12345&token=abc")
		assert.NoError(t, err)
		assert.Equal(t, "abc", token)
		assert.True(t, partnerID.IsValid())
		assert.Equal(t, uint32(12345), partnerID.AccountID())
	})
}

// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package id_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/lemon4ksan/g-man/pkg/steam/id"
	"github.com/lemon4ksan/g-man/test/requester"
)

func TestParse(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected id.ID
	}{
		{"Empty", "", id.InvalidID},
		{"SteamID64", "76561198044393456", id.ID(76561198044393456)},
		{"Steam2", "STEAM_0:0:42063864", id.ID(76561198044393456)},
		{"Steam3", "[U:1:84127728]", id.ID(76561198044393456)},
		{"Invalid String", "not_an_id", id.InvalidID},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := id.Parse(tt.input); got != tt.expected {
				t.Errorf("Parse(%s) = %v, want %v", tt.input, got, tt.expected)
			}
		})
	}
}

func TestID_Components(t *testing.T) {
	sid := id.ID(76561198044393456)

	if sid.AccountID() != 84127728 {
		t.Errorf("Expected AccountID 84127728, got %d", sid.AccountID())
	}

	if sid.Universe() != id.UniversePublic {
		t.Errorf("Expected Universe Public, got %v", sid.Universe())
	}

	if sid.Type() != id.AccountTypeIndividual {
		t.Errorf("Expected Type Individual, got %v", sid.Type())
	}

	if sid.Instance() != 1 {
		t.Errorf("Expected Instance 1, got %d", sid.Instance())
	}
}

func TestID_Formatting(t *testing.T) {
	sid := id.ID(76561198044393456)

	if sid.Steam2() != "STEAM_0:0:42063864" {
		t.Errorf("Steam2() = %s", sid.Steam2())
	}

	if sid.Steam3() != "[U:1:84127728]" {
		t.Errorf("Steam3() = %s", sid.Steam3())
	}

	if sid.String() != "76561198044393456" {
		t.Errorf("String() = %s", sid.String())
	}
}

func TestID_Valid(t *testing.T) {
	if !id.ID(76561198044393456).Valid() {
		t.Error("Standard ID should be valid")
	}
	if id.InvalidID.Valid() {
		t.Error("InvalidID should be invalid")
	}
}

func TestID_JSON(t *testing.T) {
	sid := id.ID(76561198044393456)

	// Test Marshal (Should be a string to prevent JS precision loss)
	data, err := json.Marshal(sid)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != `"76561198044393456"` {
		t.Errorf("JSON Marshal = %s", string(data))
	}

	// Test Unmarshal (Should support strings)
	var decoded id.ID
	if err := json.Unmarshal([]byte(`"76561198044393456"`), &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded != sid {
		t.Errorf("JSON Unmarshal = %v", decoded)
	}

	// Test Unmarshal (Should support numbers for backward compatibility)
	if err := json.Unmarshal([]byte(`76561198044393456`), &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded != sid {
		t.Errorf("JSON Unmarshal (number) = %v", decoded)
	}
}

func TestResolve(t *testing.T) {
	ctx := context.Background()
	mock := requester.New()

	t.Run("Direct ID", func(t *testing.T) {
		got, err := id.Resolve(ctx, mock, "76561198044393456")
		if err != nil {
			t.Fatal(err)
		}
		if got.Uint64() != 76561198044393456 {
			t.Errorf("Expected same ID back")
		}
	})

	t.Run("Profile URL", func(t *testing.T) {
		got, err := id.Resolve(ctx, mock, "https://steamcommunity.com/profiles/76561198044393456")
		if err != nil {
			t.Fatal(err)
		}
		if got.Uint64() != 76561198044393456 {
			t.Errorf("Expected ID extracted from URL")
		}
	})

	t.Run("Vanity URL Success", func(t *testing.T) {
		vanityName := "lemon4ksan"
		expectedID := "76561198044393456"

		mock.SetJSONResponse("ISteamUser", "ResolveVanityURL", map[string]any{
			"response": map[string]any{
				"success": 1,
				"steamid": expectedID,
			},
		})

		got, err := id.Resolve(ctx, mock, "https://steamcommunity.com/id/"+vanityName)
		if err != nil {
			t.Fatal(err)
		}
		if got.String() != expectedID {
			t.Errorf("Expected %s, got %v", expectedID, got)
		}
	})

	t.Run("Vanity URL Not Found", func(t *testing.T) {
		mock.SetJSONResponse("ISteamUser", "ResolveVanityURL", map[string]any{
			"response": map[string]any{
				"success": 42,
				"message": "No match",
			},
		})

		_, err := id.Resolve(ctx, mock, "https://steamcommunity.com/id/unknown_user")
		if err == nil {
			t.Fatal("Expected error for non-existent vanity URL")
		}
	})
}

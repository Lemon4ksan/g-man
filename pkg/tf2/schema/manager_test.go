// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package tf2schema

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/lemon4ksan/g-man/pkg/steam/bus"
	tr "github.com/lemon4ksan/g-man/pkg/steam/transport"
	"github.com/lemon4ksan/g-man/test"
)

func TestNewSchemaManager_ConfigDefaults(t *testing.T) {
	cfg := Config{UpdateInterval: 10 * time.Second}
	sm := New(cfg)

	if sm.config.UpdateInterval != 24*time.Hour {
		t.Errorf("expected 24h interval, got %v", sm.config.UpdateInterval)
	}

	cfgValid := Config{UpdateInterval: 5 * time.Minute}
	smValid := New(cfgValid)
	if smValid.config.UpdateInterval != 5*time.Minute {
		t.Errorf("expected 5m interval, got %v", smValid.config.UpdateInterval)
	}
}

func TestSchemaManager_LiteModePruning(t *testing.T) {
	cfg := Config{LiteMode: true}
	sm := New(cfg)

	raw := &RawSchema{
		ItemsGame: map[string]any{
			"prefabs":         map[string]any{"test": 1},
			"items":           map[string]any{"1": "test_item"},
			"equip_conflicts": map[string]any{"test": 2},
		},
	}

	sm.pruneItemsGame(raw)

	if _, exists := raw.ItemsGame["prefabs"]; exists {
		t.Error("expected 'prefabs' to be pruned")
	}
	if _, exists := raw.ItemsGame["equip_conflicts"]; exists {
		t.Error("expected 'equip_conflicts' to be pruned")
	}
	if _, exists := raw.ItemsGame["items"]; !exists {
		t.Error("expected 'items' to be kept")
	}
}

func TestSchemaManager_Refresh_Success(t *testing.T) {
	mockAPI := test.NewMockRequester()

	mockAPI.SetJSONResponse("IEconItems_440", "GetSchemaOverview", map[string]any{
		"result": map[string]any{
			"qualities": map[string]any{"Normal": 0, "Genuine": 1},
		},
	})

	mockAPI.SetJSONResponse("IEconItems_440", "GetSchemaItems", map[string]any{
		"result": map[string]any{
			"items": []any{
				map[string]any{"defindex": 5021, "name": "Mann Co. Supply Crate Key"},
			},
			"next": 0,
		},
	})

	mockAPI.OnDo = func(req *tr.Request) (*tr.Response, error) {
		target := req.Target().String()

		if strings.Contains(target, "paint_kits") || strings.Contains(target, "proto_obj") {
			vdf := "\"lang\"\n{\n\t\"Tokens\"\n\t{\n\t\t\"9_12_weapon 12\" \"Nutcracker\"\n\t}\n}\n"
			return tr.NewResponse([]byte(vdf), tr.HTTPMetadata{StatusCode: 200}), nil
		}

		if strings.Contains(target, "items_game") {
			vdf := "\"items_game\"\n{\n\t\"valid_key\" \"value\"\n}\n"
			return tr.NewResponse([]byte(vdf), tr.HTTPMetadata{StatusCode: 200}), nil
		}

		return nil, nil
	}

	sm := New(Config{LiteMode: false})
	sm.client = mockAPI
	sm.bus = bus.NewBus()

	ctx := context.Background()

	err := sm.Refresh(ctx)
	if err != nil {
		t.Fatalf("unexpected error during Refresh: %v", err)
	}

	schema := sm.Get()
	if schema == nil {
		t.Fatal("expected schema to be populated, got nil")
	}

	if len(schema.Raw.Schema.Items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(schema.Raw.Schema.Items))
	}
	if schema.Raw.Schema.Items[0].Defindex != 5021 {
		t.Errorf("expected item defindex 5021, got %d", schema.Raw.Schema.Items[0].Defindex)
	}

	if kitName, exists := schema.Raw.Schema.PaintKits["12"]; !exists || kitName != "Nutcracker" {
		t.Errorf("expected paintkit '12' to be 'Nutcracker', got %v", kitName)
	}

	if val, ok := schema.Raw.ItemsGame["valid_key"]; !ok || val != "value" {
		t.Errorf("expected ItemsGame to contain valid_key='value', got %v", val)
	}
}

func TestSchemaManager_Refresh_Failures(t *testing.T) {
	tests := []struct {
		name      string
		mockSetup func(m *test.MockRequester)
	}{
		{
			name: "Overview WebAPI Error",
			mockSetup: func(m *test.MockRequester) {
				m.ResponseErrs["IEconItems_440/GetSchemaOverview"] = errors.New("steam api down")
			},
		},
		{
			name: "Items WebAPI Error",
			mockSetup: func(m *test.MockRequester) {
				m.ResponseErrs["IEconItems_440/GetSchemaItems"] = errors.New("steam api timeout")
			},
		},
		{
			name: "External Resource HTTP Error",
			mockSetup: func(m *test.MockRequester) {
				m.OnDo = func(req *tr.Request) (*tr.Response, error) {
					return nil, errors.New("github down")
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockAPI := test.NewMockRequester()

			mockAPI.SetJSONResponse("IEconItems_440", "GetSchemaOverview", map[string]any{"result": map[string]any{}})
			mockAPI.SetJSONResponse("IEconItems_440", "GetSchemaItems", map[string]any{"result": map[string]any{"items": []any{}}})

			tt.mockSetup(mockAPI)

			sm := New(Config{})
			sm.client = mockAPI
			sm.bus = bus.NewBus()

			err := sm.Refresh(context.Background())
			if err == nil {
				t.Error("expected error during Refresh, got nil")
			}
		})
	}
}

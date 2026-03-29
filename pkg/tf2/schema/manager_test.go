// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package schema

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	tr "github.com/lemon4ksan/g-man/pkg/steam/transport"
	"github.com/lemon4ksan/g-man/test"
)

func setupSchema(t *testing.T, cfg Config) (*Manager, *test.MockRequester) {
	t.Helper()
	mockAPI := test.NewMockRequester()
	init := test.NewMockInitContext()
	init.SetService(mockAPI)

	sm := New(cfg)
	if err := sm.Init(init); err != nil {
		t.Fatalf("failed to init schema manager: %v", err)
	}
	return sm, mockAPI
}

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
	sm, _ := setupSchema(t, Config{LiteMode: true})

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
	sm, mockAPI := setupSchema(t, Config{LiteMode: false})

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

	mockAPI.OnRest = func(method, path string, body []byte) (*http.Response, error) {
		if strings.Contains(path, "proto_obj_defs") {
			vdf := "\"lang\"\n{\n\t\"Tokens\"\n\t{\n\t\t\"9_12_weapon 12\" \"Nutcracker\"\n\t}\n}\n"
			return &http.Response{
				Body: io.NopCloser(strings.NewReader(vdf)),
				StatusCode: 200,
			}, nil
		}

		if strings.Contains(path, "items_game.txt") {
			vdf := "\"items_game\"\n{\n\t\"valid_key\" \"value\"\n}\n"
			return &http.Response{
				Body: io.NopCloser(strings.NewReader(vdf)),
				StatusCode: 200,
			}, nil
		}

		return nil, nil
	}
	sub := sm.Bus.Subscribe(&SchemaUpdatedEvent{})

	err := sm.Refresh(context.Background())
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

	select {
	case <-sub.C():
		// OK
	case <-time.After(100 * time.Millisecond):
		t.Error("SchemaUpdatedEvent was not published")
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
			name: "Github Resource Down",
			mockSetup: func(m *test.MockRequester) {
				m.OnDo = func(req *tr.Request) (*tr.Response, error) {
					if strings.HasPrefix(req.Target().String(), "https://raw.githubusercontent.com") {
						return nil, errors.New("github connection failed")
					}
					return nil, nil
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sm, mockAPI := setupSchema(t, Config{})

			mockAPI.SetJSONResponse("IEconItems_440", "GetSchemaOverview", map[string]any{"result": map[string]any{}})
			mockAPI.SetJSONResponse("IEconItems_440", "GetSchemaItems", map[string]any{"result": map[string]any{"items": []any{}}})

			tt.mockSetup(mockAPI)

			err := sm.Refresh(context.Background())
			if err == nil {
				t.Error("expected error during Refresh, got nil")
			}
		})
	}
}

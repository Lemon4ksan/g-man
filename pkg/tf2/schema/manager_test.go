// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package tf2schema

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/lemon4ksan/g-man/pkg/steam/api"
	"github.com/lemon4ksan/g-man/pkg/steam/transport"
	"github.com/lemon4ksan/g-man/pkg/steam/bus"
)

type targetMock struct{ url string }

func (t targetMock) String() string { return t.url }

type mockWebAPI struct {
	overviewErr error
	itemsErr    error
	httpErr     error

	overviewData  map[string]any
	itemsData     map[string]any
	paintKitsBody string
	itemsGameBody string
}

func (m *mockWebAPI) CallWebAPI(ctx context.Context, httpMethod, iface, method string, version int, respMsg any, mods ...api.RequestModifier) error {
	if method == "GetSchemaOverview" {
		if m.overviewErr != nil {
			return m.overviewErr
		}
		*respMsg.(*map[string]any) = m.overviewData
		return nil
	}
	if method == "GetSchemaItems" {
		if m.itemsErr != nil {
			return m.itemsErr
		}
		*respMsg.(*map[string]any) = m.itemsData
		return nil
	}
	return errors.New("unknown webapi method")
}

func (m *mockWebAPI) Do(req *transport.Request) (*transport.Response, error) {
	if m.httpErr != nil {
		return nil, m.httpErr
	}

	urlStr := ""
	if req.Target() != nil {
		urlStr = req.Target().String()
	}

	resp := &transport.Response{
		StatusCode: http.StatusOK,
		Header:     make(http.Header),
	}

	if strings.Contains(urlStr, "tf_proto_obj_defs_english") || req.Target() == nil {
		resp.Body = []byte(m.paintKitsBody)
	}
	if strings.Contains(urlStr, "items_game.txt") {
		resp.Body = []byte(m.itemsGameBody)
	}

	return resp, nil
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
	mockAPI := &mockWebAPI{
		overviewData: map[string]any{
			"qualities": map[string]any{"Normal": 0, "Genuine": 1},
		},
		itemsData: map[string]any{
			"result": map[string]any{
				"items": []any{
					map[string]any{"defindex": 5021, "name": "Mann Co. Supply Crate Key"},
				},
				"next": float64(0),
			},
		},
		paintKitsBody: `
"lang"
{
	"Tokens"
	{
		"9_11_weapon 11" "11: Macabre Web"
		"9_12_weapon 12" "Nutcracker"
	}
}
`,
		itemsGameBody: `
"items_game"
{
	"valid_key" "value"
}
`,
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

	if len(schema.Raw.Schema.Qualities) == 0 {
		t.Error("expected qualities to be parsed from overview")
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
		mockSetup func(*mockWebAPI)
	}{
		{
			name: "Overview WebAPI Error",
			mockSetup: func(m *mockWebAPI) {
				m.overviewErr = errors.New("steam api down")
			},
		},
		{
			name: "Items WebAPI Error",
			mockSetup: func(m *mockWebAPI) {
				m.itemsErr = errors.New("steam api timeout")
			},
		},
		{
			name: "GitHub HTTP Error",
			mockSetup: func(m *mockWebAPI) {
				m.httpErr = errors.New("github down")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockAPI := &mockWebAPI{
				overviewData:  map[string]any{},
				itemsData:     map[string]any{"result": map[string]any{"items": []any{}}},
				paintKitsBody: `"lang" { "Tokens" {} }`,
				itemsGameBody: `"items_game" {}`,
			}
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

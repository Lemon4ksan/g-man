// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package service

import (
	"net/url"
	"testing"

	"github.com/lemon4ksan/g-man/pkg/steam/api"
	"github.com/lemon4ksan/g-man/pkg/steam/protocol/enums"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/emptypb"
)

func TestUnifiedTarget_Formatting(t *testing.T) {
	cases := []struct {
		name     string
		target   *UnifiedTarget
		expected string
	}{
		{
			name: "Standard Service",
			target: &UnifiedTarget{
				Interface: "Player",
				Method:    "GetGameBadgeLevels",
				Version:   1,
				IsService: true,
			},
			expected: "IPlayerService/GetGameBadgeLevels/v1",
		},
		{
			name: "Already prefixed",
			target: &UnifiedTarget{
				Interface: "IInventory",
				Method:    "GetItems",
				Version:   2,
				IsService: true,
			},
			expected: "IInventoryService/GetItems/v2",
		},
		{
			name: "Non-Service",
			target: &UnifiedTarget{
				Interface: "Cloud",
				Method:    "GetFile",
				Version:   1,
				IsService: false,
			},
			expected: "ICloud/GetFile/v1",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.target.HTTPPath() != tc.expected {
				t.Errorf("expected %s, got %s", tc.expected, tc.target.HTTPPath())
			}
		})
	}
}

func TestUnifiedTarget_EMsg(t *testing.T) {
	target := &UnifiedTarget{}

	if target.EMsg(true) != enums.EMsg_ServiceMethodCallFromClient {
		t.Error("wrong EMsg for authed unified request")
	}

	if target.EMsg(false) != enums.EMsg_ServiceMethodCallFromClientNonAuthed {
		t.Error("wrong EMsg for non-authed unified request")
	}
}

func TestNewUnifiedRequest_Encoding(t *testing.T) {
	t.Run("Proto encoding", func(t *testing.T) {
		msg := &emptypb.Empty{}
		req, err := NewUnifiedRequest("POST", "Test", "Method", 1, msg)
		if err != nil {
			t.Fatal(err)
		}
		expected, _ := proto.Marshal(msg)
		if string(req.Body()) != string(expected) {
			t.Error("proto body mismatch")
		}
	})

	t.Run("JSON encoding", func(t *testing.T) {
		msg := map[string]string{"foo": "bar"}
		req, err := NewUnifiedRequest("POST", "Test", "Method", 1, msg)
		if err != nil {
			t.Fatal(err)
		}
		if string(req.Body()) != `{"foo":"bar"}` {
			t.Errorf("json body mismatch: %s", string(req.Body()))
		}
	})

	t.Run("Raw bytes", func(t *testing.T) {
		raw := []byte{0x01, 0x02}
		req, _ := NewUnifiedRequest("POST", "Test", "Method", 1, raw)
		if string(req.Body()) != string(raw) {
			t.Error("raw body mismatch")
		}
	})
}

func TestWebAPITarget(t *testing.T) {
	target := &WebAPITarget{
		HttpMethod: "GET",
		Interface:  "ISteamUser",
		Method:     "GetPlayerSummaries",
		Version:    2,
	}

	expectedPath := "ISteamUser/GetPlayerSummaries/v2"
	if target.HTTPPath() != expectedPath {
		t.Errorf("expected %s, got %s", expectedPath, target.HTTPPath())
	}
}

func TestLegacyTarget(t *testing.T) {
	emsg := enums.EMsg_ClientLogon
	target := &LegacyTarget{eMsg: emsg}

	if target.EMsg(true) != emsg {
		t.Error("EMsg mismatch")
	}

	if target.ObjectName() != "" {
		t.Error("LegacyTarget should have empty object name")
	}
}

func TestRequestModifiers(t *testing.T) {
	req := NewWebAPIRequest("GET", "I", "M", 1)

	api.WithQueryParam("a", "1")(req, nil)
	api.WithQueryParams(url.Values{"b": {"2"}})(req, nil)

	if req.Params().Get("a") != "1" {
		t.Error("WithParam failed")
	}
	if req.Params().Get("b") != "2" {
		t.Error("WithParams failed")
	}
}

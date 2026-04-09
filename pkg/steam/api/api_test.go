// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package api

import (
	"testing"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/emptypb"
)

func TestHttpTarget(t *testing.T) {
	target := HttpTarget{
		HttpMethod: "POST",
		URL:        "https://steamcommunity.com/tradeoffer/new/",
	}

	if target.HTTPMethod() != "POST" {
		t.Errorf("expected POST, got %s", target.HTTPMethod())
	}

	expectedPath := "tradeoffer/new/"
	if target.HTTPPath() != expectedPath {
		t.Errorf("expected path %s, got %s", expectedPath, target.HTTPPath())
	}

	// Test default method
	targetDefault := HttpTarget{URL: "http://test.com/path"}
	if targetDefault.HTTPMethod() != "GET" {
		t.Error("expected default method GET")
	}
}

func TestUnmarshalResponse(t *testing.T) {
	t.Run("Wrapped JSON", func(t *testing.T) {
		data := []byte(`{"response": {"name": "G-Man"}}`)
		target := make(map[string]string)
		err := UnmarshalResponse(data, &target, FormatJSON)
		if err != nil {
			t.Fatalf("unmarshal failed: %v", err)
		}
		if target["name"] != "G-Man" {
			t.Errorf("expected G-Man, got %s", target["name"])
		}
	})

	t.Run("Direct JSON", func(t *testing.T) {
		data := []byte(`{"name": "Gordon"}`)
		target := make(map[string]string)
		err := UnmarshalResponse(data, &target, FormatJSON)
		if err != nil {
			t.Fatalf("unmarshal failed: %v", err)
		}
		if target["name"] != "Gordon" {
			t.Errorf("expected Gordon, got %s", target["name"])
		}
	})

	t.Run("Protobuf", func(t *testing.T) {
		msg := &emptypb.Empty{}
		data, _ := proto.Marshal(msg)

		target := &emptypb.Empty{}
		err := UnmarshalResponse(data, &target, FormatProtobuf)
		if err != nil {
			t.Fatalf("unmarshal protobuf failed: %v", err)
		}
	})

	t.Run("VDF Text", func(t *testing.T) {
		data := []byte(`"Player" { "Health" "100" }`)
		var target struct {
			Player struct {
				Health string `mapstructure:"Health"`
			} `mapstructure:"Player"`
		}
		err := UnmarshalResponse(data, &target, FormatVDF)
		if err != nil {
			t.Fatalf("unmarshal VDF failed: %v", err)
		}
		if target.Player.Health != "100" {
			t.Errorf("expected 100, got %s", target.Player.Health)
		}
	})
}

func TestEmptyResponse(t *testing.T) {
	target := make(map[string]any)

	cases := [][]byte{
		nil,
		{},
		[]byte(`{"response": {}}`),
		[]byte(`{"response": null}`),
	}

	for _, tc := range cases {
		err := UnmarshalResponse(tc, &target, FormatJSON)
		if err != nil {
			t.Errorf("expected no error for %v, got %v", tc, err)
		}
	}
}

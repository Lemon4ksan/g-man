// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package rest

import (
	"net/url"
	"testing"
)

func TestStructToValues(t *testing.T) {
	t.Run("Nil input", func(t *testing.T) {
		res, err := StructToValues(nil)
		if err != nil {
			t.Fatal(err)
		}

		if res != nil {
			t.Error("expected nil result for nil input")
		}
	})

	t.Run("Pass through url.Values", func(t *testing.T) {
		input := url.Values{"test": {"1"}}

		res, err := StructToValues(input)
		if err != nil {
			t.Fatal(err)
		}

		if res.Get("test") != "1" {
			t.Error("failed to pass through url.Values")
		}
	})

	t.Run("Basic types and pointers", func(t *testing.T) {
		type Params struct {
			Str   string  `url:"s"`
			Int   int32   `url:"i"`
			Uint  uint64  `url:"u"`
			Bool  bool    `url:"b"`
			Float float64 `url:"f"`
			Skip  string  `url:"-"`
			NoTag string
		}

		p := &Params{
			Str:   "hello",
			Int:   -42,
			Uint:  100,
			Bool:  true,
			Float: 3.14,
			Skip:  "ignore",
			NoTag: "ignore",
		}

		v, err := StructToValues(p)
		if err != nil {
			t.Fatalf("failed to convert: %v", err)
		}

		if v.Get("s") != "hello" {
			t.Errorf("expected hello, got %s", v.Get("s"))
		}

		if v.Get("i") != "-42" {
			t.Errorf("expected -42, got %s", v.Get("i"))
		}

		if v.Get("u") != "100" {
			t.Errorf("expected 100, got %s", v.Get("u"))
		}

		if v.Get("b") != "true" {
			t.Errorf("expected true, got %s", v.Get("b"))
		}

		if v.Get("f") != "3.14" {
			t.Errorf("expected 3.14, got %s", v.Get("f"))
		}

		if v.Get("-") != "" || v.Get("NoTag") != "" {
			t.Error("fields with '-' or no tag should be ignored")
		}
	})

	t.Run("Omitempty logic", func(t *testing.T) {
		type OmitParams struct {
			Show    string `url:"show,omitempty"`
			Hide    int    `url:"hide,omitempty"`
			Normal  string `url:"normal"`
			ZeroInt int    `url:"zero"`
		}

		p := OmitParams{
			Show:   "present",
			Hide:   0,
			Normal: "",
		}

		v, err := StructToValues(p)
		if err != nil {
			t.Fatal(err)
		}

		if v.Get("show") != "present" {
			t.Error("expected 'show' to be present")
		}

		if v.Has("hide") {
			t.Error("expected 'hide' to be omitted")
		}

		if !v.Has("normal") {
			t.Error("expected 'normal' to be present even if empty")
		}
	})

	t.Run("Error: Not a struct", func(t *testing.T) {
		_, err := StructToValues("string is not a struct")
		if err == nil {
			t.Error("expected error for non-struct input")
		}
	})

	t.Run("Error: Unsupported field type", func(t *testing.T) {
		type BadParams struct {
			List []string `url:"list"`
		}

		_, err := StructToValues(BadParams{List: []string{"a"}})
		if err == nil {
			t.Error("expected error for slice field")
		}
	})

	t.Run("Tag with only key", func(t *testing.T) {
		type Simple struct {
			A string `url:"only_key,"`
		}

		v, _ := StructToValues(Simple{A: "val"})
		if v.Get("only_key") != "val" {
			t.Error("failed to parse tag with trailing comma")
		}
	})
}

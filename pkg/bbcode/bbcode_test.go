// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package bbcode_test

import (
	"testing"

	"github.com/lemon4ksan/g-man/pkg/bbcode"
)

func TestBuilder(t *testing.T) {
	b := bbcode.NewBuilder()
	msg := b.H1("Title").NewLine().
		Bold("Status:").Space().Text("Success!").NewLine().
		Code("Offer #12345").NewLine().
		Link("View Profile", "https://steamcommunity.com/id/lemon4ksan").NewLine().
		Spoiler("Secret Code").NewLine().
		Quote("Some quoted text").NewLine().
		List("Item 1", "Item 2").
		String()

	expected := "[h1]Title[/h1]\n" +
		"[b]Status:[/b] Success!\n" +
		"[code]Offer #12345[/code]\n" +
		"[url=https://steamcommunity.com/id/lemon4ksan]View Profile[/url]\n" +
		"[spoiler]Secret Code[/spoiler]\n" +
		"[quote]Some quoted text[/quote]\n" +
		"[list][*]Item 1[*]Item 2[/list]"

	if msg != expected {
		t.Errorf("expected:\n%q\ngot:\n%q", expected, msg)
	}
}

func TestBuilderFormattingMethods(t *testing.T) {
	msg := bbcode.NewBuilder().
		Italic("italic").
		Underline("underline").
		Strike("strike").
		Pre("pre").
		URL("https://example.com").
		String()

	expected := "[i]italic[/i][u]underline[/u][strike]strike[/strike][code]pre[/code][url=https://example.com]https://example.com[/url]"
	if msg != expected {
		t.Errorf("expected %q, got %q", expected, msg)
	}
}

func TestStandaloneHelpers(t *testing.T) {
	if got := bbcode.Bold("hello"); got != "[b]hello[/b]" {
		t.Errorf("Bold() = %q, want [b]hello[/b]", got)
	}

	if got := bbcode.Italic("hello"); got != "[i]hello[/i]" {
		t.Errorf("Italic() = %q, want [i]hello[/i]", got)
	}

	if got := bbcode.Underline("hello"); got != "[u]hello[/u]" {
		t.Errorf("Underline() = %q, want [u]hello[/u]", got)
	}

	if got := bbcode.Strike("hello"); got != "[strike]hello[/strike]" {
		t.Errorf("Strike() = %q, want [strike]hello[/strike]", got)
	}

	if got := bbcode.Code("hello"); got != "[code]hello[/code]" {
		t.Errorf("Code() = %q, want [code]hello[/code]", got)
	}

	if got := bbcode.Link("link", "http://x"); got != "[url=http://x]link[/url]" {
		t.Errorf("Link() = %q, want [url=http://x]link[/url]", got)
	}

	if got := bbcode.Spoiler("secret"); got != "[spoiler]secret[/spoiler]" {
		t.Errorf("Spoiler() = %q, want [spoiler]secret[/spoiler]", got)
	}

	if got := bbcode.Quote("q"); got != "[quote]q[/quote]" {
		t.Errorf("Quote() = %q, want [quote]q[/quote]", got)
	}

	if got := bbcode.H1("header"); got != "[h1]header[/h1]" {
		t.Errorf("H1() = %q, want [h1]header[/h1]", got)
	}
}

// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package utils

import (
	"fmt"
	"strings"
)

// ProfileLinks contains useful URLs for a Steam user.
type ProfileLinks struct {
	Steam    string
	Bptf     string
	SteamRep string
}

// GenerateLinks creates profile links from a SteamID64.
func GenerateLinks(steamID uint64) ProfileLinks {
	idStr := fmt.Sprintf("%d", steamID)
	return ProfileLinks{
		Steam:    "https://steamcommunity.com/profiles/" + idStr,
		Bptf:     "https://backpack.tf/profiles/" + idStr,
		SteamRep: "https://steamrep.com/profiles/" + idStr,
	}
}

// ShortenItemName abbreviates common TF2 item prefixes.
func ShortenItemName(name string) string {
	if name == "" {
		return ""
	}
	replacer := strings.NewReplacer(
		"Non-Craftable", "NC",
		"Professional Killstreak", "Pro KS",
		"Specialized Killstreak", "Spec KS",
		"Killstreak", "KS",
	)
	return replacer.Replace(name)
}

// EscapeMarkdown escapes special characters used in markdown.
func EscapeMarkdown(text string) string {
	replacer := strings.NewReplacer(
		"_", "‗",
		"*", "^",
		"~", "-",
		"`", "'",
		">", "<",
		"|", "l",
		"\\", "/",
		"(", "/",
		")", "/",
		"[", "/",
		"]", "/",
	)
	return replacer.Replace(text)
}

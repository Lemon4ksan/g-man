// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package review

import "fmt"

// Formatter determines how report elements are formatted for different platforms.
type Formatter interface {
	Item(name string) string
	Link(text, url string) string
	Header(text string) string
	Bold(text string) string
}

// SteamFormatter formats the report for Steam chat.
type SteamFormatter struct{}

// Item formats the item name for Steam chat.
func (f SteamFormatter) Item(n string) string { return n }

// Link formats the link for Steam chat.
func (f SteamFormatter) Link(t, u string) string { return fmt.Sprintf("%s (%s)", t, u) }

// Header formats the header for Steam chat.
func (f SteamFormatter) Header(t string) string { return "/me " + t }

// Bold formats the bold text for Steam chat.
func (f SteamFormatter) Bold(t string) string { return t }

// WebhookFormatter formats the report for webhook.
type WebhookFormatter struct{}

// Item formats the item name for webhook.
func (f WebhookFormatter) Item(n string) string { return "_" + n + "_" }

// Link formats the link for webhook.
func (f WebhookFormatter) Link(t, u string) string { return fmt.Sprintf("[%s](%s)", t, u) }

// Header formats the header for webhook.
func (f WebhookFormatter) Header(t string) string { return "### " + t }

// Bold formats the bold text for webhook.
func (f WebhookFormatter) Bold(t string) string { return "**" + t + "**" }

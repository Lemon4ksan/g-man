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

type SteamFormatter struct{}

func (f SteamFormatter) Item(n string) string    { return n }
func (f SteamFormatter) Link(t, u string) string { return fmt.Sprintf("%s (%s)", t, u) }
func (f SteamFormatter) Header(t string) string  { return "/me " + t }
func (f SteamFormatter) Bold(t string) string    { return t } // Steam Chat doesn't support formatting

type WebhookFormatter struct{}

func (f WebhookFormatter) Item(n string) string    { return "_" + n + "_" }
func (f WebhookFormatter) Link(t, u string) string { return fmt.Sprintf("[%s](%s)", t, u) }
func (f WebhookFormatter) Header(t string) string  { return "### " + t }
func (f WebhookFormatter) Bold(t string) string    { return "**" + t + "**" }

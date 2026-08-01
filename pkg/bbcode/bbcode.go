// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package bbcode provides a fluent message builder and text formatting utilities for Steam Chat BBCode markup.
package bbcode

import (
	"fmt"
	"strings"
)

// Builder constructs BBCode formatted text strings.
type Builder struct {
	sb strings.Builder
}

// NewBuilder creates a new BBCode message builder.
func NewBuilder() *Builder {
	return &Builder{}
}

// Text appends plain text to the builder.
func (b *Builder) Text(text string) *Builder {
	b.sb.WriteString(text)
	return b
}

// Bold appends bold formatted text: [b]text[/b].
func (b *Builder) Bold(text string) *Builder {
	b.sb.WriteString("[b]")
	b.sb.WriteString(text)
	b.sb.WriteString("[/b]")

	return b
}

// Italic appends italic formatted text: [i]text[/i].
func (b *Builder) Italic(text string) *Builder {
	b.sb.WriteString("[i]")
	b.sb.WriteString(text)
	b.sb.WriteString("[/i]")

	return b
}

// Underline appends underlined text: [u]text[/u].
func (b *Builder) Underline(text string) *Builder {
	b.sb.WriteString("[u]")
	b.sb.WriteString(text)
	b.sb.WriteString("[/u]")

	return b
}

// Strike appends strikethrough text: [strike]text[/strike].
func (b *Builder) Strike(text string) *Builder {
	b.sb.WriteString("[strike]")
	b.sb.WriteString(text)
	b.sb.WriteString("[/strike]")

	return b
}

// Code appends inline code formatted text: [code]text[/code].
func (b *Builder) Code(text string) *Builder {
	b.sb.WriteString("[code]")
	b.sb.WriteString(text)
	b.sb.WriteString("[/code]")

	return b
}

// Pre appends preformatted block text.
func (b *Builder) Pre(text string) *Builder {
	b.sb.WriteString("[code]")
	b.sb.WriteString(text)
	b.sb.WriteString("[/code]")

	return b
}

// Link appends a labeled URL link: [url=target]label[/url].
func (b *Builder) Link(label, target string) *Builder {
	b.sb.WriteString("[url=")
	b.sb.WriteString(target)
	b.sb.WriteString("]")
	b.sb.WriteString(label)
	b.sb.WriteString("[/url]")

	return b
}

// URL appends a raw clickable URL: [url=target]target[/url].
func (b *Builder) URL(target string) *Builder {
	return b.Link(target, target)
}

// Spoiler appends hidden spoiler text: [spoiler]text[/spoiler].
func (b *Builder) Spoiler(text string) *Builder {
	b.sb.WriteString("[spoiler]")
	b.sb.WriteString(text)
	b.sb.WriteString("[/spoiler]")

	return b
}

// Quote appends blockquote text: [quote]text[/quote].
func (b *Builder) Quote(text string) *Builder {
	b.sb.WriteString("[quote]")
	b.sb.WriteString(text)
	b.sb.WriteString("[/quote]")

	return b
}

// H1 appends header 1 text: [h1]text[/h1].
func (b *Builder) H1(text string) *Builder {
	b.sb.WriteString("[h1]")
	b.sb.WriteString(text)
	b.sb.WriteString("[/h1]")

	return b
}

// List appends a BBCode bullet list of items: [list][*]item1[*]item2[/list].
func (b *Builder) List(items ...string) *Builder {
	if len(items) == 0 {
		return b
	}

	b.sb.WriteString("[list]")

	for _, item := range items {
		b.sb.WriteString("[*]")
		b.sb.WriteString(item)
	}

	b.sb.WriteString("[/list]")

	return b
}

// Space appends a single space character.
func (b *Builder) Space() *Builder {
	b.sb.WriteByte(' ')
	return b
}

// NewLine appends a newline character.
func (b *Builder) NewLine() *Builder {
	b.sb.WriteByte('\n')
	return b
}

// String returns the constructed BBCode message string.
func (b *Builder) String() string {
	return b.sb.String()
}

// Standalone formatting helper functions.

// Bold returns text wrapped in BBCode bold tags.
func Bold(text string) string {
	return fmt.Sprintf("[b]%s[/b]", text)
}

// Italic returns text wrapped in BBCode italic tags.
func Italic(text string) string {
	return fmt.Sprintf("[i]%s[/i]", text)
}

// Underline returns text wrapped in BBCode underline tags.
func Underline(text string) string {
	return fmt.Sprintf("[u]%s[/u]", text)
}

// Strike returns text wrapped in BBCode strikethrough tags.
func Strike(text string) string {
	return fmt.Sprintf("[strike]%s[/strike]", text)
}

// Code returns text wrapped in BBCode code tags.
func Code(text string) string {
	return fmt.Sprintf("[code]%s[/code]", text)
}

// Link returns a BBCode url tag with label and target URL.
func Link(label, target string) string {
	return fmt.Sprintf("[url=%s]%s[/url]", target, label)
}

// Spoiler returns text wrapped in BBCode spoiler tags.
func Spoiler(text string) string {
	return fmt.Sprintf("[spoiler]%s[/spoiler]", text)
}

// Quote returns text wrapped in BBCode quote tags.
func Quote(text string) string {
	return fmt.Sprintf("[quote]%s[/quote]", text)
}

// H1 returns text wrapped in BBCode h1 tags.
func H1(text string) string {
	return fmt.Sprintf("[h1]%s[/h1]", text)
}

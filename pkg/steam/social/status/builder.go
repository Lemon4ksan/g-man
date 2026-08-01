// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package status

import (
	"strings"
)

// RenderMarquee produces a scrolling text window shifted by the specified character offset.
func RenderMarquee(text string, offset, width int) string {
	runes := []rune(text)
	length := len(runes)

	if length == 0 || width <= 0 {
		return ""
	}

	if length <= width {
		return text
	}

	padding := []rune(" -- ")
	cycleLen := len(runes) + len(padding)
	fullCycle := make([]rune, 0, cycleLen)
	fullCycle = append(fullCycle, runes...)
	fullCycle = append(fullCycle, padding...)
	startPos := offset % cycleLen

	var sb strings.Builder
	sb.Grow(width * 4)

	for i := range width {
		sb.WriteRune(fullCycle[(startPos+i)%cycleLen])
	}

	return sb.String()
}

// RenderProgressBar builds a text-based progress bar representation.
func RenderProgressBar(current, goal, width int) string {
	if goal <= 0 || width <= 0 {
		return ""
	}

	filledLen := min((current*width)/goal, width)

	var sb strings.Builder
	sb.Grow(width + 10)
	sb.WriteByte('[')

	for i := range width {
		if i < filledLen {
			sb.WriteString("█")
		} else {
			sb.WriteString("░")
		}
	}

	sb.WriteByte(']')

	return sb.String()
}

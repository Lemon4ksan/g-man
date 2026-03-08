// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package log

import "strings"

// Mask returns a hidden version of the string (e.g. "account_name" -> "ac...me").
func Mask(s string) string {
	if len(s) <= 4 {
		return "****"
	}
	return s[:2] + "..." + s[len(s)-2:]
}

// MaskPath searches the path for sensitive data and masks it.
func MaskPath(path string, sensitive string) string {
	if sensitive == "" {
		return path
	}
	return strings.ReplaceAll(path, sensitive, Mask(sensitive))
}

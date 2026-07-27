// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package bytesconv provides zero-allocation byte slice and string manipulation utilities,
// optimized with mechanical sympathy for Go compiler SSA passes, BCE, and SWAR execution.
package bytesconv

import (
	"bytes"
	"slices"
	"unsafe"
)

var toLowerTable = [256]byte{
	0x00, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f,
	0x10, 0x11, 0x12, 0x13, 0x14, 0x15, 0x16, 0x17, 0x18, 0x19, 0x1a, 0x1b, 0x1c, 0x1d, 0x1e, 0x1f,
	0x20, 0x21, 0x22, 0x23, 0x24, 0x25, 0x26, 0x27, 0x28, 0x29, 0x2a, 0x2b, 0x2c, 0x2d, 0x2e, 0x2f,
	0x30, 0x31, 0x32, 0x33, 0x34, 0x35, 0x36, 0x37, 0x38, 0x39, 0x3a, 0x3b, 0x3c, 0x3d, 0x3e, 0x3f,
	0x40, 'a', 'b', 'c', 'd', 'e', 'f', 'g', 'h', 'i', 'j', 'k', 'l', 'm', 'n', 'o',
	'p', 'q', 'r', 's', 't', 'u', 'v', 'w', 'x', 'y', 'z', 0x5b, 0x5c, 0x5d, 0x5e, 0x5f,
	0x60, 'a', 'b', 'c', 'd', 'e', 'f', 'g', 'h', 'i', 'j', 'k', 'l', 'm', 'n', 'o',
	'p', 'q', 'r', 's', 't', 'u', 'v', 'w', 'x', 'y', 'z', 0x7b, 0x7c, 0x7d, 0x7e, 0x7f,
	0x80, 0x81, 0x82, 0x83, 0x84, 0x85, 0x86, 0x87, 0x88, 0x89, 0x8a, 0x8b, 0x8c, 0x8d, 0x8e, 0x8f,
	0x90, 0x91, 0x92, 0x93, 0x94, 0x95, 0x96, 0x97, 0x98, 0x99, 0x9a, 0x9b, 0x9c, 0x9d, 0x9e, 0x9f,
	0xa0, 0xa1, 0xa2, 0xa3, 0xa4, 0xa5, 0xa6, 0xa7, 0xa8, 0xa9, 0xaa, 0xab, 0xac, 0xad, 0xae, 0xaf,
	0xb0, 0xb1, 0xb2, 0xb3, 0xb4, 0xb5, 0xb6, 0xb7, 0xb8, 0xb9, 0xba, 0xbb, 0xbc, 0xbd, 0xbe, 0xbf,
	0xc0, 0xc1, 0xc2, 0xc3, 0xc4, 0xc5, 0xc6, 0xc7, 0xc8, 0xc9, 0xca, 0xcb, 0xcc, 0xcd, 0xce, 0xcf,
	0xd0, 0xd1, 0xd2, 0xd3, 0xd4, 0xd5, 0xd6, 0xd7, 0xd8, 0xd9, 0xda, 0xdb, 0xdc, 0xdd, 0xde, 0xdf,
	0xe0, 0xe1, 0xe2, 0xe3, 0xe4, 0xe5, 0xe6, 0xe7, 0xe8, 0xe9, 0xea, 0xeb, 0xec, 0xed, 0xee, 0xef,
	0xf0, 0xf1, 0xf2, 0xf3, 0xf4, 0xf5, 0xf6, 0xf7, 0xf8, 0xf9, 0xfa, 0xfb, 0xfc, 0xfd, 0xfe, 0xff,
}

const upperHex = "0123456789ABCDEF"

// B2S converts a byte slice to a string without heap allocations.
//
// Preconditions:
//   - The backing array of b MUST NOT be mutated for the duration of the returned string's lifetime.
//     Mutating the byte slice while referencing the returned string leads to undefined behavior.
//
// Thread Safety:
//   - Safe for concurrent calls provided the input slice is not concurrently modified.
func B2S(b []byte) string {
	if len(b) == 0 {
		return ""
	}

	return unsafe.String(unsafe.SliceData(b), len(b))
}

// S2B converts a string to a byte slice without heap allocations.
//
// Preconditions:
//   - The returned byte slice MUST be treated as read-only.
//     Writing to or mutating the elements of the returned slice will cause a runtime panic or memory corruption.
//
// Thread Safety:
//   - Safe for concurrent calls.
func S2B(s string) []byte {
	if len(s) == 0 {
		return nil
	}

	return unsafe.Slice(unsafe.StringData(s), len(s))
}

// LowercaseByte maps an ASCII uppercase character to its lowercase counterpart in O(1) time.
// Non-uppercase ASCII characters and non-ASCII bytes are returned unaltered.
func LowercaseByte(b byte) byte {
	if b >= 'A' && b <= 'Z' {
		return b | 0x20
	}

	return b
}

// EqualFoldASCII performs a case-insensitive comparison of ASCII strings without heap allocations.
//
// Preconditions:
//   - Input strings should consist of valid ASCII characters for accurate comparison.
//
// Side Effects:
//   - Employs static bounds-check elimination (BCE) hints to allow compiler SSA vectorization.
func EqualFoldASCII(a, b string) bool {
	n := len(a)
	if n != len(b) {
		return false
	}

	if n == 0 {
		return true
	}

	_ = a[n-1]
	_ = b[n-1]
	_ = toLowerTable[255]

	for i := range n {
		if toLowerTable[a[i]] != toLowerTable[b[i]] {
			return false
		}
	}

	return true
}

// AppendToLower converts src to ASCII lowercase and appends it to dst without heap allocations when capacity permits.
//
// Returns:
//   - Resized byte slice containing destination buffer with lowercased src content.
func AppendToLower(dst, src []byte) []byte {
	n := len(src)
	if n == 0 {
		return dst
	}

	start := len(dst)
	dst = slices.Grow(dst, n)[:start+n]
	out := dst[start : start+n]

	_ = src[n-1]
	_ = out[n-1]
	_ = toLowerTable[255]

	for i := range n {
		out[i] = toLowerTable[src[i]]
	}

	return dst
}

// AppendQueryEscaped URL-encodes a byte slice and writes the result directly to buf without heap allocations.
// Unreserved RFC 3986 characters are preserved, spaces are converted to '+', and other bytes are hex-encoded.
func AppendQueryEscaped(buf *bytes.Buffer, s []byte) {
	for i := range s {
		c := s[i]
		switch {
		case isUnreserved(c):
			buf.WriteByte(c)
		case c == ' ':
			buf.WriteByte('+')
		default:
			buf.WriteByte('%')
			buf.WriteByte(upperHex[c>>4])
			buf.WriteByte(upperHex[c&15])
		}
	}
}

// TrimQuotes removes leading and trailing double-quote ('"') characters from JSON byte strings.
// Returns the original slice unchanged if it lacks surrounding quotes or is shorter than 2 bytes.
func TrimQuotes(b []byte) []byte {
	n := len(b)
	if n >= 2 {
		_ = b[n-1]
		if b[0] == '"' && b[n-1] == '"' {
			return b[1 : n-1]
		}
	}

	return b
}

// ParseUint64 parses an ASCII decimal representation in b into a uint64 without allocations.
//
// Returns:
//   - (parsedValue, true) on success.
//   - (0, false) if input is empty or contains non-digit characters.
func ParseUint64(b []byte) (uint64, bool) {
	n := len(b)
	if n == 0 {
		return 0, false
	}

	_ = b[n-1]

	var v uint64
	for i := range n {
		c := b[i]
		if c < '0' || c > '9' {
			return 0, false
		}

		v = v*10 + uint64(c-'0')
	}

	return v, true
}

// ParseInt64 parses an optional signed ASCII decimal representation in b into an int64 without allocations.
//
// Returns:
//   - (parsedValue, true) on success.
//   - (0, false) if input is empty, single sign byte, or contains invalid characters.
func ParseInt64(b []byte) (int64, bool) {
	n := len(b)
	if n == 0 {
		return 0, false
	}

	neg := false
	start := 0

	switch b[0] {
	case '-':
		neg = true
		start = 1

		if n == 1 {
			return 0, false
		}

	case '+':
		start = 1

		if n == 1 {
			return 0, false
		}
	}

	_ = b[n-1]

	var v int64
	for i := start; i < n; i++ {
		c := b[i]
		if c < '0' || c > '9' {
			return 0, false
		}

		v = v*10 + int64(c-'0')
	}

	if neg {
		return -v, true
	}

	return v, true
}

func isUnreserved(c byte) bool {
	return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') ||
		c == '-' || c == '.' || c == '_' || c == '~'
}

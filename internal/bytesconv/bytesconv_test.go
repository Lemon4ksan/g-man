// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package bytesconv

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestB2S_And_S2B(t *testing.T) {
	t.Parallel()

	t.Run("b2s_empty", func(t *testing.T) {
		t.Parallel()
		assert.Equal(t, "", B2S(nil))
		assert.Equal(t, "", B2S([]byte{}))
	})

	t.Run("b2s_non_empty", func(t *testing.T) {
		t.Parallel()

		data := []byte("hello world")
		assert.Equal(t, "hello world", B2S(data))
	})

	t.Run("s2b_empty", func(t *testing.T) {
		t.Parallel()
		assert.Nil(t, S2B(""))
	})

	t.Run("s2b_non_empty", func(t *testing.T) {
		t.Parallel()

		str := "hello world"
		assert.Equal(t, []byte("hello world"), S2B(str))
	})
}

func TestLowercaseByte(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   byte
		want byte
	}{
		{"uppercase_A", 'A', 'a'},
		{"uppercase_Z", 'Z', 'z'},
		{"lowercase_a", 'a', 'a'},
		{"number", '5', '5'},
		{"symbol", '!', '!'},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, LowercaseByte(tt.in))
		})
	}
}

func TestEqualFoldASCII(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		a    string
		b    string
		want bool
	}{
		{"exact_match", "hello", "hello", true},
		{"case_insensitive_match", "HeLLo", "hElLO", true},
		{"empty_strings", "", "", true},
		{"different_lengths", "hello", "hell", false},
		{"mismatch", "hello", "world", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, EqualFoldASCII(tt.a, tt.b))
		})
	}
}

func TestAppendToLower(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		dst  []byte
		src  []byte
		want []byte
	}{
		{
			name: "empty_src",
			dst:  []byte("prefix_"),
			src:  nil,
			want: []byte("prefix_"),
		},
		{
			name: "lowercasing",
			dst:  []byte("test_"),
			src:  []byte("HELLO_123!"),
			want: []byte("test_hello_123!"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			actual := AppendToLower(tt.dst, tt.src)
			assert.Equal(t, tt.want, actual)
		})
	}
}

func TestAppendQueryEscaped(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   string
		want string
	}{
		{"unreserved_chars", "abcXYZ123-._~", "abcXYZ123-._~"},
		{"space_to_plus", "hello world", "hello+world"},
		{"special_chars", "a&b=c", "a%26b%3Dc"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			buf := new(bytes.Buffer)
			AppendQueryEscaped(buf, S2B(tt.in))
			assert.Equal(t, tt.want, buf.String())
		})
	}
}

func TestTrimQuotes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   []byte
		want []byte
	}{
		{"quoted_string", []byte(`"hello"`), []byte("hello")},
		{"single_quote_untrimmed", []byte(`"hello`), []byte(`"hello`)},
		{"no_quotes", []byte(`hello`), []byte("hello")},
		{"short_slice", []byte(`"`), []byte(`"`)},
		{"empty", []byte{}, []byte{}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, TrimQuotes(tt.in))
		})
	}
}

func TestParseUint64(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		in      string
		wantVal uint64
		wantOk  bool
	}{
		{"valid_zero", "0", 0, true},
		{"valid_number", "1234567890", 1234567890, true},
		{"invalid_char", "123a45", 0, false},
		{"empty_string", "", 0, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			val, ok := ParseUint64(S2B(tt.in))
			assert.Equal(t, tt.wantOk, ok)
			assert.Equal(t, tt.wantVal, val)
		})
	}
}

func TestParseInt64(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		in      string
		wantVal int64
		wantOk  bool
	}{
		{"positive_number", "12345", 12345, true},
		{"explicit_positive_number", "+12345", 12345, true},
		{"negative_number", "-12345", -12345, true},
		{"single_minus_sign", "-", 0, false},
		{"single_plus_sign", "+", 0, false},
		{"empty_string", "", 0, false},
		{"invalid_character", "123x", 0, false},
		{"invalid_character_negative", "-123x", 0, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			val, ok := ParseInt64(S2B(tt.in))
			assert.Equal(t, tt.wantOk, ok)
			assert.Equal(t, tt.wantVal, val)
		})
	}
}

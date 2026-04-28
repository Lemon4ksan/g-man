// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package api

import (
	"bytes"
	"encoding/binary"
	"errors"
	"reflect"
	"testing"
)

func encodeCString(b *bytes.Buffer, s string) {
	b.WriteString(s)
	b.WriteByte(0)
}

func encodeWideString(b *bytes.Buffer, s string) {
	for _, r := range s {
		binary.Write(b, binary.LittleEndian, uint16(r))
	}

	binary.Write(b, binary.LittleEndian, uint16(0))
}

func TestBVDFParser_AllTypes(t *testing.T) {
	buf := new(bytes.Buffer)

	buf.WriteByte(kvTypeNone)
	encodeCString(buf, "root")

	// String
	buf.WriteByte(kvTypeString)
	encodeCString(buf, "str")
	encodeCString(buf, "val")

	// Int32
	buf.WriteByte(kvTypeInt32)
	encodeCString(buf, "i32")
	binary.Write(buf, binary.LittleEndian, int32(42))

	// Float32
	buf.WriteByte(kvTypeFloat32)
	encodeCString(buf, "f32")
	binary.Write(buf, binary.LittleEndian, float32(3.14))

	// UInt64
	buf.WriteByte(kvTypeUInt64)
	encodeCString(buf, "u64")
	binary.Write(buf, binary.LittleEndian, uint64(123456789))

	// Int64
	buf.WriteByte(kvTypeInt64)
	encodeCString(buf, "i64")
	binary.Write(buf, binary.LittleEndian, int64(-123456789))

	// WideString
	buf.WriteByte(kvTypeWideString)
	encodeCString(buf, "wide")
	encodeWideString(buf, "hi")

	// Color & Pointer
	buf.WriteByte(kvTypeColor)
	encodeCString(buf, "clr")
	binary.Write(buf, binary.LittleEndian, int32(1))
	buf.WriteByte(kvTypePointer)
	encodeCString(buf, "ptr")
	binary.Write(buf, binary.LittleEndian, int32(2))

	buf.WriteByte(kvTypeEnd)
	buf.WriteByte(kvTypeEnd)

	var target map[string]any

	err := UnmarshalBinaryKV(buf.Bytes(), &target)
	if err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	root := target["root"].(map[string]any)
	if root["str"] != "val" || root["i32"].(int32) != 42 || root["u64"].(uint64) != 123456789 {
		t.Errorf("incorrect values in parsed map: %+v", root)
	}
}

func TestBVDFParser_SliceConversion(t *testing.T) {
	buf := new(bytes.Buffer)
	buf.WriteByte(kvTypeNone)
	encodeCString(buf, "list")

	buf.WriteByte(kvTypeString)
	encodeCString(buf, "0")
	encodeCString(buf, "first")

	buf.WriteByte(kvTypeString)
	encodeCString(buf, "1")
	encodeCString(buf, "second")

	buf.WriteByte(kvTypeEnd)
	buf.WriteByte(kvTypeEnd)

	var target struct {
		List []string `mapstructure:"list"`
	}

	err := UnmarshalBinaryKV(buf.Bytes(), &target)
	if err != nil {
		t.Fatal(err)
	}

	expected := []string{"first", "second"}
	if !reflect.DeepEqual(target.List, expected) {
		t.Errorf("expected %v, got %v", expected, target.List)
	}
}

func TestBVDFParser_Errors(t *testing.T) {
	tests := []struct {
		name string
		data []byte
	}{
		{"Unexpected EOF root", []byte{}},
		{"Unterminated CString", []byte{kvTypeString, 'a', 'b'}},
		{"EOF reading Int32", []byte{kvTypeInt32, 'k', 'e', 'y', 0, 1, 2}},
		{"EOF reading Uint64", []byte{kvTypeUInt64, 'k', 'e', 'y', 0, 1, 2, 3}},
		{"EOF reading Int64", []byte{kvTypeInt64, 'k', 'e', 'y', 0, 1, 2, 3}},
		{"EOF reading Float32", []byte{kvTypeFloat32, 'k', 'e', 'y', 0, 1, 2}},
		{"EOF reading WideString", []byte{kvTypeWideString, 'k', 'e', 'y', 0, 1}},
		{"Unknown type", []byte{99, 'k', 'e', 'y', 0}},
		{"Alternate End", []byte{kvTypeNone, 0, kvTypeAlternateEnd}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var target any

			err := UnmarshalBinaryKV(tt.data, &target)
			if err == nil {
				t.Error("expected error, got nil")
			}
		})
	}
}

func TestBVDFParser_RootNotObject(t *testing.T) {
	buf := new(bytes.Buffer)
	buf.WriteByte(kvTypeString)
	encodeCString(buf, "0")
	encodeCString(buf, "should_be_slice_root")
	buf.WriteByte(kvTypeEnd)

	var target any

	err := UnmarshalBinaryKV(buf.Bytes(), &target)
	if err == nil {
		t.Error("expected error because root is a slice (due to key '0'), but got nil")
	} else if !errors.Is(err, ErrFormat) {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestBVDFParser_EmptyNameOptimization(t *testing.T) {
	buf := new(bytes.Buffer)
	buf.WriteByte(kvTypeNone)
	encodeCString(buf, "")
	encodeCString(buf, "real")
	buf.WriteByte(kvTypeInt32)
	encodeCString(buf, "val")
	binary.Write(buf, binary.LittleEndian, int32(1))
	buf.WriteByte(kvTypeEnd)
	buf.WriteByte(kvTypeEnd)

	var target any

	err := UnmarshalBinaryKV(buf.Bytes(), &target)
	if err != nil {
		t.Fatal(err)
	}
}

func TestBVDFParser_SliceConversion_Complete(t *testing.T) {
	buf := new(bytes.Buffer)
	buf.WriteByte(kvTypeNone)
	encodeCString(buf, "list")

	buf.WriteByte(kvTypeString)
	encodeCString(buf, "0")
	encodeCString(buf, "a")
	buf.WriteByte(kvTypeString)
	encodeCString(buf, "1")
	encodeCString(buf, "b")

	buf.WriteByte(kvTypeEnd)
	buf.WriteByte(kvTypeEnd)

	var target struct {
		List []string `mapstructure:"list"`
	}

	err := UnmarshalBinaryKV(buf.Bytes(), &target)
	if err != nil {
		t.Fatal(err)
	}

	expected := []string{"a", "b"}
	if !reflect.DeepEqual(target.List, expected) {
		t.Errorf("expected %v, got %v", expected, target.List)
	}
}

func TestBVDFParser_Errors_Extended(t *testing.T) {
	tests := []struct {
		name string
		data []byte
	}{
		{"EOF during kind read", []byte{}},
		{"EOF during name read", []byte{kvTypeString}},
		{"EOF in recursive parse", []byte{kvTypeNone, 'n', 0}},
		{"EOF in wide string half-data", []byte{kvTypeWideString, 'n', 0, 0x61}}, // 1 byte of uint16
		{"Unknown type 99", []byte{99, 'n', 0}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var target any

			err := UnmarshalBinaryKV(tt.data, &target)
			if err == nil {
				t.Error("expected error for malformed data")
			}
		})
	}
}

func TestBVDFParser_EmptyNameOptimization_Deep(t *testing.T) {
	buf := new(bytes.Buffer)
	buf.WriteByte(kvTypeNone)
	encodeCString(buf, "")
	encodeCString(buf, "real_name")
	buf.WriteByte(kvTypeInt32)
	encodeCString(buf, "val")
	binary.Write(buf, binary.LittleEndian, int32(10))
	buf.WriteByte(kvTypeEnd)
	buf.WriteByte(kvTypeEnd)

	var target map[string]any

	err := UnmarshalBinaryKV(buf.Bytes(), &target)
	if err != nil {
		t.Fatal(err)
	}

	if _, ok := target["real_name"]; !ok {
		t.Error("failed to handle empty name header optimization")
	}
}

func TestUnmarshalResponse_FormatRaw_Error(t *testing.T) {
	data := []byte("some data")

	var wrongTarget int

	err := UnmarshalResponse(data, &wrongTarget, FormatRaw)
	if err == nil {
		t.Error("expected error when FormatRaw target is not *[]byte")
	}
}

func TestConvertToSliceIfNeeded(t *testing.T) {
	t.Run("Gaps", func(t *testing.T) {
		obj := map[string]any{
			"0": "first",
			"2": "third",
		}

		res := convertToSliceIfNeeded(obj)
		if _, ok := res.(map[string]any); !ok {
			t.Errorf("expected map due to index gap, got %T", res)
		}
	})

	t.Run("Empty", func(t *testing.T) {
		obj := map[string]any{}

		res := convertToSliceIfNeeded(obj)
		if _, ok := res.(map[string]any); !ok {
			t.Errorf("expected map due to index empty, got %T", res)
		}
	})
}

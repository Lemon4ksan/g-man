// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package encoding

import (
	"bytes"
	"errors"
	"fmt"
	"testing"

	"github.com/lemon4ksan/aoni/fast"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/emptypb"
)

type faultyReader struct{}

func (faultyReader) Read(p []byte) (n int, err error) {
	return 0, errors.New("read error")
}

func UnmarshalResponse(data []byte, target any, format ResponseFormat) error {
	if len(data) == 0 {
		return nil
	}

	switch format {
	case FormatRaw:
		if ptr, ok := target.(*[]byte); ok {
			*ptr = append([]byte(nil), data...)
			return nil
		}

		return fmt.Errorf("FormatRaw requires *[]byte as output type, got %T", target)

	case FormatProtobuf:
		return ProtobufDecoder(bytes.NewReader(data), target)
	case FormatJSON:
		return SteamJSONDecoder(bytes.NewReader(data), target)
	case FormatVDF:
		return VDFDecoder(bytes.NewReader(data), target)
	case FormatBinaryVDF:
		return BinaryVDFDecoder(bytes.NewReader(data), target)
	default:
		return fmt.Errorf("unsupported format %v", format)
	}
}

func TestUnmarshalResponse(t *testing.T) {
	t.Parallel()

	t.Run("wrapped_json", func(t *testing.T) {
		t.Parallel()

		data := []byte(`{"response": {"name": "G-Man"}}`)
		target := make(map[string]string)

		err := UnmarshalResponse(data, &target, FormatJSON)
		require.NoError(t, err)
		assert.Equal(t, "G-Man", target["name"])
	})

	t.Run("direct_json", func(t *testing.T) {
		t.Parallel()

		data := []byte(`{"name": "Gordon"}`)
		target := make(map[string]string)

		err := UnmarshalResponse(data, &target, FormatJSON)
		require.NoError(t, err)
		assert.Equal(t, "Gordon", target["name"])
	})

	t.Run("protobuf", func(t *testing.T) {
		t.Parallel()

		msg := &emptypb.Empty{}
		data, err := proto.Marshal(msg)
		require.NoError(t, err)

		target := &emptypb.Empty{}
		err = UnmarshalResponse(data, &target, FormatProtobuf)
		assert.NoError(t, err)
	})

	t.Run("vdf_text", func(t *testing.T) {
		t.Parallel()

		data := []byte(`"Player" { "Health" "100" }`)

		var target struct {
			Player struct {
				Health string `mapstructure:"Health"`
			} `mapstructure:"Player"`
		}

		err := UnmarshalResponse(data, &target, FormatVDF)
		require.NoError(t, err)
		assert.Equal(t, "100", target.Player.Health)
	})

	t.Run("format_raw", func(t *testing.T) {
		t.Parallel()

		data := []byte("raw_binary_data")

		var target []byte

		err := UnmarshalResponse(data, &target, FormatRaw)
		require.NoError(t, err)
		assert.Equal(t, "raw_binary_data", string(target))

		// Test error when target is not *[]byte
		var wrongTarget string

		err = UnmarshalResponse(data, &wrongTarget, FormatRaw)
		assert.Error(t, err)
	})

	t.Run("protobuf_json_detection", func(t *testing.T) {
		t.Parallel()

		data := []byte(`{}`)
		target := &emptypb.Empty{}

		err := UnmarshalResponse(data, target, FormatProtobuf)
		assert.NoError(t, err)
	})

	t.Run("vdf_wrapped", func(t *testing.T) {
		t.Parallel()

		data := []byte(`"response" { "success" "1" }`)

		var target struct {
			Success string `mapstructure:"success"`
		}

		err := UnmarshalResponse(data, &target, FormatVDF)
		require.NoError(t, err)
		assert.Equal(t, "1", target.Success)
	})

	t.Run("unsupported_format", func(t *testing.T) {
		t.Parallel()

		err := UnmarshalResponse([]byte("data"), nil, FormatUnknown)
		assert.Error(t, err)
	})

	t.Run("binary_vdf_success", func(t *testing.T) {
		t.Parallel()

		data := []byte{0, 'd', 'a', 't', 'a', 0, 2, 'i', 'd', 0, 123, 0, 0, 0, 8, 8}

		var target struct {
			Data struct {
				ID int `mapstructure:"id"`
			} `mapstructure:"data"`
		}

		err := UnmarshalResponse(data, &target, FormatBinaryVDF)
		require.NoError(t, err)
		assert.Equal(t, 123, target.Data.ID)
	})

	t.Run("binary_vdf_failure", func(t *testing.T) {
		t.Parallel()

		var target any

		err := UnmarshalResponse([]byte{0xFF}, &target, FormatBinaryVDF)
		assert.Error(t, err)
	})
}

func TestEmptyResponse(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		data []byte
	}{
		{"nil_data", nil},
		{"empty_slice", []byte{}},
		{"empty_json_response", []byte(`{"response": {}}`)},
		{"null_json_response", []byte(`{"response": null}`)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			target := make(map[string]any)

			err := UnmarshalResponse(tt.data, &target, FormatJSON)
			assert.NoError(t, err)
		})
	}
}

func TestUnmarshalProtobuf_WrongType(t *testing.T) {
	t.Parallel()

	err := ProtobufDecoder(bytes.NewReader([]byte("{}")), "not a proto message")
	assert.Error(t, err)
}

func TestUnmarshalVDFText_Invalid(t *testing.T) {
	t.Parallel()

	t.Run("invalid_syntax", func(t *testing.T) {
		t.Parallel()

		err := VDFDecoder(bytes.NewReader([]byte("invalid vdf {")), &struct{}{})
		assert.Error(t, err)
	})

	t.Run("non_pointer_target", func(t *testing.T) {
		t.Parallel()

		err := VDFDecoder(bytes.NewReader([]byte(`"Player" { "Health" "100" }`)), struct{}{})
		assert.Error(t, err)
	})
}

func TestUnmarshalJSON_EdgeCases(t *testing.T) {
	t.Parallel()

	t.Run("invalid_json", func(t *testing.T) {
		t.Parallel()

		var target map[string]any

		err := SteamJSONDecoder(bytes.NewReader([]byte(`{invalid}`)), &target)
		assert.Error(t, err)
	})

	t.Run("response_field_not_object", func(t *testing.T) {
		t.Parallel()

		data := []byte(`{"response": 123}`)

		var target int

		err := SteamJSONDecoder(bytes.NewReader(data), &target)
		require.NoError(t, err)
		assert.Equal(t, 123, target)
	})
}

func TestRequestModifiers(t *testing.T) {
	t.Parallel()

	t.Run("as_json", func(t *testing.T) {
		t.Parallel()

		req := fast.NewRequest(nil)
		defer req.Release()

		modifier := AsJSON()
		assert.NotNil(t, modifier)
		modifier(req)
	})

	t.Run("as_protobuf", func(t *testing.T) {
		t.Parallel()

		req := fast.NewRequest(nil)
		defer req.Release()

		modifier := AsProtobuf()
		assert.NotNil(t, modifier)
		modifier(req)
	})

	t.Run("as_vdf", func(t *testing.T) {
		t.Parallel()

		req := fast.NewRequest(nil)
		defer req.Release()

		modifier := AsVDF()
		assert.NotNil(t, modifier)
		modifier(req)
	})

	t.Run("as_binary_vdf", func(t *testing.T) {
		t.Parallel()

		req := fast.NewRequest(nil)
		defer req.Release()

		modifier := AsBinaryVDF()
		assert.NotNil(t, modifier)
		modifier(req)
	})
}

func TestSteamJSONDecoder_EdgeCases(t *testing.T) {
	t.Parallel()

	t.Run("read_error", func(t *testing.T) {
		t.Parallel()

		var target any

		err := SteamJSONDecoder(faultyReader{}, &target)
		assert.Error(t, err)
	})

	t.Run("html_xml_detection", func(t *testing.T) {
		t.Parallel()

		var target any

		err := SteamJSONDecoder(bytes.NewReader([]byte("   \n\t <html lang=\"en\">Oops</html>")), &target)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "expected JSON but got HTML/XML")
	})
}

func TestProtobufDecoder_EdgeCases(t *testing.T) {
	t.Parallel()

	t.Run("read_error", func(t *testing.T) {
		t.Parallel()

		target := &emptypb.Empty{}
		err := ProtobufDecoder(faultyReader{}, target)
		assert.Error(t, err)
	})

	t.Run("empty_body", func(t *testing.T) {
		t.Parallel()

		target := &emptypb.Empty{}
		err := ProtobufDecoder(bytes.NewReader([]byte{}), target)
		assert.NoError(t, err)
	})

	t.Run("malformed_binary", func(t *testing.T) {
		t.Parallel()

		target := &emptypb.Empty{}
		err := ProtobufDecoder(bytes.NewReader([]byte{0x08, 0xFF, 0xFF}), target)
		assert.Error(t, err)
	})
}

func TestVDFDecoder_InitFailure(t *testing.T) {
	t.Parallel()

	err := VDFDecoder(bytes.NewReader([]byte(`"response" { "success" "1" }`)), struct{}{})
	assert.Error(t, err)
}

// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package api

import (
	"bytes"
	"encoding/json"
	"fmt"

	"github.com/andygrunwald/vdf"
	"github.com/mitchellh/mapstructure"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

// ResponseFormat defines the expected encoding of the Steam API response.
type ResponseFormat int

const (
	// FormatUnknown is the default state.
	FormatUnknown ResponseFormat = iota
	// FormatRaw returns the response body as-is without parsing.
	FormatRaw
	// FormatProtobuf parses binary or JSON-encoded Protobuf messages.
	FormatProtobuf
	// FormatJSON parses standard JSON, automatically unwrapping the "response" field if present.
	FormatJSON
	// FormatVDF parses KeyValues/VDF text format.
	FormatVDF
	// FormatBinaryKV parses Binary KeyValues, which is a Valve-proprietary format
	FormatBinaryKV
)

// UnmarshalerFunc describes the function signature for decoding data. Accepts
// the raw response bytes and the target object for writing the result.
type UnmarshalerFunc func(data []byte, target any) error

// UnmarshalRegistry is a thread-safe registry of decoders. Allows centralized
// management of Steam API response handling methods, adding custom formats or
// overriding standard ones without changing the library code.
type UnmarshalRegistry struct {
	decoders map[ResponseFormat]UnmarshalerFunc
}

// NewUnmarshalRegistry creates and initializes a new registry
// with standard decoders (JSON, Protobuf, VDF, BinaryKV, Raw).
func NewUnmarshalRegistry() *UnmarshalRegistry {
	r := &UnmarshalRegistry{
		decoders: make(map[ResponseFormat]UnmarshalerFunc),
	}

	r.Register(FormatRaw, UnmarshalRaw)
	r.Register(FormatProtobuf, UnmarshalProtobuf)
	r.Register(FormatJSON, UnmarshalJSON)
	r.Register(FormatVDF, UnmarshalVDFText)
	r.Register(FormatBinaryKV, UnmarshalBinaryKV)

	return r
}

// Register registers a new decoding function for the specified format. If the
// format already exists, it will be overwritten. The method is safe for use
// across different goroutines.
func (r *UnmarshalRegistry) Register(format ResponseFormat, fn UnmarshalerFunc) {
	r.decoders[format] = fn
}

// Unmarshal searches the registry for a suitable decoder and runs it. If the
// data is empty (len=0), the method returns nil without performing decoding.
// Returns an error if the format is not registered in the registry.
func (r *UnmarshalRegistry) Unmarshal(data []byte, target any, format ResponseFormat) error {
	if len(data) == 0 {
		return nil
	}

	fn, ok := r.decoders[format]
	if !ok {
		return fmt.Errorf("%w: unsupported or unregistered format %v", ErrFormat, format)
	}

	return fn(data, target)
}

// UnmarshalRaw implements the standard UnmarshalerFunc for the FormatRaw format.
// It expects target to be a pointer to a byte slice (*[]byte), and copies the
// contents of data into it.
func UnmarshalRaw(data []byte, target any) error {
	if ptr, ok := target.(*[]byte); ok {
		*ptr = append([]byte(nil), data...)
		return nil
	}

	return fmt.Errorf("%w: FormatRaw requires *[]byte as output type, got %T", ErrFormat, target)
}

// UnmarshalProtobuf decodes Protobuf data. It automatically detects if the
// source is JSON-encoded Protobuf or standard binary wire format.
func UnmarshalProtobuf(data []byte, target any) error {
	pm, ok := target.(proto.Message)
	if !ok {
		return fmt.Errorf("%w: target is not a proto.Message", ErrFormat)
	}

	if len(data) > 0 && data[0] == '{' {
		return protojson.UnmarshalOptions{DiscardUnknown: true}.Unmarshal(data, pm)
	}

	return proto.Unmarshal(data, pm)
}

// UnmarshalJSON decodes JSON data. If the JSON contains a top-level "response"
// key (common in Steam Web API), it automatically drills down into it.
func UnmarshalJSON(data []byte, target any) error {
	var wrapper map[string]json.RawMessage
	if err := json.Unmarshal(data, &wrapper); err == nil {
		if inner, ok := wrapper["response"]; ok {
			return json.Unmarshal(inner, target)
		}
	}

	return json.Unmarshal(data, target)
}

// UnmarshalVDFText parses Valve Data Format (KeyValues) text.
// Like UnmarshalJSON, it automatically handles the "response" wrapper.
func UnmarshalVDFText(data []byte, target any) error {
	p := vdf.NewParser(bytes.NewReader(data))

	m, err := p.Parse()
	if err != nil {
		return err
	}

	config := &mapstructure.DecoderConfig{
		WeaklyTypedInput: true,
		Result:           target,
		Squash:           true,
	}

	decoder, err := mapstructure.NewDecoder(config)
	if err != nil {
		return err
	}

	if res, ok := m["response"].(map[string]any); ok {
		return decoder.Decode(res)
	}

	return decoder.Decode(m)
}

// UnmarshalBinaryKV parses a byte array in Binary KeyValues format into map[string]any.
func UnmarshalBinaryKV(data []byte, target any) error {
	p := &bvdfParser{data: data, offset: 0}

	res, err := p.parse()
	if err != nil {
		return err
	}

	parsed, ok := res.(map[string]any)
	if !ok {
		return fmt.Errorf("%w: root of binary vdf is not an object", ErrFormat)
	}

	config := &mapstructure.DecoderConfig{
		WeaklyTypedInput: true,
		Result:           target,
		Squash:           true,
	}

	decoder, err := mapstructure.NewDecoder(config)
	if err != nil {
		return err
	}

	return decoder.Decode(parsed)
}

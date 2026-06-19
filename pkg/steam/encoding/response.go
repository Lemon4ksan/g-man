// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package encoding

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"github.com/andygrunwald/vdf"
	"github.com/lemon4ksan/aoni"
	"github.com/mitchellh/mapstructure"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

// ErrFormat is returned when the response doesn't
// match the specified format or the target is invalid.
var ErrFormat = errors.New("api: response format error")

// ResponseFormat defines the expected encoding of the Steam API response.
type ResponseFormat int

const (
	// FormatUnknown is the default state.
	FormatUnknown ResponseFormat = iota
	// FormatRaw returns the response body as-is without parsing.
	FormatRaw
	// FormatJSON parses standard JSON, automatically unwrapping the "response" field if present.
	FormatJSON
	// FormatProtobuf parses binary or JSON-encoded Protobuf messages.
	FormatProtobuf
	// FormatXML parses XML text format.
	FormatXML
	// FormatYAML parses YAML text format.
	FormatYAML
	// FormatVDF parses KeyValues/VDF text format.
	FormatVDF
	// FormatBinaryVDF parses Binary KeyValues, which is a Valve-proprietary format
	FormatBinaryVDF
)

// UnmarshalBinaryKVOffset parses a byte array in Binary KeyValues format starting from a specific offset and updates the offset.
func UnmarshalBinaryKVOffset(data []byte, offset *int, target any) error {
	p := &bvdfParser{data: data, offset: *offset}

	res, err := p.parse()
	if err != nil {
		return fmt.Errorf("%w: %w", ErrFormat, err)
	}

	*offset = p.offset

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

// aoni.Decoder implementations

// SteamJSONDecoder wraps aoni.JSONDecoder to automatically unwrap the "response" field
// common in Steam Web API.
var SteamJSONDecoder = aoni.DecoderFunc(func(r io.Reader, target any) error {
	data, err := io.ReadAll(r)
	if err != nil {
		return err
	}

	firstNonSpace := byte(0)
	for _, b := range data {
		if b != ' ' && b != '\t' && b != '\r' && b != '\n' {
			firstNonSpace = b
			break
		}
	}

	if firstNonSpace == '<' {
		limit := min(len(data), 100)
		return fmt.Errorf("expected JSON but got HTML/XML (possible steam API outage): %s", string(data[:limit]))
	}

	var wrapper map[string]json.RawMessage
	if err := json.Unmarshal(data, &wrapper); err == nil {
		if inner, ok := wrapper["response"]; ok {
			return json.Unmarshal(inner, target)
		}
	}

	return json.Unmarshal(data, target)
})

// ProtobufDecoder parses Protobuf payloads into a [proto.Message].
// It automatically detects whether the input data is JSON-encoded Protobuf
// or binary wire format. The target argument must satisfy [proto.Message].
// Returns an error if target does not satisfy [proto.Message] or if decoding fails.
var ProtobufDecoder = aoni.DecoderFunc(func(r io.Reader, target any) error {
	pm, ok := target.(proto.Message)
	if !ok {
		return errors.New("aoni: target is not a proto.Message")
	}

	data, err := io.ReadAll(r)
	if err != nil {
		return err
	}

	if len(data) > 0 && data[0] == '{' {
		return protojson.UnmarshalOptions{DiscardUnknown: true}.Unmarshal(data, pm)
	}

	return proto.Unmarshal(data, pm)
})

// VDFDecoder parses Valve Data Format (KeyValues) text.
var VDFDecoder = aoni.DecoderFunc(func(r io.Reader, target any) error {
	p := vdf.NewParser(r)

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
})

// BinaryVDFDecoder parses Valve Binary KeyValues format.
var BinaryVDFDecoder = aoni.DecoderFunc(func(r io.Reader, target any) error {
	data, err := io.ReadAll(r)
	if err != nil {
		return err
	}

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
})

// AsJSON returns a [RequestModifier] that configures the client to use [SteamJSONDecoder].
func AsJSON() aoni.RequestModifier { return aoni.WithDecoder(SteamJSONDecoder) }

// AsProtobuf returns a [RequestModifier] that configures the client to use [ProtobufDecoder].
func AsProtobuf() aoni.RequestModifier { return aoni.WithDecoder(ProtobufDecoder) }

// AsVDF returns a [RequestModifier] that configures the client to use [VDFDecoder].
func AsVDF() aoni.RequestModifier { return aoni.WithDecoder(VDFDecoder) }

// AsBinaryVDF returns a [RequestModifier] that configures the client to use [BinaryVDFDecoder].
func AsBinaryVDF() aoni.RequestModifier { return aoni.WithDecoder(BinaryVDFDecoder) }

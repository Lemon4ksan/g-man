// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package encoding implements decoders and request modifiers tailored for Steam Web API and serialization formats (JSON, Protobuf, and VDF).
package encoding

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"github.com/andygrunwald/vdf"
	"github.com/lemon4ksan/aoni"
	"github.com/mitchellh/mapstructure"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"

	"github.com/lemon4ksan/g-man/pkg/steam/encoding/bvdf"
)

// ErrFormat is returned when the response format is unexpected or when target structures are incompatible.
var ErrFormat = errors.New("api: response format error")

// ResponseFormat defines the expected encoding type of a Steam API response payload.
// It is used to specify how raw bytes should be parsed by corresponding decoders.
type ResponseFormat int

const (
	// FormatUnknown represents an uninitialized or unsupported response format.
	FormatUnknown ResponseFormat = iota
	// FormatRaw represents raw, unparsed response bytes.
	FormatRaw
	// FormatJSON represents standard JSON format with automatic "response" object unwrapping.
	FormatJSON
	// FormatProtobuf represents a binary wire format or JSON-encoded Protobuf payload.
	FormatProtobuf
	// FormatXML represents XML text payload format.
	FormatXML
	// FormatYAML represents YAML text payload format.
	FormatYAML
	// FormatVDF represents KeyValues/VDF text payload format.
	FormatVDF
	// FormatBinaryVDF represents Valve Proprietary Binary KeyValues payload format.
	FormatBinaryVDF
)

// SteamJSONDecoder wraps standard JSON decoding and automatically extracts the inner "response" object if present.
// It returns an error if the input looks like HTML or XML, which typically indicates a Steam API outage.
// It returns decoding errors if the payload is malformed or if the target is invalid.
// If the reader is nil, the decoder will return a read error.
var SteamJSONDecoder = aoni.DecoderFunc(func(r io.Reader, target any) error {
	data, err := io.ReadAll(r)
	if err != nil {
		return err
	}

	trimmed := bytes.TrimSpace(data)
	if len(trimmed) > 0 && trimmed[0] == '<' {
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

// ProtobufDecoder parses Protobuf payloads into a [proto.Message] structure.
// It detects whether the payload is JSON-encoded Protobuf or standard binary wire format.
// It returns an error if the target argument does not implement [proto.Message].
// If the reader is nil, the decoder will return a read error.
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

// VDFDecoder parses Valve Data Format (VDF) text KeyValues into a target object.
// It automatically unwraps the "response" parent key if it is present in the document.
// It returns formatting errors if parsing fails, or mapping errors if the target is incompatible.
// If the reader is nil, the decoder will return a read error.
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

// BinaryVDFDecoder parses Valve Proprietary Binary KeyValues payload using [bvdf.Unmarshal].
// It returns formatting errors if the binary structure is corrupted.
// The target argument must be a non-nil pointer to a map or struct.
var BinaryVDFDecoder = aoni.DecoderFunc(bvdf.Unmarshal)

// AsJSON returns an [aoni.RequestModifier] that configures the client to use [SteamJSONDecoder] for decoding.
func AsJSON() aoni.RequestModifier { return aoni.WithDecoder(SteamJSONDecoder) }

// AsProtobuf returns an [aoni.RequestModifier] that configures the client to use [ProtobufDecoder] for decoding.
func AsProtobuf() aoni.RequestModifier { return aoni.WithDecoder(ProtobufDecoder) }

// AsVDF returns an [aoni.RequestModifier] that configures the client to use [VDFDecoder] for decoding.
func AsVDF() aoni.RequestModifier { return aoni.WithDecoder(VDFDecoder) }

// AsBinaryVDF returns an [aoni.RequestModifier] that configures the client to use [BinaryVDFDecoder] for decoding.
func AsBinaryVDF() aoni.RequestModifier { return aoni.WithDecoder(BinaryVDFDecoder) }

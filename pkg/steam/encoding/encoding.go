// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package encoding provides custom decoders and request modifiers for Steam JSON, Protobuf, VDF, and Binary VDF responses.
package encoding

import (
	"bytes"
	"errors"
	"fmt"
	"io"

	"github.com/andygrunwald/vdf"
	json "github.com/goccy/go-json"
	"github.com/lemon4ksan/aoni"
	"github.com/lemon4ksan/aoni/codec/decode"
	"github.com/lemon4ksan/aoni/mod"
	"github.com/mitchellh/mapstructure"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"

	"github.com/lemon4ksan/g-man/pkg/steam/encoding/bvdf"
	"github.com/lemon4ksan/g-man/pkg/steam/protocol"
)

var (
	// ErrFormat indicates a response payload formatting mismatch.
	ErrFormat = errors.New("api: response format error")
	// ErrEmptyResponseBody indicates a response payload body stream was empty.
	ErrEmptyResponseBody = errors.New("steam: empty response body")
	// ErrNotProtoMessage indicates a decoding target does not satisfy proto.Message.
	ErrNotProtoMessage = errors.New("aoni: target is not a proto.Message")
)

type ResponseFormat int

const (
	FormatUnknown ResponseFormat = iota
	FormatRaw
	FormatJSON
	FormatProtobuf
	FormatVDF
	FormatBinaryVDF
)

// RapidValidateSteamResponse checks leading bytes for HTML/XML error tags indicating server outage.
func RapidValidateSteamResponse(data []byte) error {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) > 0 && trimmed[0] == '<' {
		limit := min(len(trimmed), 128)

		return fmt.Errorf("expected JSON but got HTML/XML (possible steam API outage): %s", string(trimmed[:limit]))
	}

	return nil
}

// SteamJSONDecoder decodes JSON streams and automatically unwraps nested "response" wrapper objects.
var SteamJSONDecoder = decode.DecoderFunc(func(r io.Reader, target any) error {
	data, err := io.ReadAll(r)
	if err != nil {
		return err
	}

	if len(data) == 0 {
		return ErrEmptyResponseBody
	}

	if err := RapidValidateSteamResponse(data); err != nil {
		return err
	}

	var wrapper map[string]json.RawMessage
	if err := json.Unmarshal(data, &wrapper); err == nil {
		if inner, ok := wrapper["response"]; ok && len(inner) > 0 {
			return json.Unmarshal(inner, target)
		}
	}

	return json.Unmarshal(data, target)
})

// ProtobufDecoder parses binary Protobuf or JSON-encoded Protobuf payloads into proto.Message.
var ProtobufDecoder = decode.DecoderFunc(func(r io.Reader, target any) error {
	pm, ok := target.(proto.Message)
	if !ok {
		return ErrNotProtoMessage
	}

	data, err := io.ReadAll(r)
	if err != nil {
		return err
	}

	if len(data) > 0 && data[0] == '{' {
		return protojson.UnmarshalOptions{DiscardUnknown: true}.Unmarshal(data, pm)
	}

	return protocol.UnmarshalProto(data, pm)
})

// VDFDecoder decodes text Valve Data Format (VDF) KeyValues payloads.
var VDFDecoder = decode.DecoderFunc(func(r io.Reader, target any) error {
	p := vdf.NewParser(r)

	m, err := p.Parse()
	if err != nil {
		return err
	}

	if res, ok := m["response"].(map[string]any); ok {
		m = res
	}

	if targetMap, ok := target.(*map[string]any); ok {
		*targetMap = m

		return nil
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

	return decoder.Decode(m)
})

// BinaryVDFDecoder decodes Valve Proprietary Binary KeyValues payloads using bvdf.
var BinaryVDFDecoder = decode.DecoderFunc(bvdf.Unmarshal)

func AsJSON() aoni.RequestModifier      { return mod.WithDecoder(SteamJSONDecoder) }
func AsProtobuf() aoni.RequestModifier  { return mod.WithDecoder(ProtobufDecoder) }
func AsVDF() aoni.RequestModifier       { return mod.WithDecoder(VDFDecoder) }
func AsBinaryVDF() aoni.RequestModifier { return mod.WithDecoder(BinaryVDFDecoder) }

// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/lemon4ksan/g-man/steam/protocol"
	tr "github.com/lemon4ksan/g-man/steam/transport"

	"github.com/andygrunwald/vdf"
	"github.com/mitchellh/mapstructure"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

const WebAPIBase = "https://api.steampowered.com/"

type Requester interface {
	Do(req *tr.Request) (*tr.Response, error)
}

type UnifiedRequester interface {
	Requester
	CallUnified(ctx context.Context, httpMethod, iface, method string, version int, reqMsg, respMsg any, mods ...RequestModifier) error
}

type WebAPIRequester interface {
	Requester
	CallWebAPI(ctx context.Context, iface, method string, version int, respMsg any, mods ...RequestModifier) error
}

type LegacyRequester interface {
	Requester
	CallLegacy(ctx context.Context, eMsg protocol.EMsg, reqMsg, respMsg proto.Message, mods ...RequestModifier) error
}

type UnifiedLegacyRequester interface {
	UnifiedRequester
	LegacyRequester
}

type VDFUnmarshaler interface {
	UnmarshalVDF(data []byte) error
}

type UnifiedClient struct {
	transport   tr.Transport
	apiKey      string
	accessToken string
}

func NewUnifiedClient(tr tr.Transport) *UnifiedClient {
	return &UnifiedClient{transport: tr}
}

func (c *UnifiedClient) WithAPIKey(key string) *UnifiedClient {
	c.apiKey = key
	return c
}

func (c *UnifiedClient) WithAccessToken(token string) *UnifiedClient {
	c.accessToken = token
	return c
}

func (c *UnifiedClient) Transport() tr.Transport { return c.transport }

func (c *UnifiedClient) Do(req *tr.Request) (*tr.Response, error) {
	if c.apiKey != "" {
		req.WithParam("key", c.apiKey)
	}
	if c.accessToken != "" {
		req.WithParam("access_token", c.accessToken)
	}

	apiResp, err := c.transport.Do(req)
	if err != nil {
		return nil, fmt.Errorf("transport error: %w", err)
	}

	if apiResp.StatusCode != 0 && apiResp.StatusCode != http.StatusOK {
		return nil, &SteamAPIError{StatusCode: apiResp.StatusCode}
	}

	if apiResp.Result != 0 && apiResp.Result != protocol.EResult_OK {
		return nil, &EResultError{EResult: apiResp.Result}
	}

	return apiResp, nil
}

func (c *UnifiedClient) CallUnified(ctx context.Context, httpMethod, iface, method string, version int, reqMsg, respMsg any, mods ...RequestModifier) error {
	req, err := NewUnifiedRequest(ctx, httpMethod, iface, method, version, reqMsg)
	if err != nil {
		return err
	}
	for _, mod := range mods {
		mod(req)
	}

	resp, err := c.Do(req)
	if err != nil || resp == nil {
		return err
	}
	return c.unmarshalResponse(resp.Body, respMsg)
}

func (c *UnifiedClient) CallWebAPI(ctx context.Context, iface, method string, version int, respMsg any, mods ...RequestModifier) error {
	req := NewWebAPIRequest(ctx, "GET", iface, method, version)
	for _, mod := range mods {
		mod(req)
	}

	resp, err := c.Do(req)
	if err != nil || resp == nil {
		return err
	}
	return c.unmarshalResponse(resp.Body, respMsg)
}

func (c *UnifiedClient) CallLegacy(ctx context.Context, eMsg protocol.EMsg, reqMsg, respMsg proto.Message, mods ...RequestModifier) error {
	req, err := NewLegacyRequest(ctx, eMsg, reqMsg)
	if err != nil {
		return err
	}
	for _, mod := range mods {
		mod(req)
	}

	resp, err := c.Do(req)
	if err != nil || resp == nil {
		return err
	}
	return c.unmarshalResponse(resp.Body, respMsg)
}

func (c *UnifiedClient) unmarshalResponse(data []byte, msg any) error {
	if len(data) == 0 || (len(data) == 1 && data[0] == 0x00) {
		return nil
	}

	// Steam's WebAPI is messy, it might return JSON {"response": {...}} even
	// if you asked for Protobuf, or return a binary Protobuf if the stars align.
	var wrapper map[string]json.RawMessage
	if err := json.Unmarshal(data, &wrapper); err == nil {
		if inner, ok := wrapper["response"]; ok {
			if len(inner) == 0 || string(inner) == "null" || string(inner) == "{}" {
				return nil
			}
			return c.decode(inner, msg)
		}
	}

	return c.decode(data, msg)
}

func (c *UnifiedClient) decode(data []byte, target any) error {
	if len(data) == 0 {
		return nil
	}

	if unmarshaler, ok := target.(VDFUnmarshaler); ok {
		return unmarshaler.UnmarshalVDF(data)
	}

	if pm, ok := target.(proto.Message); ok {
		if json.Valid(data) {
			return protojson.UnmarshalOptions{DiscardUnknown: true}.Unmarshal(data, pm)
		}
		return proto.Unmarshal(data, pm)
	}

	if len(data) > 0 && data[0] == 0x22 {
		return c.unmarshalVDFText(data, target)
	}

	if len(data) > 0 && data[0] == 0x00 {
		return c.unmarshalVDFBinary(data, target)
	}

	return json.Unmarshal(data, target)
}

func (c *UnifiedClient) unmarshalVDFText(data []byte, target any) error {
	p := vdf.NewParser(bytes.NewReader(data))
	m, _ := p.Parse()
	return mapstructure.Decode(m, target)
}

func (c *UnifiedClient) unmarshalVDFBinary(data []byte, target any) error {
	return fmt.Errorf("vdf binary parser not implemented")
}

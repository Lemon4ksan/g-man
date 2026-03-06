// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"

	"github.com/lemon4ksan/g-man/steam/protocol"
	tr "github.com/lemon4ksan/g-man/steam/transport"
	"google.golang.org/protobuf/proto"
)

// RequestModifier allows injecting headers or params into a request before execution.
type RequestModifier func(req *tr.Request)

type HttpTarget struct {
	HttpMethod string
	URL        string
}

func (c HttpTarget) String() string { return c.URL }
func (c HttpTarget) HTTPMethod() string {
	if c.HttpMethod != "" {
		return c.HttpMethod
	}
	return "GET"
}
func (c HttpTarget) HTTPPath() string {
	u, _ := url.Parse(c.URL)
	return strings.TrimPrefix(u.Path, "/")
}

func NewHttpRequest(ctx context.Context, httpMethod string, url string, body []byte) *tr.Request {
	return tr.NewRequest(ctx, HttpTarget{HttpMethod: httpMethod, URL: url}, body)
}

type UnifiedTarget struct {
	HttpMethod string
	Interface  string
	MethodName string
	Version    int
	IsService  bool
}

// NewUnifiedRequest creates a request for a modern Service method.
// msg can be a proto.Message, raw []byte, or any struct (which will be JSON encoded).
func NewUnifiedRequest(ctx context.Context, httpMethod, iface, method string, version int, msg any) (*tr.Request, error) {
	var body []byte
	var err error

	switch v := msg.(type) {
	case nil:
		body = nil
	case proto.Message:
		body, err = proto.Marshal(v)
	case []byte:
		body = v
	default:
		body, err = json.Marshal(v)
	}

	if err != nil {
		return nil, fmt.Errorf("api: failed to encode unified body: %w", err)
	}

	target := UnifiedTarget{
		HttpMethod: httpMethod,
		Interface:  iface,
		MethodName: method,
		Version:    version,
		IsService:  true,
	}
	return tr.NewRequest(ctx, target, body), nil
}

func (u UnifiedTarget) String() string {
	return fmt.Sprintf("%s.%s#%d", u.Interface, u.MethodName, u.Version)
}
func (u UnifiedTarget) HTTPMethod() string {
	if u.HttpMethod != "" {
		return u.HttpMethod
	}
	return "POST"
}
func (u UnifiedTarget) HTTPPath() string {
	iface := u.Interface
	if iface[0] != 'I' {
		iface = "I" + iface
	}
	if u.IsService && !strings.HasSuffix(iface, "Service") {
		iface += "Service"
	}
	return fmt.Sprintf("%s/%s/v%d", iface, u.MethodName, u.Version)
}
func (u UnifiedTarget) EMsg(isAuth bool) protocol.EMsg {
	if isAuth {
		return protocol.EMsg_ServiceMethodCallFromClient
	}
	return protocol.EMsg_ServiceMethodCallFromClientNonAuthed
}
func (u UnifiedTarget) ObjectName() string { return u.String() }

type WebAPITarget struct {
	HttpMethod string
	Interface  string
	Method     string
	Version    int
}

func NewWebAPIRequest(ctx context.Context, httpMethod, iface, method string, version int) *tr.Request {
	return tr.NewRequest(ctx, WebAPITarget{HttpMethod: httpMethod, Interface: iface, Method: method, Version: version}, nil)
}

func (w WebAPITarget) String() string     { return w.Interface + "/" + w.Method }
func (w WebAPITarget) HTTPMethod() string { return w.HttpMethod }
func (w WebAPITarget) HTTPPath() string {
	return fmt.Sprintf("%s/%s/v%d", w.Interface, w.Method, w.Version)
}

type LegacyTarget struct {
	eMsg protocol.EMsg
}

func NewLegacyRequest(ctx context.Context, eMsg protocol.EMsg, msg proto.Message) (*tr.Request, error) {
	var body []byte
	if msg != nil {
		var err error
		body, err = proto.Marshal(msg)
		if err != nil {
			return nil, fmt.Errorf("api: failed to marshal legacy body: %w", err)
		}
	}
	return tr.NewRequest(ctx, LegacyTarget{eMsg}, body), nil
}

func (l LegacyTarget) String() string                 { return l.eMsg.String() }
func (l LegacyTarget) EMsg(isAuth bool) protocol.EMsg { return l.eMsg }
func (l LegacyTarget) ObjectName() string             { return "" }

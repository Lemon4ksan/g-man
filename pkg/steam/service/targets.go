// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package service

import (
	"bytes"
	"fmt"
	"strings"

	json "github.com/goccy/go-json"
	"google.golang.org/protobuf/proto"

	"github.com/lemon4ksan/g-man/pkg/steam/protocol/enums"
	tr "github.com/lemon4ksan/g-man/pkg/steam/transport"
)

// UnifiedTarget represents a Service method target supporting both WebAPI and socket calls.
type UnifiedTarget struct {
	HttpMethod string
	Interface  string
	Method     string
	Version    int
	IsService  bool
}

// NewUnifiedRequest constructs a transport request for a Unified Service call.
func NewUnifiedRequest(httpMethod, iface, method string, version int, msg any) (*tr.Request, error) {
	body, err := marshalBody(msg)
	if err != nil {
		return nil, fmt.Errorf("service: failed to encode unified body: %w", err)
	}

	target := &UnifiedTarget{
		HttpMethod: httpMethod,
		Interface:  iface,
		Method:     method,
		Version:    version,
		IsService:  true,
	}

	return tr.NewRequest(target, bytes.NewReader(body)), nil
}

func (u *UnifiedTarget) String() string {
	return fmt.Sprintf("%s.%s#%d", u.Interface, u.Method, u.Version)
}

func (u *UnifiedTarget) HTTPMethod() string {
	if u.HttpMethod != "" {
		return u.HttpMethod
	}

	return "POST"
}

func (u *UnifiedTarget) HTTPPath() string {
	iface := u.Interface
	if !strings.HasPrefix(iface, "I") {
		iface = "I" + iface
	}

	if u.IsService && !strings.HasSuffix(iface, "Service") {
		iface += "Service"
	}

	return fmt.Sprintf("%s/%s/v%d", iface, u.Method, u.Version)
}

func (u *UnifiedTarget) EMsg(isAuth bool) enums.EMsg {
	if isAuth {
		return enums.EMsg_ServiceMethodCallFromClient
	}

	return enums.EMsg_ServiceMethodCallFromClientNonAuthed
}

func (u *UnifiedTarget) SetHTTPMethod(method string) { u.HttpMethod = method }
func (u *UnifiedTarget) SetVersion(v int)            { u.Version = v }
func (u *UnifiedTarget) ObjectName() string          { return u.String() }

// WebAPITarget represents standard WebAPI endpoints.
type WebAPITarget struct {
	HttpMethod string
	Interface  string
	Method     string
	Version    int
}

// NewWebAPIRequest constructs a transport request for a standard WebAPI call.
func NewWebAPIRequest(httpMethod, iface, method string, version int) *tr.Request {
	return tr.NewRequest(&WebAPITarget{
		HttpMethod: httpMethod,
		Interface:  iface,
		Method:     method,
		Version:    version,
	}, nil)
}

func (w *WebAPITarget) String() string { return w.Interface + "/" + w.Method }

func (w *WebAPITarget) HTTPMethod() string { return w.HttpMethod }

func (w *WebAPITarget) HTTPPath() string {
	return fmt.Sprintf("%s/%s/v%d", w.Interface, w.Method, w.Version)
}

func (w *WebAPITarget) SetHTTPMethod(m string) { w.HttpMethod = m }
func (w *WebAPITarget) ObjectName() string     { return fmt.Sprintf("%s/%s", w.Interface, w.Method) }
func (w *WebAPITarget) SetVersion(v int)       { w.Version = v }

// LegacyTarget represents raw EMsg socket calls.
type LegacyTarget struct {
	eMsg enums.EMsg
}

// NewLegacyRequest constructs a transport request for an EMsg socket call.
func NewLegacyRequest(eMsg enums.EMsg, msg proto.Message) (*tr.Request, error) {
	body, err := marshalBody(msg)
	if err != nil {
		return nil, fmt.Errorf("service: failed to marshal legacy body: %w", err)
	}

	return tr.NewRequest(&LegacyTarget{eMsg}, bytes.NewReader(body)), nil
}

// NewLegacyProtoRequest constructs a transport request for an EMsg socket call forcing Protobuf headers.
func NewLegacyProtoRequest(eMsg enums.EMsg, msg proto.Message) (*tr.Request, error) {
	req, err := NewLegacyRequest(eMsg, msg)
	if err != nil {
		return nil, err
	}

	return req.WithForceProto(), nil
}

func (l *LegacyTarget) String() string              { return l.eMsg.String() }
func (l *LegacyTarget) EMsg(isAuth bool) enums.EMsg { return l.eMsg }
func (l *LegacyTarget) ObjectName() string          { return "" }

func marshalBody(msg any) ([]byte, error) {
	if msg == nil {
		return nil, nil
	}

	switch v := msg.(type) {
	case proto.Message:
		return proto.Marshal(v)
	case []byte:
		return v, nil
	default:
		return json.Marshal(v)
	}
}

// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package service

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"

	"google.golang.org/protobuf/proto"

	"github.com/lemon4ksan/g-man/pkg/steam/protocol/enums"
	tr "github.com/lemon4ksan/g-man/pkg/steam/transport"
)

// UnifiedTarget represents a modern Steam Service method call.
// It supports both HTTP routing (via path) and Socket routing (via EMsg).
type UnifiedTarget struct {
	// HttpMethod is the verb used for web requests (default is POST).
	HttpMethod string
	// Interface is the name of the service (for example, "Player").
	Interface string
	// Method is the name of the RPC function (for example, "GetNickname").
	Method string
	// Version is the API version (for example, 1).
	Version int
	// IsService determines if the "Service" suffix is appended to the interface name in HTTP paths.
	IsService bool
}

// NewUnifiedRequest creates a transport request for a Service method.
// The msg parameter can be a proto.Message, raw []byte, or a struct (which will be JSON encoded).
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

// String returns a human-readable identifier for the UnifiedTarget.
func (u *UnifiedTarget) String() string {
	return fmt.Sprintf("%s.%s#%d", u.Interface, u.Method, u.Version)
}

// HTTPMethod returns "POST" if not explicitly set, as Unified Services require a body.
func (u *UnifiedTarget) HTTPMethod() string {
	if u.HttpMethod != "" {
		return u.HttpMethod
	}

	return "POST"
}

// HTTPPath constructs the Steam URL path, e.g., "IPlayerService/GetNickname/v1".
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

// EMsg returns the appropriate EMsg for socket-based service calls.
func (u *UnifiedTarget) EMsg(isAuth bool) enums.EMsg {
	if isAuth {
		return enums.EMsg_ServiceMethodCallFromClient
	}

	return enums.EMsg_ServiceMethodCallFromClientNonAuthed
}

// SetHTTPMethod updates the HTTP method for the target.
func (u *UnifiedTarget) SetHTTPMethod(method string) { u.HttpMethod = method }

// SetVersion updates the API method version for the target.
func (u *UnifiedTarget) SetVersion(v int) { u.Version = v }

// ObjectName returns the name for the socket representation of the target.
func (u *UnifiedTarget) ObjectName() string { return u.String() }

// WebAPITarget represents a classic JSON/VDF WebAPI call.
type WebAPITarget struct {
	HttpMethod string
	Interface  string
	Method     string
	Version    int
}

// NewWebAPIRequest creates a transport request for a standard WebAPI endpoint.
func NewWebAPIRequest(httpMethod, iface, method string, version int) *tr.Request {
	return tr.NewRequest(&WebAPITarget{
		HttpMethod: httpMethod,
		Interface:  iface,
		Method:     method,
		Version:    version,
	}, nil)
}

// String returns a human-readable identifier for the WebAPITarget.
func (w *WebAPITarget) String() string { return w.Interface + "/" + w.Method }

// HTTPMethod returns the configured HTTP method.
func (w *WebAPITarget) HTTPMethod() string { return w.HttpMethod }

// HTTPPath constructs the Steam WebAPI URL path.
func (w *WebAPITarget) HTTPPath() string {
	return fmt.Sprintf("%s/%s/v%d", w.Interface, w.Method, w.Version)
}

// SetHTTPMethod updates the HTTP method for the target.
func (w *WebAPITarget) SetHTTPMethod(m string) { w.HttpMethod = m }

// ObjectName returns the WebAPI method name for identification purposes.
func (w *WebAPITarget) ObjectName() string { return fmt.Sprintf("%s/%s", w.Interface, w.Method) }

// SetVersion updates the WebAPI version for the target.
func (w *WebAPITarget) SetVersion(v int) { w.Version = v }

// LegacyTarget represents a raw EMsg-based message used in socket connections.
type LegacyTarget struct {
	eMsg enums.EMsg
}

// NewLegacyRequest creates a request identified solely by its EMsg.
func NewLegacyRequest(eMsg enums.EMsg, msg proto.Message) (*tr.Request, error) {
	body, err := marshalBody(msg)
	if err != nil {
		return nil, fmt.Errorf("service: failed to marshal legacy body: %w", err)
	}

	return tr.NewRequest(&LegacyTarget{eMsg}, bytes.NewReader(body)), nil
}

// NewLegacyProtoRequest forces a Protobuf CM header for the outer Steam packet.
func NewLegacyProtoRequest(eMsg enums.EMsg, msg proto.Message) (*tr.Request, error) {
	req, err := NewLegacyRequest(eMsg, msg)
	if err != nil {
		return nil, err
	}

	return req.WithForceProto(), nil
}

// String returns the string representation of the underlying EMsg.
func (l *LegacyTarget) String() string { return l.eMsg.String() }

// EMsg returns the associated EMsg for the target.
func (l *LegacyTarget) EMsg(isAuth bool) enums.EMsg { return l.eMsg }

// ObjectName returns an empty string as legacy targets do not have object names.
func (l *LegacyTarget) ObjectName() string { return "" }

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

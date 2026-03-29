// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package service

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/lemon4ksan/g-man/pkg/steam/protocol"
	tr "github.com/lemon4ksan/g-man/pkg/steam/transport"
	"google.golang.org/protobuf/proto"
)

// UnifiedTarget represents a modern Steam Service method call.
// It supports both HTTP routing (via path) and Socket routing (via EMsg).
type UnifiedTarget struct {
	HttpMethod string // Default is POST for Protobuf calls
	Interface  string // e.g., "Player"
	Method     string // e.g., "GetNickname"
	Version    int    // e.g., 1
	IsService  bool   // If true, appends "Service" to the interface name in HTTP paths
}

// NewUnifiedRequest creates a transport request for a Service method.
// The msg parameter can be a proto.Message, raw []byte, or a struct (which will be JSON encoded).
func NewUnifiedRequest(httpMethod, iface, method string, version int, msg any) (*tr.Request, error) {
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

	target := &UnifiedTarget{
		HttpMethod: httpMethod,
		Interface:  iface,
		Method:     method,
		Version:    version,
		IsService:  true,
	}
	return tr.NewRequest(target, body), nil
}

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
func (u *UnifiedTarget) EMsg(isAuth bool) protocol.EMsg {
	if isAuth {
		return protocol.EMsg_ServiceMethodCallFromClient
	}
	return protocol.EMsg_ServiceMethodCallFromClientNonAuthed
}

// SetHTTPMethod updates the http method for the target.
func (u *UnifiedTarget) SetHTTPMethod(method string) {
	u.HttpMethod = method
}

// SetVersion updates the api method version for the target.
func (u *UnifiedTarget) SetVersion(v int) {
	u.Version = v
}

// ObjectName returns the name for the socket representation of the target.
func (u *UnifiedTarget) ObjectName() string { return u.String() }

// WebAPITarget represents a classic JSON/VDF WebAPI call.
type WebAPITarget struct {
	HttpMethod string // e.g., "GET" or "POST"
	Interface  string // e.g., "ISteamUser"
	Method     string // e.g., "GetPlayerSummaries"
	Version    int    // e.g., 2
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

func (w *WebAPITarget) String() string     { return w.Interface + "/" + w.Method }
func (w *WebAPITarget) HTTPMethod() string { return w.HttpMethod }
func (w *WebAPITarget) HTTPPath() string {
	return fmt.Sprintf("%s/%s/v%d", w.Interface, w.Method, w.Version)
}

// SetHTTPMethod updates the http method for the target.
func (w *WebAPITarget) SetHTTPMethod(method string) {
	w.HttpMethod = method
}

// SetVersion updates the api method version for the target.
func (w *WebAPITarget) SetVersion(v int) {
	w.Version = v
}

// LegacyTarget represents a raw EMsg-based message used in socket connections.
type LegacyTarget struct {
	eMsg protocol.EMsg
}

// NewLegacyRequest creates a request identified solely by its EMsg.
func NewLegacyRequest(eMsg protocol.EMsg, msg proto.Message) (*tr.Request, error) {
	var body []byte
	if msg != nil {
		var err error
		body, err = proto.Marshal(msg)
		if err != nil {
			return nil, fmt.Errorf("api: failed to marshal legacy body: %w", err)
		}
	}
	return tr.NewRequest(&LegacyTarget{eMsg}, body), nil
}

func (l *LegacyTarget) String() string                 { return l.eMsg.String() }
func (l *LegacyTarget) EMsg(isAuth bool) protocol.EMsg { return l.eMsg }
func (l *LegacyTarget) ObjectName() string             { return "" }

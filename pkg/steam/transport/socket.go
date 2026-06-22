// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package transport

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/lemon4ksan/g-man/pkg/steam/protocol"
	"github.com/lemon4ksan/g-man/pkg/steam/protocol/enums"
	"github.com/lemon4ksan/g-man/pkg/steam/socket"
)

// SocketMetadata holds context-specific information from a socket-based response.
type SocketMetadata struct {
	// Result is the Steam EResult code extracted from the packet header.
	Result enums.EResult
	// Header contains the full, parsed binary packet header.
	Header protocol.Header
	// SourceJobID is the original Job ID that this message is a response to.
	SourceJobID uint64
}

// SocketTarget is an extension of the Target interface for destinations that can be
// reached via a persistent socket connection.
type SocketTarget interface {
	Target
	EMsg(isAuth bool) enums.EMsg
	ObjectName() string
}

// SocketCaller defines the minimum interface required by the transport to interact
// with the underlying socket.
type SocketCaller interface {
	Send(ctx context.Context, build socket.PayloadBuilder, opts ...socket.SendOption) error
	SendSync(ctx context.Context, build socket.PayloadBuilder, opts ...socket.SendOption) (*protocol.Packet, error)
	Session() socket.Session
}

// SocketTransport implements the [Transport] interface for socket-based communication.
// It translates abstract [Request] structures into concrete [protocol.Packet] messages.
//
// Create new instances of SocketTransport using [NewSocketTransport].
type SocketTransport struct {
	caller SocketCaller
}

// NewSocketTransport creates a new socket transport layer.
func NewSocketTransport(caller SocketCaller) *SocketTransport {
	return &SocketTransport{
		caller: caller,
	}
}

// Do executes a [Request] over a persistent socket connection.
//
// It returns an error if the request's [Target] does not implement [SocketTarget],
// if the connection session is missing, or if the synchronous write/read fails.
func (t *SocketTransport) Do(ctx context.Context, req *Request) (*Response, error) {
	target, ok := req.Target().(SocketTarget)
	if !ok {
		return nil, fmt.Errorf("socket_transport: target %T does not support socket protocol", req.Target())
	}

	sess := t.caller.Session()
	if sess == nil {
		return nil, errors.New("socket is disconnected")
	}

	isAuth := sess.IsAuthenticated()

	var bodyBytes []byte
	if req.Body() != nil {
		var err error

		bodyBytes, err = io.ReadAll(req.Body())
		if err != nil {
			return nil, fmt.Errorf("socket_transport: failed to read request body: %w", err)
		}
	}

	var builder socket.PayloadBuilder
	if req.IsForceProto() {
		builder = socket.DynamicRawProto(target.EMsg(isAuth), bodyBytes, req.RoutingAppID())
	} else {
		builder = socket.DynamicRaw(target.EMsg(isAuth), target.ObjectName(), bodyBytes, req.RoutingAppID())
	}

	if req.Params().Get("__no_response") == "true" {
		err := t.caller.Send(ctx, builder, socket.WithToken(req.Token()))
		if err != nil {
			return nil, fmt.Errorf("socket_transport send failed: %w", err)
		}

		return NewResponse(io.NopCloser(bytes.NewReader(nil)), SocketMetadata{
			Result: enums.EResult_OK,
		}), nil
	}

	p, err := t.caller.SendSync(ctx, builder, socket.WithToken(req.Token()))
	if err != nil {
		return nil, fmt.Errorf("socket_transport call failed: %w", err)
	}

	result := enums.EResult_OK

	var sourceJobID uint64

	if p.Header != nil {
		if eh, ok := p.Header.(protocol.EHeader); ok {
			result = eh.GetEResult()
		}

		sourceJobID = p.GetSourceJobID()
	}

	return NewResponse(io.NopCloser(bytes.NewReader(p.Payload)), SocketMetadata{
		Result:      result,
		SourceJobID: sourceJobID,
		Header:      p.Header,
	}), nil
}

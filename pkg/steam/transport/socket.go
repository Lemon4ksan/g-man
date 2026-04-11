// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package transport

import (
	"context"
	"errors"
	"fmt"

	"github.com/lemon4ksan/g-man/pkg/steam/protocol"
	"github.com/lemon4ksan/g-man/pkg/steam/protocol/enums"
	"github.com/lemon4ksan/g-man/pkg/steam/socket"
)

// SocketMetadata holds context-specific information from a socket-based response.
type SocketMetadata struct {
	Result      enums.EResult   // EResult code from the message header.
	Header      protocol.Header // The full, parsed packet header.
	SourceJobID uint64          // The original Job ID that this message is a response to.
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
	SendSync(ctx context.Context, build socket.PayloadBuilder, opts ...socket.SendOption) (*protocol.Packet, error)
	Session() socket.Session
}

// SocketTransport implements the Transport interface for socket-based communication.
// It translates abstract Requests into concrete protocol.Packets.
type SocketTransport struct {
	caller SocketCaller
}

// NewSocketTransport creates a new socket transport layer.
func NewSocketTransport(caller SocketCaller) *SocketTransport {
	return &SocketTransport{
		caller: caller,
	}
}

// Do executes a Request over a persistent socket. It performs the following steps:
// 1. Asserts that the request's Target is a SocketTarget.
// 2. Determines the correct EMsg based on the current authentication state.
// 3. Uses the SocketCaller to send the message and wait for a response packet.
// 4. Extracts metadata (EResult, header) from the response packet.
// 5. Wraps the result in a generic Response container.
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

	p, err := t.caller.SendSync(ctx,
		socket.DynamicRaw(target.EMsg(isAuth), target.ObjectName(), req.Body()),
		socket.WithToken(req.Token()),
	)
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

	return NewResponse(p.Payload, SocketMetadata{
		Result:      result,
		SourceJobID: sourceJobID,
		Header:      p.Header,
	}), nil
}

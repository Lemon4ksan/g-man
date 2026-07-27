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

var (
	// ErrTargetNotSocket indicates target does not implement SocketTarget.
	ErrTargetNotSocket = errors.New("socket_transport: target does not support socket protocol")
	// ErrSocketDisconnected indicates socket transport call failed because session is missing or offline.
	ErrSocketDisconnected = errors.New("socket_transport: socket is disconnected")
)

type SocketMetadata struct {
	Result      enums.EResult
	SourceJobID uint64
}

type SocketTarget interface {
	Target
	EMsg(isAuth bool) enums.EMsg
	ObjectName() string
}

type SocketCaller interface {
	Send(ctx context.Context, build socket.PayloadBuilder, opts ...socket.SendOption) error
	SendSync(ctx context.Context, build socket.PayloadBuilder, opts ...socket.SendOption) (*protocol.Packet, error)
	Session() socket.Session
}

// SocketTransport executes transport requests over persistent socket connections.
type SocketTransport struct {
	caller SocketCaller
}

// NewSocketTransport constructs a SocketTransport instance.
func NewSocketTransport(caller SocketCaller) *SocketTransport {
	return &SocketTransport{
		caller: caller,
	}
}

func (t *SocketTransport) Do(ctx context.Context, req *Request) (*Response, error) {
	target, ok := req.Target().(SocketTarget)
	if !ok {
		return nil, fmt.Errorf("%w: %T", ErrTargetNotSocket, req.Target())
	}

	sess := t.caller.Session()
	if sess == nil {
		return nil, ErrSocketDisconnected
	}

	isAuth := sess.IsAuthenticated()

	bodyBytes, err := extractBodyBytes(req.Body)
	if err != nil {
		return nil, fmt.Errorf("socket_transport: failed to read request body: %w", err)
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

	resp, err := t.caller.SendSync(ctx, builder, socket.WithToken(req.Token()))
	if err != nil {
		return nil, fmt.Errorf("socket_transport call failed: %w", err)
	}

	defer protocol.ReleasePacket(resp)

	result := resp.GetEResult()
	if result == enums.EResult_Invalid {
		result = enums.EResult_OK
	}

	sourceJobID := resp.GetSourceJobID()

	return NewResponse(io.NopCloser(bytes.NewReader(resp.Payload)), SocketMetadata{
		Result:      result,
		SourceJobID: sourceJobID,
	}), nil
}

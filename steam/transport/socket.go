// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package transport

import (
	"context"
	"fmt"

	"github.com/lemon4ksan/g-man/jobs"
	"github.com/lemon4ksan/g-man/steam/protocol"
	"github.com/lemon4ksan/g-man/steam/socket"
)

type SocketTarget interface {
	Target
	EMsg(isAuth bool) protocol.EMsg
	ObjectName() string
}

// SocketCaller defines the minimal socket interface required by the transport.
// This decouples the transport from the concrete socket implementation.
type SocketCaller interface {
	CallUnifiedRaw(ctx context.Context, eMsg protocol.EMsg, targetName string, payload []byte, cb jobs.Callback[*protocol.Packet]) error
	Session() socket.Session
}

type SocketTransport struct {
	caller SocketCaller
}

var _ Transport = (*SocketTransport)(nil)

func NewSocketTransport(caller SocketCaller) *SocketTransport {
	return &SocketTransport{
		caller: caller,
	}
}

// callResult groups the output of the async socket call.
type callResult struct {
	resp *Response
	err  error
}

func (t *SocketTransport) Do(req *Request) (*Response, error) {
	target, ok := req.Target().(SocketTarget)
	if !ok {
		return nil, fmt.Errorf("socket_transport: target %T does not support socket protocol", req.Target())
	}

	isAuth := t.caller.Session().IsAuthenticated()

	// Buffer 1 ensures the callback won't block forever if the caller abandons the request
	resCh := make(chan callResult, 1)

	err := t.caller.CallUnifiedRaw(req.Context(), target.EMsg(isAuth), target.ObjectName(), req.Body(), func(p *protocol.Packet, err error) {
		if err != nil {
			resCh <- callResult{err: err}
			return
		}

		res := &Response{
			Body:   p.Payload,
			Result: protocol.EResult_OK,
		}

		if p.Header != nil {
			if eh, ok := p.Header.(protocol.EHeader); ok {
				res.Result = eh.GetEResult()
			}
			res.SourceJobID = p.GetSourceJobID()
		}

		resCh <- callResult{resp: res}
	})

	if err != nil {
		return nil, fmt.Errorf("socket_transport call failed: %w", err)
	}

	result := <-resCh
	if result.err != nil {
		return nil, result.err
	}

	return result.resp, nil
}

func (t *SocketTransport) Close() error { return nil }

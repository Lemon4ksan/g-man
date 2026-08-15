// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package socket

import (
	"context"

	"github.com/lemon4ksan/g-man/pkg/steam/protocol"
	"github.com/lemon4ksan/g-man/pkg/steam/protocol/enums"
)

// SteamSocket is the interface for the Steam socket unified API.
// 
// @aoni:socket
// @endpoint CMServer
// @packet *protocol.Packet
// @opcode enums.EMsg
// @job_id uint64
// @heartbeat interval="10s"
type SteamSocket interface {
	Connect(ctx context.Context, endpoint CMServer) error
	Disconnect() error
	Close() error
	IsConnected() bool

	RegisterMsgHandler(op enums.EMsg, handler func(p *protocol.Packet))
	RegisterServiceHandler(method string, handler func(p *protocol.Packet))

	Send(ctx context.Context, req []byte) error
}

// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:generate aoni-gen -file=api.go

package live

import (
	"context"

	pb "github.com/lemon4ksan/g-man/protobuf/steam"
	"github.com/lemon4ksan/g-man/pkg/steam/module"
	"github.com/lemon4ksan/g-man/pkg/steam/protocol/enums"
)

var (
	_ enums.EMsg
	_ module.InitContext
)

// Events defines typed RPC, Notify, and Event operations for Steam live trading.
//
// @aoni:service
// @protocol rpc
// @requester "module.InitContext"
type Events interface {
	// --- Inbound Events ---

	// @event enums.EMsg_EconTrading_InitiateTradeProposed
	OnInitiateTradeProposed(handler func(msg *pb.CMsgTrading_InitiateTradeRequest)) (unsubscribe func())

	// @event enums.EMsg_EconTrading_InitiateTradeResult
	OnInitiateTradeResult(handler func(msg *pb.CMsgTrading_InitiateTradeResponse)) (unsubscribe func())

	// @event enums.EMsg_EconTrading_StartSession
	OnStartSession(handler func(msg *pb.CMsgTrading_StartSession)) (unsubscribe func())

	// --- Outbound Notifications ---

	// @notify
	// @op enums.EMsg_EconTrading_InitiateTradeRequest
	SendTradeRequest(ctx context.Context, req *pb.CMsgTrading_InitiateTradeRequest) error

	// @notify
	// @op enums.EMsg_EconTrading_CancelTradeRequest
	CancelTradeRequest(ctx context.Context, req *pb.CMsgTrading_CancelTradeRequest) error

	// @notify
	// @op enums.EMsg_EconTrading_InitiateTradeResponse
	SendTradeResponse(ctx context.Context, req *pb.CMsgTrading_InitiateTradeResponse) error

	// Close unsubscribes all active event listeners.
	Close() error
}

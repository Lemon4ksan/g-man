// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:generate aoni-gen -file=api.go

package status

import (
	"context"

	pb "github.com/lemon4ksan/g-man/pkg/protobuf/steam"
	"github.com/lemon4ksan/g-man/pkg/steam/module"
	"github.com/lemon4ksan/g-man/pkg/steam/protocol/enums"
)

var (
	_ enums.EMsg
	_ module.InitContext
)

// Events defines typed RPC operations for status management.
//
// @aoni:service
// @protocol rpc
// @requester "module.InitContext"
type Events interface {
	// @notify
	// @op enums.EMsg_ClientGamesPlayedWithDataBlob
	SendGamesPlayed(ctx context.Context, req *pb.CMsgClientGamesPlayed) error

	// Close unsubscribes all active event listeners.
	Close() error
}

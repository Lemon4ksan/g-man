// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package apps

import (
	"context"
	"fmt"

	"github.com/lemon4ksan/g-man/steam/protocol"
	pb "github.com/lemon4ksan/g-man/steam/protocol/protobuf"
	"google.golang.org/protobuf/proto"
)

// GetPlayerCount requests the current number of players online for a specific AppID.
// Use appID 0 to get the total number of people connected to Steam.
func (a *Apps) GetPlayerCount(ctx context.Context, appID uint32) (int32, error) {
	req := &pb.CMsgDPGetNumberOfCurrentPlayers{
		Appid: proto.Uint32(appID),
	}

	// Because we are expecting a specific response (CMsgDPGetNumberOfCurrentPlayersResponse),
	// we use CallLegacy to handle the job tracking automatically.
	resp := &pb.CMsgDPGetNumberOfCurrentPlayersResponse{}

	err := a.client.Proto().CallLegacy(ctx, protocol.EMsg_ClientGetNumberOfCurrentPlayersDP, req, resp)
	if err != nil {
		return 0, fmt.Errorf("failed to get player count: %w", err)
	}

	eResult := protocol.EResult(resp.GetEresult())
	if eResult != protocol.EResult_OK {
		return 0, fmt.Errorf("steam returned error result: %s", eResult.String())
	}

	return resp.GetPlayerCount(), nil
}

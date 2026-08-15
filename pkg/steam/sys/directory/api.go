// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:generate aoni-gen -file=api.go

package directory

import (
	"context"

	"github.com/lemon4ksan/g-man/pkg/steam/service"
)

var _ = service.Doer(nil)

// API defines declarative RPC operations for discovering Steam Connection Managers.
//
// @aoni:service
// @base_url "https://api.steampowered.com/ISteamDirectory"
// @requester "service.Doer"
type API interface {
	// GetCMList fetches TCP and WebSocket server address strings.
	//
	// @get "GetCMList/v1/"
	// @unwrap response
	GetCMList(ctx context.Context, req *CMListRequest) (*CMListResponse, error)

	// GetCMListForConnect fetches detailed CMServer endpoints filtered by CMCfg parameters.
	//
	// @get "GetCMListForConnect/v1/"
	// @unwrap response
	GetCMListForConnect(ctx context.Context, req *CMListForConnectRequest) (*CMListForConnectResponse, error)

	// GetSteamPipeDomains fetches active domain names used by Steam content delivery networks.
	//
	// @get "GetSteamPipeDomains/v1/"
	// @unwrap response
	GetSteamPipeDomains(ctx context.Context) (*SteamPipeDomainsResponse, error)
}

// CMListRequest parameters for GetCMList.
//
// @aoni:dto
type CMListRequest struct {
	CellID   uint32 `url:"cellid"`
	MaxCount uint32 `url:"maxcount,omitempty"`
}

type CMListResponse struct {
	ServerList           []string `json:"serverlist"`
	ServerListWebsockets []string `json:"serverlist_websockets"`
}

// CMListForConnectRequest parameters for GetCMListForConnect.
//
// @aoni:dto
type CMListForConnectRequest struct {
	CellID   uint32 `url:"cellid,omitempty"`
	MaxCount uint32 `url:"maxcount,omitempty"`
	CMType   string `url:"cmtype,omitempty"`
	Realm    string `url:"realm,omitempty"`
}

type CMListForConnectResponse struct {
	ServerList []CMServer `json:"serverlist"`
}

type SteamPipeDomainsResponse struct {
	DomainList []string `json:"domainlist"`
}

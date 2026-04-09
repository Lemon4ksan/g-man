// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package directory

import (
	"context"
	"slices"

	"github.com/lemon4ksan/g-man/pkg/steam/service"
	"github.com/lemon4ksan/g-man/pkg/steam/socket"
)

type CMServer struct {
	Endpoint       string
	LegacyEndpoint string
	Type           string // "tcp", "websocket", "netfilter"
	DC             string
	Realm          string // "steamglobal"
	Load           int
	WtdLoad        float64
}

type CMCfg struct {
	CellID   uint32
	MaxCount uint32
	CmType   string
	Realm    string
}

type DirectoryService struct {
	client service.Doer
}

func NewDirectoryService(client service.Doer) *DirectoryService {
	return &DirectoryService{
		client: client,
	}
}

// GetCMList returns the complete list of tcp and ws servers. MaxCount is optional.
func (d *DirectoryService) GetCMList(ctx context.Context, cellID, maxCount uint32) ([]string, []string, error) {
	req := struct {
		CellID   uint32 `url:"cellid"`
		MaxCount uint32 `url:"maxcount,omitempty"`
	}{cellID, maxCount}
	type respStruct struct {
		ServerList           []string `json:"serverlist"`
		ServerListWebsockets []string `json:"serverlist_websockets"`
	}
	resp, err := service.WebAPI[respStruct](ctx, d.client, "GET", "ISteamDirectory", "GetCMList", 1, req)
	if err != nil {
		return nil, nil, err
	}
	return resp.ServerList, resp.ServerListWebsockets, nil
}

// GetCMListForConnect returns the optimal servers for connecting to steam.
func (d *DirectoryService) GetCMListForConnect(ctx context.Context, cfg CMCfg) ([]CMServer, error) {
	req := struct {
		CellID   uint32 `url:"cellid,omitempty"`
		MaxCount uint32 `url:"maxcount,omitempty"`
		CmType   string `url:"cmtype,omitempty"`
		Realm    string `url:"realm,omitempty"`
	}{cfg.CellID, cfg.MaxCount, cfg.CmType, cfg.Realm}
	type respStruct struct {
		ServerList []CMServer `json:"serverlist"`
	}

	resp, err := service.WebAPI[respStruct](ctx, d.client, "GET", "ISteamDirectory", "GetCMListForConnect", 1, req)
	if err != nil {
		return nil, err
	}
	return resp.ServerList, err
}

// GetOptimalCMServer returns the CM server with the lowest load.
func (d *DirectoryService) GetOptimalCMServer(ctx context.Context) (socket.CMServer, error) {
	cmList, err := d.GetCMListForConnect(ctx, CMCfg{})
	if err != nil {
		return socket.CMServer{}, err
	}

	slices.SortFunc(cmList, func(a, b CMServer) int {
		if a.Load < b.Load {
			return -1
		}
		if a.Load > b.Load {
			return 1
		}
		return 0
	})

	cm := cmList[0]
	return socket.CMServer{
		Endpoint: cm.Endpoint,
		Type:     cm.Type,
		Load:     float64(cm.Load),
		Realm:    cm.Realm,
	}, nil
}

func (d *DirectoryService) GetSteamPipeDomains(ctx context.Context) ([]string, error) {
	type respStruct struct {
		DomainList []string `json:"domainlist"`
	}
	resp, err := service.WebAPI[respStruct](ctx, d.client, "GET", "ISteamDirectory", "GetSteamPipeDomains", 1, nil)
	if err != nil {
		return nil, err
	}
	return resp.DomainList, err
}

// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package directory

import (
	"context"
	"net/url"
	"slices"
	"strconv"

	"github.com/lemon4ksan/g-man/pkg/steam/api"
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
	client api.WebAPIRequester
}

func NewDirectoryService(client api.WebAPIRequester) *DirectoryService {
	return &DirectoryService{
		client: client,
	}
}

// GetCMList returns the complete list of tcp and ws servers. MaxCount is optional.
func (d *DirectoryService) GetCMList(ctx context.Context, cellID, maxCount uint32) ([]string, []string, error) {
	params := url.Values{}
	params.Add("cellid", strconv.FormatUint(uint64(cellID), 10))
	if maxCount > 0 {
		params.Add("maxcount", strconv.FormatUint(uint64(maxCount), 10))
	}
	var resp struct {
		Response struct {
			ServerList           []string `json:"serverlist"`
			ServerListWebsockets []string `json:"serverlist_websockets"`
		} `json:"response"`
	}
	err := d.client.CallWebAPI(ctx, "GET", "ISteamDirectory", "GetCMList", 1, &resp, api.WithParams(params))
	return resp.Response.ServerList, resp.Response.ServerListWebsockets, err
}

// GetCMListForConnect returns the optimal servers for connecting to steam.
func (d *DirectoryService) GetCMListForConnect(ctx context.Context, cfg CMCfg) ([]CMServer, error) {
	params := url.Values{}
	if cfg.CellID != 0 {
		params.Add("cellid", strconv.FormatUint(uint64(cfg.CellID), 10))
	}
	if cfg.MaxCount > 0 {
		params.Add("maxcount", strconv.FormatUint(uint64(cfg.MaxCount), 10))
	}
	if cfg.CmType != "" {
		params.Add("cmtype", cfg.CmType)
	}
	if cfg.Realm != "" {
		params.Add("realm", cfg.Realm)
	}
	var resp struct {
		Response struct {
			ServerList []CMServer `json:"serverlist"`
		} `json:"response"`
	}
	err := d.client.CallWebAPI(ctx, "GET", "ISteamDirectory", "GetCMListForConnect", 1, &resp, api.WithParams(params))
	return resp.Response.ServerList, err
}

// GetOptimalCMServer returns the CM server with the lowest load.
func (d *DirectoryService) GetOptimalCMServer(ctx context.Context) (CMServer, error) {
	cmList, err := d.GetCMListForConnect(ctx, CMCfg{})
	if err != nil {
		return CMServer{}, err
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

	return cmList[0], nil
}

func (d *DirectoryService) GetSteamPipeDomains(ctx context.Context) ([]string, error) {
	var resp struct {
		Response struct {
			DomainList []string `json:"domainlist"`
		} `json:"response"`
	}
	err := d.client.CallWebAPI(ctx, "GET", "ISteamDirectory", "GetSteamPipeDomains", 1, &resp)
	return resp.Response.DomainList, err
}

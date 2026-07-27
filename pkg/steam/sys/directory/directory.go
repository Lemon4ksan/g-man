// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package directory queries the Steam Directory WebAPI to discover connection endpoints and content server routes.
package directory

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/lemon4ksan/g-man/pkg/steam/service"
	"github.com/lemon4ksan/g-man/pkg/steam/socket"
)

// ErrNoCMServers indicates Steam Directory returned an empty Connection Manager server list.
var ErrNoCMServers = errors.New("directory: no cm servers returned from steam")

// CMServer represents Connection Manager server endpoints and load metrics.
type CMServer struct {
	Endpoint       string  `json:"endpoint"`
	LegacyEndpoint string  `json:"legacy_endpoint"`
	Type           string  `json:"type"`
	DC             string  `json:"dc"`
	Realm          string  `json:"realm"`
	Load           int     `json:"load"`
	WtdLoad        float64 `json:"wtd_load"`
}

// CMCfg configures server selection parameters.
type CMCfg struct {
	CellID   uint32
	MaxCount uint32
	CmType   string
	Realm    string
}

// Service executes queries against the ISteamDirectory WebAPI.
type Service struct {
	client service.Doer
}

// New constructs a Service instance.
func New(client service.Doer) *Service {
	return &Service{
		client: client,
	}
}

// GetCMList fetches TCP and WebSocket server address strings.
func (d *Service) GetCMList(ctx context.Context, cellID, maxCount uint32) ([]string, []string, error) {
	req := struct {
		CellID   uint32 `url:"cellid"`
		MaxCount uint32 `url:"maxcount,omitempty"`
	}{cellID, maxCount}

	type respType struct {
		ServerList           []string `json:"serverlist"`
		ServerListWebsockets []string `json:"serverlist_websockets"`
	}

	resp, err := service.WebAPI[respType](ctx, d.client, "GET", "ISteamDirectory", "GetCMList", 1, req)
	if err != nil {
		return nil, nil, fmt.Errorf("directory: get cm list failed: %w", err)
	}

	return resp.ServerList, resp.ServerListWebsockets, nil
}

// GetCMListForConnect fetches detailed CMServer endpoints filtered by CMCfg parameters.
func (d *Service) GetCMListForConnect(ctx context.Context, cfg CMCfg) ([]CMServer, error) {
	req := struct {
		CellID   uint32 `url:"cellid,omitempty"`
		MaxCount uint32 `url:"maxcount,omitempty"`
		CmType   string `url:"cmtype,omitempty"`
		Realm    string `url:"realm,omitempty"`
	}{cfg.CellID, cfg.MaxCount, cfg.CmType, cfg.Realm}

	type respType struct {
		ServerList []CMServer `json:"serverlist"`
	}

	resp, err := service.WebAPI[respType](ctx, d.client, "GET", "ISteamDirectory", "GetCMListForConnect", 1, req)
	if err != nil {
		return nil, fmt.Errorf("directory: get cm list for connect failed: %w", err)
	}

	return resp.ServerList, nil
}

// GetOptimalCMServer discovers active CM servers and selects the endpoint reporting the lowest load metric.
func (d *Service) GetOptimalCMServer(ctx context.Context) (socket.CMServer, error) {
	cmList, err := d.GetCMListForConnect(ctx, CMCfg{})
	if err != nil {
		return socket.CMServer{}, err
	}

	if len(cmList) == 0 {
		return socket.CMServer{}, ErrNoCMServers
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

// GetSteamPipeDomains fetches active domain names used by Steam content delivery networks.
func (d *Service) GetSteamPipeDomains(ctx context.Context) ([]string, error) {
	type respType struct {
		DomainList []string `json:"domainlist"`
	}

	resp, err := service.WebAPI[respType](ctx, d.client, "GET", "ISteamDirectory", "GetSteamPipeDomains", 1, nil)
	if err != nil {
		return nil, fmt.Errorf("directory: get steampipe domains failed: %w", err)
	}

	return resp.DomainList, nil
}

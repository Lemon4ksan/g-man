// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package directory provides a client for the ISteamDirectory WebAPI,
// which is used to discover Steam Connection Manager (CM) servers.
package directory

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/lemon4ksan/g-man/pkg/steam/service"
	"github.com/lemon4ksan/g-man/pkg/steam/socket"
)

// CMServer represents a Steam Connection Manager server endpoint with load metrics.
type CMServer struct {
	// Endpoint is the primary address (Host:Port).
	Endpoint string
	// LegacyEndpoint is an alternative address format for older clients.
	LegacyEndpoint string
	// Type defines the transport protocol: "tcp", "websocket", or "netfilter".
	Type string
	// DC is the data center identifier.
	DC string
	// Realm is the Steam realm, usually "steamglobal".
	Realm string
	// Load is a metric representing the current server utilization.
	Load int
	// WtdLoad is the weighted load metric.
	WtdLoad float64
}

// CMCfg holds parameters for filtering the CM server list.
type CMCfg struct {
	// CellID is the geographical location ID of the client.
	CellID uint32
	// MaxCount limits the number of servers returned.
	MaxCount uint32
	// CmType filters by protocol ("tcp" or "websockets").
	CmType string
	// Realm filters by Steam realm.
	Realm string
}

// DirectoryService orchestrates requests to the ISteamDirectory interface.
type DirectoryService struct {
	client service.Doer
}

// NewDirectoryService initializes a new DirectoryService with the provided transport client.
func NewDirectoryService(client service.Doer) *DirectoryService {
	return &DirectoryService{
		client: client,
	}
}

// GetCMList returns the complete list of TCP and WebSocket servers as raw strings.
// CellID and MaxCount can be used for geographical optimization and limiting.
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
		return nil, nil, fmt.Errorf("directory: get cm list failed: %w", err)
	}

	return resp.ServerList, resp.ServerListWebsockets, nil
}

// GetCMListForConnect returns a detailed list of CM servers suitable for establishing a connection.
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
		return nil, fmt.Errorf("directory: get cm list for connect failed: %w", err)
	}

	return resp.ServerList, nil
}

// GetOptimalCMServer discovers available servers and returns the one with the lowest reported load.
// It returns an error if no servers are found.
func (d *DirectoryService) GetOptimalCMServer(ctx context.Context) (socket.CMServer, error) {
	cmList, err := d.GetCMListForConnect(ctx, CMCfg{})
	if err != nil {
		return socket.CMServer{}, err
	}

	if len(cmList) == 0 {
		return socket.CMServer{}, errors.New("directory: no cm servers returned from steam")
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

// GetSteamPipeDomains returns a list of domains used by Steam's content delivery system (SteamPipe).
func (d *DirectoryService) GetSteamPipeDomains(ctx context.Context) ([]string, error) {
	type respStruct struct {
		DomainList []string `json:"domainlist"`
	}

	resp, err := service.WebAPI[respStruct](ctx, d.client, "GET", "ISteamDirectory", "GetSteamPipeDomains", 1, nil)
	if err != nil {
		return nil, fmt.Errorf("directory: get steampipe domains failed: %w", err)
	}

	return resp.DomainList, nil
}

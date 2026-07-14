// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

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
// Instances of this struct are returned by [Service.GetCMListForConnect].
type CMServer struct {
	// Endpoint is the primary host and port address of the server.
	Endpoint string `json:"endpoint"`
	// LegacyEndpoint is an alternative port or address format for older client protocols.
	LegacyEndpoint string `json:"legacy_endpoint"`
	// Type defines the protocol transport type (such as "tcp" or "websockets").
	Type string `json:"type"`
	// DC is the data center identifier.
	DC string `json:"dc"`
	// Realm is the Steam server realm (such as "steamglobal").
	Realm string `json:"realm"`
	// Load is the server load metric reported by Steam.
	Load int `json:"load"`
	// WtdLoad is the weighted load metric calculated by Steam.
	WtdLoad float64 `json:"wtd_load"`
}

// CMCfg holds parameters for filtering the CM server list.
// Pass this configuration structure to [Service.GetCMListForConnect] to restrict endpoints.
type CMCfg struct {
	// CellID is the geographical location ID of the client.
	CellID uint32
	// MaxCount limits the maximum number of servers returned.
	MaxCount uint32
	// CmType filters by protocol transport type (such as "tcp" or "websockets").
	CmType string
	// Realm filters by Steam realm.
	Realm string
}

// Service orchestrates requests to the ISteamDirectory interface.
// It provides standard methods for retrieving Connection Manager endpoints.
// Create new instances of Service using the [New] constructor.
type Service struct {
	client service.Doer
}

// New initializes a new [Service] with the provided [service.Doer] client.
// It will panic if the provided client argument is nil.
func New(client service.Doer) *Service {
	return &Service{
		client: client,
	}
}

// GetCMList returns the complete list of TCP and WebSocket servers as raw strings.
// If cellID or maxCount are set to 0, they are omitted from the WebAPI query parameters.
// It returns an error if the underlying network transport fails or if the context is cancelled.
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

// GetCMListForConnect returns a detailed list of [CMServer] endpoints suitable for establishing a connection.
// If the [CMCfg] structure is empty, it returns a generic unfiltered list of active servers.
// It returns an error if the underlying network transport fails or if the context is cancelled.
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

// GetOptimalCMServer discovers available servers and returns the one with the lowest reported load.
// It returns a [socket.CMServer] endpoint populated with connection metrics.
// It returns an error if the WebAPI call fails, if the context is cancelled, or if Steam returns an empty list.
func (d *Service) GetOptimalCMServer(ctx context.Context) (socket.CMServer, error) {
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
// It returns an error if the underlying network transport fails or if the context is cancelled.
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

// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package directory queries the Steam Directory WebAPI to discover connection endpoints and content server routes.
package directory

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"slices"
	"time"

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
	api API
}

// New constructs a Service instance backed by a service.Doer.
func New(client service.Doer) *Service {
	return &Service{
		api: MustNewAPI(client),
	}
}

// GetCMList fetches TCP and WebSocket server address strings.
func (d *Service) GetCMList(ctx context.Context, cellID, maxCount uint32) ([]string, []string, error) {
	resp, err := d.api.GetCMList(ctx, &CMListRequest{
		CellID:   cellID,
		MaxCount: maxCount,
	})
	if err != nil {
		return nil, nil, fmt.Errorf("directory: get cm list failed: %w", err)
	}

	if resp == nil {
		return nil, nil, nil
	}

	return resp.ServerList, resp.ServerListWebsockets, nil
}

// GetCMListForConnect fetches detailed CMServer endpoints filtered by CMCfg parameters.
func (d *Service) GetCMListForConnect(ctx context.Context, cfg CMCfg) ([]CMServer, error) {
	resp, err := d.api.GetCMListForConnect(ctx, &CMListForConnectRequest{
		CellID:   cfg.CellID,
		MaxCount: cfg.MaxCount,
		CMType:   cfg.CmType,
		Realm:    cfg.Realm,
	})
	if err != nil {
		return nil, fmt.Errorf("directory: get cm list for connect failed: %w", err)
	}

	if resp == nil {
		return nil, nil
	}

	return resp.ServerList, nil
}

// GetOptimalCMServer discovers active CM servers and selects the endpoint reporting the lowest load metric.
func (d *Service) GetOptimalCMServer(ctx context.Context) (socket.CMServer, error) {
	var cmList []CMServer
	var err error

	for i := 0; i < 5; i++ {
		cmList, err = d.GetCMListForConnect(ctx, CMCfg{
			Realm: "steamglobal",
		})
		if err == nil && len(cmList) > 0 {
			break
		}

		select {
		case <-ctx.Done():
			return socket.CMServer{}, ctx.Err()
		case <-time.After(time.Second):
		}
	}

	if err != nil {
		return socket.CMServer{}, err
	}

	if len(cmList) == 0 {
		return socket.CMServer{}, ErrNoCMServers
	}

	var filtered []CMServer
	for _, cm := range cmList {
		if cm.Realm == "steamglobal" {
			filtered = append(filtered, cm)
		}
	}
	if len(filtered) > 0 {
		cmList = filtered
	}

	slices.SortFunc(cmList, func(a, b CMServer) int {
		if a.WtdLoad < b.WtdLoad {
			return -1
		}
		if a.WtdLoad > b.WtdLoad {
			return 1
		}
		return 0
	})

	maxIdx := len(cmList)
	if maxIdx > 5 {
		maxIdx = 5
	}

	cm := cmList[rand.IntN(maxIdx)]

	return socket.CMServer{
		Endpoint: cm.Endpoint,
		Type:     cm.Type,
		Load:     float64(cm.Load),
		Realm:    cm.Realm,
	}, nil
}

// GetSteamPipeDomains fetches active domain names used by Steam content delivery networks.
func (d *Service) GetSteamPipeDomains(ctx context.Context) ([]string, error) {
	resp, err := d.api.GetSteamPipeDomains(ctx)
	if err != nil {
		return nil, fmt.Errorf("directory: get steampipe domains failed: %w", err)
	}

	if resp == nil {
		return nil, nil
	}

	return resp.DomainList, nil
}

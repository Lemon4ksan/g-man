// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package directory_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lemon4ksan/g-man/pkg/steam/sys/directory"
	"github.com/lemon4ksan/g-man/test/mock"
)

func setup(t *testing.T) (*directory.Service, *mock.ServiceMock) {
	mock := mock.NewServiceMock()
	svc := directory.New(mock)
	return svc, mock
}

func TestGetCMList(t *testing.T) {
	svc, mock := setup(t)
	ctx := context.Background()

	t.Run("Success", func(t *testing.T) {
		mock.SetJSONResponse("ISteamDirectory", "GetCMList", map[string]any{
			"response": map[string]any{
				"serverlist":            []string{"1.2.3.4:27015", "1.2.3.5:27015"},
				"serverlist_websockets": []string{"wss://cm.steam.com"},
			},
		})

		tcp, ws, err := svc.GetCMList(ctx, 0, 5)
		require.NoError(t, err)
		assert.Len(t, tcp, 2)
		assert.Len(t, ws, 1)
		assert.Equal(t, "1.2.3.4:27015", tcp[0])
	})

	t.Run("API Error", func(t *testing.T) {
		mock.ResponseErrs["ISteamDirectory/GetCMList"] = errors.New("network fail")

		tcp, ws, err := svc.GetCMList(ctx, 0, 0)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "directory: get cm list failed")
		assert.Nil(t, tcp)
		assert.Nil(t, ws)
	})
}

func TestGetCMListForConnect(t *testing.T) {
	svc, mock := setup(t)
	ctx := context.Background()

	t.Run("Success", func(t *testing.T) {
		mock.SetJSONResponse("ISteamDirectory", "GetCMListForConnect", map[string]any{
			"response": map[string]any{
				"serverlist": []directory.CMServer{
					{Endpoint: "1.1.1.1:27015", Load: 50, Type: "tcp"},
				},
			},
		})

		list, err := svc.GetCMListForConnect(ctx, directory.CMCfg{CmType: "tcp"})
		require.NoError(t, err)
		assert.Len(t, list, 1)
		assert.Equal(t, "1.1.1.1:27015", list[0].Endpoint)
	})

	t.Run("API Error", func(t *testing.T) {
		mock.ResponseErrs["ISteamDirectory/GetCMListForConnect"] = errors.New("timeout")

		list, err := svc.GetCMListForConnect(ctx, directory.CMCfg{})
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "directory: get cm list for connect failed")
		assert.Nil(t, list)
	})
}

func TestGetOptimalCMServer(t *testing.T) {
	svc, mock := setup(t)
	ctx := context.Background()

	t.Run("Success Sorting", func(t *testing.T) {
		mock.SetJSONResponse("ISteamDirectory", "GetCMListForConnect", map[string]any{
			"response": map[string]any{
				"serverlist": []directory.CMServer{
					{Endpoint: "heavy:27015", Load: 100, Type: "tcp", Realm: "steamglobal"},
					{Endpoint: "optimal:27015", Load: 10, Type: "tcp", Realm: "steamglobal"},
					{Endpoint: "medium:27015", Load: 50, Type: "tcp", Realm: "steamglobal"},
				},
			},
		})

		cm, err := svc.GetOptimalCMServer(ctx)
		require.NoError(t, err)
		assert.Equal(t, "optimal:27015", cm.Endpoint)
		assert.Equal(t, float64(10), cm.Load)
	})

	t.Run("Empty List Error", func(t *testing.T) {
		mock.SetJSONResponse("ISteamDirectory", "GetCMListForConnect", map[string]any{
			"response": map[string]any{
				"serverlist": []directory.CMServer{},
			},
		})

		_, err := svc.GetOptimalCMServer(ctx)
		assert.Error(t, err)
		assert.Equal(t, "directory: no cm servers returned from steam", err.Error())
	})

	t.Run("Underlying Error", func(t *testing.T) {
		mock.ResponseErrs["ISteamDirectory/GetCMListForConnect"] = errors.New("api down")

		_, err := svc.GetOptimalCMServer(ctx)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "api down")
	})
}

func TestGetSteamPipeDomains(t *testing.T) {
	svc, mock := setup(t)
	ctx := context.Background()

	t.Run("Success", func(t *testing.T) {
		mock.SetJSONResponse("ISteamDirectory", "GetSteamPipeDomains", map[string]any{
			"response": map[string]any{
				"domainlist": []string{"steamcontent.com", "steampipe.akamaized.net"},
			},
		})

		domains, err := svc.GetSteamPipeDomains(ctx)
		require.NoError(t, err)
		assert.Len(t, domains, 2)
		assert.Contains(t, domains, "steamcontent.com")
	})

	t.Run("API Error", func(t *testing.T) {
		mock.ResponseErrs["ISteamDirectory/GetSteamPipeDomains"] = errors.New("fail")

		domains, err := svc.GetSteamPipeDomains(ctx)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "directory: get steampipe domains failed")
		assert.Nil(t, domains)
	})
}

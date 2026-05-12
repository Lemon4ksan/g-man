// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package bptf

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lemon4ksan/g-man/pkg/steam/id"
)

func TestClient_GetInventoryStatus(t *testing.T) {
	steamID := id.New(76561198033830321)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/inventory/76561198033830321/status", r.URL.Path)
		assert.Equal(t, http.MethodGet, r.Method)

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)

		resp := InventoryStatus{
			CurrentTime: 1600000000,
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := New(nil, "api-key", "user-token")
	client.restClient = client.restClient.WithBaseURL(server.URL)

	status, err := client.GetInventoryStatus(context.Background(), steamID)

	require.NoError(t, err)
	assert.Equal(t, int64(1600000000), status.CurrentTime)
}

func TestClient_GetUsersInfo(t *testing.T) {
	steamIDs := []id.ID{id.New(123), id.New(456)}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/users/info/v1", r.URL.Path)
		assert.Equal(t, "123,456", r.URL.Query().Get("steamids"))
		assert.Equal(t, "api-key", r.Header.Get("X-Api-Key"))

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)

		resp := V1UserResponse{}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := New(nil, "api-key", "")
	client.restClient = client.restClient.WithBaseURL(server.URL)

	_, err := client.GetUsersInfo(context.Background(), steamIDs)

	require.NoError(t, err)
}

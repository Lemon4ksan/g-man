// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package webapi

import (
	"net/url"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lemon4ksan/aoni"
)

func TestWebAPI_Service_Initialization(t *testing.T) {
	t.Parallel()

	client := aoni.NewClient(nil)
	service, err := NewICSGOPlayers_730(client)
	require.NoError(t, err)
	assert.NotNil(t, service)

	mustService := MustNewICSGOPlayers_730(client)
	assert.NotNil(t, mustService)
}

func TestWebAPI_NilClient_ReturnsError(t *testing.T) {
	t.Parallel()

	_, err := NewICSGOPlayers_730(nil)
	assert.Error(t, err)

	assert.Panics(t, func() {
		MustNewICSGOPlayers_730(nil)
	})
}

func TestWebAPI_DTO_AppendQueryAndFormData(t *testing.T) {
	t.Parallel()

	req := &ICSGOPlayers_730_GetNextMatchSharingCode_Request{
		SteamID:    76561198000000000,
		SteamIDKey: "secret_key",
		Knowncode:  "CSGO-XXXXX",
	}

	buf := req.AppendQuery(nil)
	queryStr := string(buf)
	assert.Contains(t, queryStr, "steamid=76561198000000000")
	assert.Contains(t, queryStr, "steamidkey=secret_key")
	assert.Contains(t, queryStr, "knowncode=CSGO-XXXXX")

	vals := make(url.Values)
	req.EncodeValues(vals)
	assert.Equal(t, "76561198000000000", vals.Get("steamid"))
	assert.Equal(t, "secret_key", vals.Get("steamidkey"))
	assert.Equal(t, "CSGO-XXXXX", vals.Get("knowncode"))
}

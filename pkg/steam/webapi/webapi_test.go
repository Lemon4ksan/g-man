// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package webapi

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/lemon4ksan/aoni"
)

func TestWebAPI_Service_Initialization(t *testing.T) {
	t.Parallel()

	client := aoni.NewClient(nil)
	service := NewCSGOPlayers730(client)
	assert.NotNil(t, service)

	defaultService := NewCSGOPlayers730(nil)
	assert.NotNil(t, defaultService)
}

func TestWebAPI_DTO_Request(t *testing.T) {
	t.Parallel()

	req := &UploadTournamentFantasyLineupRequest{
		Event:      1,
		SteamID:    76561198000000000,
		SteamIDKey: "test_key",
		Sectionid:  2,
	}

	buf := req.AppendQuery(nil)
	assert.NotEmpty(t, buf)
}

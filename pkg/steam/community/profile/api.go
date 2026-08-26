// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package profile

import (
	"context"

	"github.com/lemon4ksan/aoni"

	"github.com/lemon4ksan/g-man/pkg/steam/community"
)

var _ = community.BaseURL

// API is an interface for the Steam profile API.
//
// @aoni:service casing=snake_case
// @engine custom type="community.Requester" required
// @base_url "https://steamcommunity.com"
type API interface {
	// @get "profiles/{steamID}/edit/info"
	// @return body | attr(css="#profile_edit_config", name="data-profile-edit") | html_unescape | json
	GetEditConfig(ctx context.Context, steamID uint64, mods ...aoni.RequestModifier) (*rawProfileEditConfig, error)

	// @get "profiles/{steamID}/edit/settings"
	// @return body | attr(css="#profile_edit_config", name="data-profile-edit") | html_unescape | json
	GetPrivacyConfig(ctx context.Context, steamID uint64, mods ...aoni.RequestModifier) (*rawPrivacyConfig, error)

	// @post "profiles/{steamID}/edit"
	// @form
	SaveProfile(
		ctx context.Context,
		steamID uint64,
		req *profileSaveRequest,
		mods ...aoni.RequestModifier,
	) (*saveResponse, error)

	// @post "profiles/{steamID}/ajaxsetprivacy"
	// @form casing=flatcase
	SavePrivacy(
		ctx context.Context,
		steamID uint64,
		sessionID string,
		privacy rawPrivacySettings, // @field "Privacy" = json | url_escape
		commentPermission int, // @field "eCommentPermission"
		mods ...aoni.RequestModifier,
	) (*privacyResponse, error)

	// @post "actions/FileUploader"
	// @multipart
	UploadAvatarFile(
		ctx context.Context,
		uploadType string, // @part "type"
		steamID string, // @part "sId"
		sessionID string, // @part "sessionid"
		doSub string, // @part "doSub"
		jsonFlag string, // @part "json"
		image []byte, // @file name="avatar" filename="{filename}" content_type="{contentType}"
		filename string,
		contentType string,
		mods ...aoni.RequestModifier,
	) (*uploadResponse, error)
}

type saveResponse struct {
	Success int    `json:"success"`
	ErrMsg  string `json:"errmsg"`
}

type privacyResponse struct {
	Success int `json:"success"`
}

type uploadResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
	Hash    string `json:"hash"`
}

// @aoni:dto
type profileSaveRequest struct {
	Type          string `query:"type"`
	Weblink1Title string `query:"weblink_1_title"`
	Weblink1URL   string `query:"weblink_1_url"`
	Weblink2Title string `query:"weblink_2_title"`
	Weblink2URL   string `query:"weblink_2_url"`
	Weblink3Title string `query:"weblink_3_title"`
	Weblink3URL   string `query:"weblink_3_url"`
	PersonaName   string `query:"personaName"`
	RealName      string `query:"real_name"`
	Summary       string `query:"summary"`
	Country       string `query:"country"`
	State         string `query:"state"`
	City          string `query:"city"`
	CustomURL     string `query:"customURL"`
	JSON          int    `query:"json"`
}

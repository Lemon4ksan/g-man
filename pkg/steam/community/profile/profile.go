// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package profile

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"strconv"
	"strings"

	"github.com/PuerkitoBio/goquery"
	"github.com/lemon4ksan/aoni"

	"github.com/lemon4ksan/g-man/pkg/steam/community"
	"github.com/lemon4ksan/g-man/pkg/steam/id"
)

// WithAvatarUpload assembles a Steam-specific multipart avatar upload form.
// It ensures that the field name is "avatar" and the filename has the correct extension.
func WithAvatarUpload(fields map[string]string, filename string, image []byte) aoni.RequestModifier {
	return func(req *http.Request) {
		body := &bytes.Buffer{}
		writer := multipart.NewWriter(body)

		for name, val := range fields {
			if err := writer.WriteField(name, val); err != nil {
				return
			}
		}

		part, err := writer.CreateFormFile("avatar", filename)
		if err != nil {
			return
		}

		if _, err := part.Write(image); err != nil {
			return
		}

		if err := writer.Close(); err != nil {
			return
		}

		req.Body = io.NopCloser(body)
		req.ContentLength = int64(body.Len())
		req.Header.Set("Content-Type", writer.FormDataContentType())
	}
}

// Settings represents customizable profile details.
//
// Struct fields are pointers. Any field set to nil is ignored during update,
// keeping the existing profile values in Steam.
type Settings struct {
	// Name is the custom display nickname of the user.
	Name *string
	// RealName is the user's real name.
	RealName *string
	// Summary is the user's custom bio description.
	Summary *string
	// Country is the ISO two-letter country code of the location.
	Country *string
	// State is the state code of the location.
	State *string
	// City is the city code of the location.
	City *string
	// CustomURL is the custom vanity URL slug.
	CustomURL *string
}

// PrivacyState represents profile privacy level.
type PrivacyState int

// PrivacyState constants define the profile privacy level.
const (
	PrivacyPrivate     PrivacyState = 1
	PrivacyFriendsOnly PrivacyState = 2
	PrivacyPublic      PrivacyState = 3
)

// String returns a human-readable representation of PrivacyState.
func (p PrivacyState) String() string {
	switch p {
	case PrivacyPrivate:
		return "Private"
	case PrivacyFriendsOnly:
		return "FriendsOnly"
	case PrivacyPublic:
		return "Public"
	default:
		return "Unknown"
	}
}

// CommentPermission represents who can post comments on the profile.
type CommentPermission int

// CommentPermission constants define who can post comments on the profile.
const (
	CommentFriendsOnly CommentPermission = 0
	CommentAnyone      CommentPermission = 1
	CommentPrivate     CommentPermission = 2
)

// PrivacySettings represents customizable profile privacy details.
//
// Struct fields are pointers. Any field set to nil is ignored during update,
// keeping the existing privacy values in Steam.
type PrivacySettings struct {
	// Profile is the general privacy level of the profile.
	Profile *PrivacyState
	// Comments is the commentary permission level of the profile.
	Comments *CommentPermission
	// Inventory is the privacy level of the inventory.
	Inventory *PrivacyState
	// InventoryGifts specifies if inventory gifts are kept private (true) or public (false).
	InventoryGifts *bool
	// GameDetails is the privacy level of active game playtimes and details.
	GameDetails *PrivacyState
	// Playtime specifies if total gameplay playtime is kept private (true) or public (false).
	Playtime *bool
	// FriendsList is the privacy level of the friends list.
	FriendsList *PrivacyState
}

// EditProfile retrieves the existing profile configuration, merges the changes, and saves the updated settings.
func EditProfile(ctx context.Context, client community.Requester, steamID id.ID, settings Settings) error {
	html, err := community.GetHTML(
		ctx, client, "profiles/{steamID}/edit/info",
		aoni.WithVar("steamID", steamID),
	)
	if err != nil {
		return fmt.Errorf("profile: failed to fetch edit page: %w", err)
	}
	defer html.Close()

	currentConfig, err := parseProfileEditConfig(html)
	if err != nil {
		return err
	}

	reqPayload := buildProfileSaveRequest(currentConfig, settings)

	type saveResponse struct {
		Success int    `json:"success"`
		ErrMsg  string `json:"errmsg"`
	}

	resp, err := community.PostForm[saveResponse](
		ctx, client, "profiles/{steamID}/edit", reqPayload,
		aoni.WithVar("steamID", steamID),
	)
	if err != nil {
		return fmt.Errorf("profile: failed to post profile save: %w", err)
	}

	if resp.Success != 1 {
		errMsg := resp.ErrMsg
		if errMsg == "" {
			errMsg = "request was not successful"
		}

		return fmt.Errorf("profile: save failed: %s", errMsg)
	}

	return nil
}

// UpdatePrivacySettings fetches the current privacy status, merges the changes, and posts updates.
func UpdatePrivacySettings(
	ctx context.Context,
	client community.Requester,
	steamID id.ID,
	settings PrivacySettings,
) error {
	html, err := community.GetHTML(
		ctx, client, "profiles/{steamID}/edit/settings",
		aoni.WithVar("steamID", steamID),
	)
	if err != nil {
		return fmt.Errorf("profile: failed to fetch settings page: %w", err)
	}
	defer html.Close()

	currentConfig, err := parsePrivacyConfig(html)
	if err != nil {
		return err
	}

	reqPayload, err := buildPrivacySaveRequest(currentConfig, settings)
	if err != nil {
		return err
	}

	type privacyResponse struct {
		Success int `json:"success"`
	}

	resp, err := community.PostForm[privacyResponse](
		ctx, client, "profiles/{steamID}/ajaxsetprivacy", reqPayload,
		aoni.WithVar("steamID", steamID),
	)
	if err != nil {
		return fmt.Errorf("profile: failed to post privacy settings: %w", err)
	}

	if resp.Success != 1 {
		return fmt.Errorf("profile: privacy save failed: success=%d", resp.Success)
	}

	return nil
}

// UploadAvatar uploads a new profile avatar image and returns its secure hash.
func UploadAvatar(
	ctx context.Context,
	client community.Requester,
	steamID id.ID,
	image []byte,
	contentType string,
) (string, error) {
	if len(image) == 0 {
		return "", errors.New("profile: empty avatar image buffer")
	}

	filename, err := resolveAvatarFilename(contentType)
	if err != nil {
		return "", err
	}

	fields := map[string]string{
		"MAX_FILE_SIZE": strconv.Itoa(len(image)),
		"type":          "player_avatar_image",
		"sId":           strconv.FormatUint(uint64(steamID), 10),
		"sessionid":     client.SessionID(community.BaseURL),
		"doSub":         "1",
		"json":          "1",
	}

	type upload struct {
		Success bool   `json:"success"`
		Message string `json:"message"`
		Hash    string `json:"hash"`
	}

	resp, err := aoni.PostJSON[upload](
		ctx, client, "actions/FileUploader", nil,
		WithAvatarUpload(fields, filename, image),
		aoni.WithHeader("Accept", "application/json, text/javascript; q=0.01"),
	)
	if err != nil {
		return "", fmt.Errorf("profile: upload request failed: %w", err)
	}

	if !resp.Success {
		errMsg := resp.Message
		if errMsg == "" {
			errMsg = "upload was not successful"
		}

		return "", fmt.Errorf("profile: upload failed: %s", errMsg)
	}

	return resp.Hash, nil
}

func parseProfileEditConfig(html io.Reader) (*rawProfileEditConfig, error) {
	doc, err := goquery.NewDocumentFromReader(html)
	if err != nil {
		return nil, fmt.Errorf("profile: failed to parse HTML: %w", err)
	}

	configEl := doc.Find("#profile_edit_config")
	if configEl.Length() == 0 {
		return nil, errors.New("profile: could not find profile_edit_config element")
	}

	dataVal, exists := configEl.Attr("data-profile-edit")
	if !exists {
		return nil, errors.New("profile: missing data-profile-edit attribute")
	}

	var config rawProfileEditConfig
	if err := json.Unmarshal([]byte(dataVal), &config); err != nil {
		return nil, fmt.Errorf("profile: failed to parse existing settings JSON: %w", err)
	}

	return &config, nil
}

type profileSaveRequest struct {
	Type          string `url:"type"`
	Weblink1Title string `url:"weblink_1_title"`
	Weblink1URL   string `url:"weblink_1_url"`
	Weblink2Title string `url:"weblink_2_title"`
	Weblink2URL   string `url:"weblink_2_url"`
	Weblink3Title string `url:"weblink_3_title"`
	Weblink3URL   string `url:"weblink_3_url"`
	PersonaName   string `url:"personaName"`
	RealName      string `url:"real_name"`
	Summary       string `url:"summary"`
	Country       string `url:"country"`
	State         string `url:"state"`
	City          string `url:"city"`
	CustomURL     string `url:"customURL"`
	JSON          int    `url:"json"`
}

func buildProfileSaveRequest(current *rawProfileEditConfig, settings Settings) profileSaveRequest {
	req := profileSaveRequest{
		Type:        "profileSave",
		PersonaName: current.PersonaName,
		RealName:    current.RealName,
		Summary:     current.Summary,
		Country:     current.LocationData.CountryCode,
		State:       current.LocationData.StateCode,
		City:        current.LocationData.CityCode,
		CustomURL:   current.CustomURL,
		JSON:        1,
	}

	if settings.Name != nil {
		req.PersonaName = *settings.Name
	}

	if settings.RealName != nil {
		req.RealName = *settings.RealName
	}

	if settings.Summary != nil {
		req.Summary = *settings.Summary
	}

	if settings.Country != nil {
		req.Country = *settings.Country
	}

	if settings.State != nil {
		req.State = *settings.State
	}

	if settings.City != nil {
		req.City = *settings.City
	}

	if settings.CustomURL != nil {
		req.CustomURL = *settings.CustomURL
	}

	return req
}

func parsePrivacyConfig(html io.Reader) (*rawPrivacyConfig, error) {
	doc, err := goquery.NewDocumentFromReader(html)
	if err != nil {
		return nil, fmt.Errorf("profile: failed to parse HTML: %w", err)
	}

	configEl := doc.Find("#profile_edit_config")
	if configEl.Length() == 0 {
		return nil, errors.New("profile: could not find profile_edit_config element")
	}

	dataVal, exists := configEl.Attr("data-profile-edit")
	if !exists {
		return nil, errors.New("profile: missing data-profile-edit attribute")
	}

	var config rawPrivacyConfig
	if err := json.Unmarshal([]byte(dataVal), &config); err != nil {
		return nil, fmt.Errorf("profile: failed to parse existing privacy settings JSON: %w", err)
	}

	return &config, nil
}

type privacySaveRequest struct {
	Privacy            string `url:"Privacy"`
	ECommentPermission int    `url:"eCommentPermission"`
}

func buildPrivacySaveRequest(current *rawPrivacyConfig, settings PrivacySettings) (privacySaveRequest, error) {
	commentMapping := map[CommentPermission]int{
		CommentFriendsOnly: 0,
		CommentAnyone:      1,
		CommentPrivate:     2,
	}

	privacy := current.Privacy.PrivacySettings
	comments := current.Privacy.ECommentPermission

	if settings.Profile != nil {
		privacy.PrivacyProfile = int(*settings.Profile)
	}

	if settings.Comments != nil {
		comments = commentMapping[*settings.Comments]
	}

	if settings.Inventory != nil {
		privacy.PrivacyInventory = int(*settings.Inventory)
	}

	if settings.InventoryGifts != nil {
		if *settings.InventoryGifts {
			privacy.PrivacyInventoryGifts = int(PrivacyPrivate)
		} else {
			privacy.PrivacyInventoryGifts = int(PrivacyPublic)
		}
	}

	if settings.GameDetails != nil {
		privacy.PrivacyOwnedGames = int(*settings.GameDetails)
	}

	if settings.Playtime != nil {
		if *settings.Playtime {
			privacy.PrivacyPlaytime = int(PrivacyPrivate)
		} else {
			privacy.PrivacyPlaytime = int(PrivacyPublic)
		}
	}

	if settings.FriendsList != nil {
		privacy.PrivacyFriendsList = int(*settings.FriendsList)
	}

	privacyJSON, err := json.Marshal(privacy)
	if err != nil {
		return privacySaveRequest{}, fmt.Errorf("profile: failed to marshal privacy settings: %w", err)
	}

	return privacySaveRequest{
		Privacy:            string(privacyJSON),
		ECommentPermission: comments,
	}, nil
}

func resolveAvatarFilename(contentType string) (string, error) {
	switch strings.ToLower(contentType) {
	case "image/jpeg", "image/jpg", "jpg", "jpeg":
		return "avatar.jpg", nil
	case "image/png", "png":
		return "avatar.png", nil
	case "image/gif", "gif":
		return "avatar.gif", nil
	default:
		return "", fmt.Errorf("profile: unsupported content-type: %s", contentType)
	}
}

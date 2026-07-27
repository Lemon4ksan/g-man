// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package profile manages profile bio details, avatars, and privacy configurations for Steam Community accounts.
package profile

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"strconv"
	"strings"

	"github.com/PuerkitoBio/goquery"
	json "github.com/goccy/go-json"
	"github.com/lemon4ksan/aoni"
	"github.com/lemon4ksan/aoni/mod"
	"github.com/lemon4ksan/aoni/request"
	"github.com/lemon4ksan/miyako/generic"

	"github.com/lemon4ksan/g-man/pkg/steam/community"
	"github.com/lemon4ksan/g-man/pkg/steam/id"
)

var (
	// ErrEmptyAvatarBuffer indicates avatar image buffer is empty.
	ErrEmptyAvatarBuffer = errors.New("profile: empty avatar image buffer")
	// ErrConfigNotFound indicates profile edit JSON element was missing from page HTML.
	ErrConfigNotFound = errors.New("profile: config element not found (possibly not logged in)")
	// ErrMissingDataAttr indicates data-profile-edit HTML attribute was missing.
	ErrMissingDataAttr = errors.New("profile: missing data-profile-edit attribute")
)

// WithAvatarUpload constructs a multipart avatar upload form.
func WithAvatarUpload(fields map[string]string, filename string, image []byte) aoni.RequestModifier {
	return func(req aoni.Request) {
		body := &bytes.Buffer{}
		writer := multipart.NewWriter(body)

		for name, val := range fields {
			if err := writer.WriteField(name, val); err != nil {
				aoni.MarkModifierError(req, fmt.Errorf("field %s: %w", name, err))
				return
			}
		}

		part, err := writer.CreateFormFile("avatar", filename)
		if err != nil {
			aoni.MarkModifierError(req, err)
			return
		}

		if _, err := part.Write(image); err != nil {
			aoni.MarkModifierError(req, err)
			return
		}

		if err := writer.Close(); err != nil {
			aoni.MarkModifierError(req, err)
			return
		}

		req.SetBodyBytes(body.Bytes())
		req.SetHeader("Content-Type", writer.FormDataContentType())
	}
}

// Settings represents customizable profile bio fields.
type Settings struct {
	Name      *string
	RealName  *string
	Summary   *string
	Country   *string
	State     *string
	City      *string
	CustomURL *string
}

// PrivacyState represents profile visibility permissions.
type PrivacyState int

const (
	PrivacyPrivate     PrivacyState = 1
	PrivacyFriendsOnly PrivacyState = 2
	PrivacyPublic      PrivacyState = 3
)

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

// CommentPermission represents commentary permissions.
type CommentPermission int

const (
	CommentFriendsOnly CommentPermission = 0
	CommentAnyone      CommentPermission = 1
	CommentPrivate     CommentPermission = 2
)

// PrivacySettings represents customizable profile privacy settings.
type PrivacySettings struct {
	Profile        *PrivacyState
	Comments       *CommentPermission
	Inventory      *PrivacyState
	InventoryGifts *bool
	GameDetails    *PrivacyState
	Playtime       *bool
	FriendsList    *PrivacyState
}

// EditProfile updates profile display details.
func EditProfile(ctx context.Context, client community.Requester, steamID id.ID, settings Settings) error {
	html, err := community.GetHTML(
		ctx, client, "profiles/{steamID}/edit/info",
		mod.WithVar("steamID", steamID),
	)
	if err != nil {
		return fmt.Errorf("profile: failed to fetch edit page: %w", err)
	}

	defer html.Close()

	currentConfig, err := parseSteamConfig[rawProfileEditConfig](html)
	if err != nil {
		return err
	}

	reqPayload := buildProfileSaveRequest(currentConfig, settings)

	type saveResponse struct {
		Success int    `json:"success"`
		ErrMsg  string `json:"errmsg"`
	}

	resp, err := community.PostFormTo[saveResponse](
		ctx, client, "profiles/{steamID}/edit", reqPayload,
		mod.WithVar("steamID", steamID),
	)
	if err != nil {
		return fmt.Errorf("profile: failed to post profile save: %w", err)
	}

	if resp.Success != 1 {
		return fmt.Errorf("profile: save failed: %s", generic.Coalesce(resp.ErrMsg, "request was not successful"))
	}

	return nil
}

// UpdatePrivacySettings modifies profile privacy options.
func UpdatePrivacySettings(
	ctx context.Context,
	client community.Requester,
	steamID id.ID,
	settings PrivacySettings,
) error {
	html, err := community.GetHTML(
		ctx, client, "profiles/{steamID}/edit/settings",
		mod.WithVar("steamID", steamID),
	)
	if err != nil {
		return fmt.Errorf("profile: failed to fetch settings page: %w", err)
	}

	defer html.Close()

	currentConfig, err := parseSteamConfig[rawPrivacyConfig](html)
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

	resp, err := community.PostFormTo[privacyResponse](
		ctx, client, "profiles/{steamID}/ajaxsetprivacy", reqPayload,
		mod.WithVar("steamID", steamID),
	)
	if err != nil {
		return fmt.Errorf("profile: failed to post privacy settings: %w", err)
	}

	if resp.Success != 1 {
		return fmt.Errorf("profile: privacy save failed: success=%d", resp.Success)
	}

	return nil
}

// UploadAvatar uploads an image buffer and sets it as the profile avatar.
func UploadAvatar(
	ctx context.Context,
	client community.Requester,
	steamID id.ID,
	image []byte,
	contentType string,
) (string, error) {
	if len(image) == 0 {
		return "", ErrEmptyAvatarBuffer
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

	resp, err := request.PostTo[upload](
		ctx, client, "actions/FileUploader", nil,
		WithAvatarUpload(fields, filename, image),
		mod.WithHeader("Accept", "application/json, text/javascript; q=0.01"),
	)
	if err != nil {
		return "", fmt.Errorf("profile: upload request failed: %w", err)
	}

	if !resp.Success {
		return "", fmt.Errorf("profile: upload failed: %s", generic.Coalesce(resp.Message, "upload was not successful"))
	}

	return resp.Hash, nil
}

func parseSteamConfig[T any](html io.Reader) (*T, error) {
	doc, err := goquery.NewDocumentFromReader(html)
	if err != nil {
		return nil, fmt.Errorf("profile: failed to parse HTML: %w", err)
	}

	configEl := doc.Find("#profile_edit_config")
	if configEl.Length() == 0 {
		return nil, ErrConfigNotFound
	}

	dataVal, exists := configEl.Attr("data-profile-edit")
	if !exists {
		return nil, ErrMissingDataAttr
	}

	var config T
	if err := json.Unmarshal([]byte(dataVal), &config); err != nil {
		return nil, fmt.Errorf("profile: failed to unmarshal config: %w", err)
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

	if settings.Profile != nil {
		privacy.PrivacyProfile = int(*settings.Profile)
	}

	if settings.Comments != nil {
		current.Privacy.ECommentPermission = commentMapping[*settings.Comments]
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
		ECommentPermission: current.Privacy.ECommentPermission,
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

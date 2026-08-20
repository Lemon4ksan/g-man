// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package profile manages profile bio details, avatars, and privacy configurations for Steam Community accounts.
package profile

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/lemon4ksan/aoni/codec/extract"
	"github.com/lemon4ksan/foundation/generic"

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
	api := MustNewAPI(client)

	currentConfig, err := api.GetEditConfig(ctx, uint64(steamID))
	if err != nil {
		if errors.Is(err, extract.ErrElementNotFound) || strings.Contains(err.Error(), "element not found") {
			return ErrConfigNotFound
		}

		if errors.Is(err, extract.ErrAttrNotFound) || strings.Contains(err.Error(), "attribute not found") {
			return ErrMissingDataAttr
		}

		if strings.Contains(err.Error(), "json unmarshal") || strings.Contains(err.Error(), "failed to unmarshal") {
			return fmt.Errorf("profile: failed to unmarshal config: %w", err)
		}

		if strings.Contains(err.Error(), "read error") {
			return fmt.Errorf("profile: failed to parse HTML: %w", err)
		}

		return fmt.Errorf("profile: failed to fetch edit page: %w", err)
	}

	reqPayload := buildProfileSaveRequest(currentConfig, settings)

	resp, err := api.SaveProfile(ctx, uint64(steamID), &reqPayload)
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
	api := MustNewAPI(client)

	currentConfig, err := api.GetPrivacyConfig(ctx, uint64(steamID))
	if err != nil {
		if errors.Is(err, extract.ErrElementNotFound) || strings.Contains(err.Error(), "element not found") {
			return ErrConfigNotFound
		}

		if errors.Is(err, extract.ErrAttrNotFound) || strings.Contains(err.Error(), "attribute not found") {
			return ErrMissingDataAttr
		}

		if strings.Contains(err.Error(), "json unmarshal") || strings.Contains(err.Error(), "failed to unmarshal") {
			return fmt.Errorf("profile: failed to unmarshal config: %w", err)
		}

		if strings.Contains(err.Error(), "read error") {
			return fmt.Errorf("profile: failed to parse HTML: %w", err)
		}

		return fmt.Errorf("profile: failed to fetch settings page: %w", err)
	}

	privacy, commentPermission := buildPrivacySettings(currentConfig, settings)

	sessionID := client.SessionID(community.BaseURL)

	resp, err := api.SavePrivacy(ctx, uint64(steamID), sessionID, privacy, commentPermission)
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

	api := MustNewAPI(client)

	resp, err := api.UploadAvatarFile(
		ctx,
		"player_avatar_image",
		strconv.FormatUint(uint64(steamID), 10),
		client.SessionID(community.BaseURL),
		"1",
		"1",
		image,
		filename,
		contentType,
	)
	if err != nil {
		return "", fmt.Errorf("profile: upload request failed: %w", err)
	}

	if !resp.Success {
		return "", fmt.Errorf("profile: upload failed: %s", generic.Coalesce(resp.Message, "upload was not successful"))
	}

	return resp.Hash, nil
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

func buildPrivacySettings(current *rawPrivacyConfig, settings PrivacySettings) (rawPrivacySettings, int) {
	commentMapping := map[CommentPermission]int{
		CommentFriendsOnly: 0,
		CommentAnyone:      1,
		CommentPrivate:     2,
	}

	privacy := current.Privacy.PrivacySettings

	if settings.Profile != nil {
		privacy.PrivacyProfile = int(*settings.Profile)
	}

	commentPermission := current.Privacy.ECommentPermission
	if settings.Comments != nil {
		commentPermission = commentMapping[*settings.Comments]
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

	return privacy, commentPermission
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

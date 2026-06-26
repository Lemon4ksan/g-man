// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package profile

import (
	"context"
	"errors"
	"io"
	"net/http"
	"testing"

	"github.com/lemon4ksan/aoni"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lemon4ksan/g-man/pkg/steam/id"
	"github.com/lemon4ksan/g-man/test/mock"
)

const testSteamID = id.ID(76561197960265728)

type errorReader struct{}

func (errorReader) Read(p []byte) (n int, err error) {
	return 0, errors.New("read error")
}

type parseErrorMock struct {
	*mock.HTTPStub
}

func (m *parseErrorMock) Request(
	ctx context.Context,
	method, path string,
	mods ...aoni.RequestModifier,
) (*http.Response, error) {
	return &http.Response{
		StatusCode: 200,
		Body:       io.NopCloser(&errorReader{}),
	}, nil
}

func TestPrivacyState_String(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "Private", PrivacyPrivate.String())
	assert.Equal(t, "FriendsOnly", PrivacyFriendsOnly.String())
	assert.Equal(t, "Public", PrivacyPublic.String())
	assert.Equal(t, "Unknown", PrivacyState(999).String())
}

func TestEditProfile(t *testing.T) {
	t.Parallel()

	t.Run("success_with_overrides", func(t *testing.T) {
		t.Parallel()

		clientMock := mock.NewHTTPStub()

		editHTML := `
		<html>
			<body>
				<div id="profile_edit_config" data-profile-edit='{"strPersonaName":"OldNickname","strRealName":"OldRealName","strSummary":"OldSummary","strCustomURL":"oldurl","LocationData":{"locCountryCode":"US","locStateCode":"FL","locCityCode":"Miami"}}'></div>
			</body>
		</html>`
		clientMock.SetHTMLResponse("profiles/{steamID}/edit/info", 200, editHTML)
		clientMock.SetJSONResponse("profiles/{steamID}/edit", 200, map[string]int{"success": 1})

		newName := "NewNickname"
		newSummary := "NewSummary"
		newCountry := "CA"
		newCustomURL := "newurl"
		settings := Settings{
			Name:      &newName,
			Summary:   &newSummary,
			Country:   &newCountry,
			CustomURL: &newCustomURL,
		}

		err := EditProfile(t.Context(), clientMock, testSteamID, settings)
		assert.NoError(t, err)

		lastCall := clientMock.GetLastCall()
		require.NotNil(t, lastCall)

		bodyBytes, _ := io.ReadAll(lastCall.Body)
		bodyStr := string(bodyBytes)
		assert.Contains(t, bodyStr, "personaName=NewNickname")
		assert.Contains(t, bodyStr, "real_name=OldRealName")
		assert.Contains(t, bodyStr, "summary=NewSummary")
		assert.Contains(t, bodyStr, "country=CA")
		assert.Contains(t, bodyStr, "customURL=newurl")
	})

	t.Run("success_with_remaining_overrides", func(t *testing.T) {
		t.Parallel()

		clientMock := mock.NewHTTPStub()

		editHTML := `
		<html>
			<body>
				<div id="profile_edit_config" data-profile-edit='{"strPersonaName":"a","strRealName":"b","strSummary":"c","strCustomURL":"d","LocationData":{"locCountryCode":"US","locStateCode":"FL","locCityCode":"Miami"}}'></div>
			</body>
		</html>`
		clientMock.SetHTMLResponse("profiles/{steamID}/edit/info", 200, editHTML)
		clientMock.SetJSONResponse("profiles/{steamID}/edit", 200, map[string]int{"success": 1})

		realName := "NewRealName"
		state := "NY"
		city := "NewYork"
		settings := Settings{
			RealName: &realName,
			State:    &state,
			City:     &city,
		}

		err := EditProfile(t.Context(), clientMock, testSteamID, settings)
		assert.NoError(t, err)

		lastCall := clientMock.GetLastCall()
		require.NotNil(t, lastCall)

		bodyBytes, _ := io.ReadAll(lastCall.Body)
		bodyStr := string(bodyBytes)
		assert.Contains(t, bodyStr, "real_name=NewRealName")
		assert.Contains(t, bodyStr, "state=NY")
		assert.Contains(t, bodyStr, "city=NewYork")
	})

	t.Run("missing_config_element", func(t *testing.T) {
		t.Parallel()

		clientMock := mock.NewHTTPStub()
		clientMock.SetHTMLResponse("profiles/{steamID}/edit/info", 200, `<html><body></body></html>`)

		err := EditProfile(t.Context(), clientMock, testSteamID, Settings{})
		assert.ErrorContains(t, err, "could not find profile_edit_config element")
	})

	t.Run("missing_data_profile_edit_attribute", func(t *testing.T) {
		t.Parallel()

		clientMock := mock.NewHTTPStub()
		clientMock.SetHTMLResponse(
			"profiles/{steamID}/edit/info",
			200,
			`<html><body><div id="profile_edit_config"></div></body></html>`,
		)

		err := EditProfile(t.Context(), clientMock, testSteamID, Settings{})
		assert.ErrorContains(t, err, "missing data-profile-edit attribute")
	})

	t.Run("malformed_json_inside_data_profile_edit", func(t *testing.T) {
		t.Parallel()

		clientMock := mock.NewHTTPStub()
		clientMock.SetHTMLResponse(
			"profiles/{steamID}/edit/info",
			200,
			`<html><body><div id="profile_edit_config" data-profile-edit="{invalid_json}"></div></body></html>`,
		)

		err := EditProfile(t.Context(), clientMock, testSteamID, Settings{})
		assert.ErrorContains(t, err, "failed to parse existing settings JSON")
	})

	t.Run("html_parse_failure", func(t *testing.T) {
		t.Parallel()

		mockErr := &parseErrorMock{HTTPStub: mock.NewHTTPStub()}
		err := EditProfile(t.Context(), mockErr, testSteamID, Settings{})
		assert.ErrorContains(t, err, "failed to parse HTML")
	})

	t.Run("html_fetch_failure", func(t *testing.T) {
		t.Parallel()

		clientMock := mock.NewHTTPStub()
		clientMock.ResponseErrs["profiles/76561197960265728/edit/info"] = errors.New("network failure")

		err := EditProfile(t.Context(), clientMock, testSteamID, Settings{})
		assert.ErrorContains(t, err, "failed to fetch edit page")
	})

	t.Run("post_profile_save_failure", func(t *testing.T) {
		t.Parallel()

		clientMock := mock.NewHTTPStub()

		editHTML := `<div id="profile_edit_config" data-profile-edit='{"strPersonaName":"a"}'></div>`
		clientMock.SetHTMLResponse("profiles/{steamID}/edit/info", 200, editHTML)
		clientMock.ResponseErrs["profiles/{steamID}/edit"] = errors.New("post failure")

		err := EditProfile(t.Context(), clientMock, testSteamID, Settings{})
		assert.ErrorContains(t, err, "failed to post profile save")
	})

	t.Run("save_failed_response_with_error_message", func(t *testing.T) {
		t.Parallel()

		clientMock := mock.NewHTTPStub()

		editHTML := `<div id="profile_edit_config" data-profile-edit='{"strPersonaName":"a"}'></div>`
		clientMock.SetHTMLResponse("profiles/{steamID}/edit/info", 200, editHTML)
		clientMock.SetJSONResponse(
			"profiles/{steamID}/edit",
			200,
			map[string]any{"success": 0, "errmsg": "invalid custom URL"},
		)

		err := EditProfile(t.Context(), clientMock, testSteamID, Settings{})
		assert.ErrorContains(t, err, "save failed: invalid custom URL")
	})

	t.Run("save_failed_response_with_empty_message", func(t *testing.T) {
		t.Parallel()

		clientMock := mock.NewHTTPStub()

		editHTML := `<div id="profile_edit_config" data-profile-edit='{"strPersonaName":"a"}'></div>`
		clientMock.SetHTMLResponse("profiles/{steamID}/edit/info", 200, editHTML)
		clientMock.SetJSONResponse(
			"profiles/{steamID}/edit",
			200,
			map[string]any{"success": 0, "errmsg": ""},
		)

		err := EditProfile(t.Context(), clientMock, testSteamID, Settings{})
		assert.ErrorContains(t, err, "save failed: request was not successful")
	})
}

func TestUpdatePrivacySettings(t *testing.T) {
	t.Parallel()

	t.Run("success_privacy_override", func(t *testing.T) {
		t.Parallel()

		clientMock := mock.NewHTTPStub()

		settingsHTML := `
			<html>
				<body>
					<div id="profile_edit_config" data-profile-edit='{"Privacy":{"PrivacySettings":{"PrivacyProfile":3,"PrivacyInventory":3,"PrivacyInventoryGifts":3,"PrivacyOwnedGames":3,"PrivacyPlaytime":3,"PrivacyFriendsList":3},"eCommentPermission":1}}'></div>
				</body>
			</html>`
		clientMock.SetHTMLResponse("profiles/{steamID}/edit/settings", 200, settingsHTML)
		clientMock.SetJSONResponse("profiles/{steamID}/ajaxsetprivacy", 200, map[string]int{"success": 1})

		newProfile := PrivacyPrivate
		newComments := CommentPrivate
		giftsPrivate := true
		settings := PrivacySettings{
			Profile:        &newProfile,
			Comments:       &newComments,
			InventoryGifts: &giftsPrivate,
		}

		err := UpdatePrivacySettings(t.Context(), clientMock, testSteamID, settings)
		assert.NoError(t, err)

		lastCall := clientMock.GetLastCall()
		require.NotNil(t, lastCall)

		bodyBytes, _ := io.ReadAll(lastCall.Body)
		bodyStr := string(bodyBytes)
		assert.Contains(
			t,
			bodyStr,
			"Privacy=%7B%22PrivacyProfile%22%3A1%2C%22PrivacyInventory%22%3A3%2C%22PrivacyInventoryGifts%22%3A1%2C%22PrivacyOwnedGames%22%3A3%2C%22PrivacyPlaytime%22%3A3%2C%22PrivacyFriendsList%22%3A3%7D",
		)
	})

	t.Run("success_full_privacy_override_fields", func(t *testing.T) {
		t.Parallel()

		clientMock := mock.NewHTTPStub()

		rawJSON := `
				<div id="profile_edit_config" data-profile-edit='{"Privacy":{"PrivacySettings":{"PrivacyProfile":3,"PrivacyInventory":3,"PrivacyInventoryGifts":3,"PrivacyPlaytime":3,"PrivacyOwnedGames":3,"PrivacyFriendsList":3},"eCommentPermission":1}}'></div>
			`
		clientMock.SetHTMLResponse("profiles/{steamID}/edit/settings", 200, rawJSON)
		clientMock.SetJSONResponse("profiles/{steamID}/ajaxsetprivacy", 200, map[string]int{"success": 1})

		pState := PrivacyFriendsOnly
		comments := CommentAnyone
		invState := PrivacyFriendsOnly
		giftsPrivate := false
		gameDetails := PrivacyFriendsOnly
		playtimePrivate := false
		friendsList := PrivacyFriendsOnly

		settings := PrivacySettings{
			Profile:        &pState,
			Comments:       &comments,
			Inventory:      &invState,
			InventoryGifts: &giftsPrivate,
			GameDetails:    &gameDetails,
			Playtime:       &playtimePrivate,
			FriendsList:    &friendsList,
		}

		err := UpdatePrivacySettings(t.Context(), clientMock, testSteamID, settings)
		assert.NoError(t, err)

		lastCall := clientMock.GetLastCall()
		require.NotNil(t, lastCall)

		bodyBytes, _ := io.ReadAll(lastCall.Body)
		bodyStr := string(bodyBytes)
		assert.Contains(t, bodyStr, "PrivacyInventory%22%3A2")
		assert.Contains(t, bodyStr, "PrivacyInventoryGifts%22%3A3") // PrivacyPublic
		assert.Contains(t, bodyStr, "PrivacyPlaytime%22%3A3")       // PrivacyPublic
		assert.Contains(t, bodyStr, "eCommentPermission=1")
	})

	t.Run("html_fetch_failure", func(t *testing.T) {
		t.Parallel()

		clientMock := mock.NewHTTPStub()
		clientMock.ResponseErrs["profiles/76561197960265728/edit/settings"] = errors.New("network failure")

		err := UpdatePrivacySettings(t.Context(), clientMock, testSteamID, PrivacySettings{})
		assert.ErrorContains(t, err, "failed to fetch settings page")
	})

	t.Run("missing_data_profile_edit_attribute", func(t *testing.T) {
		t.Parallel()

		clientMock := mock.NewHTTPStub()
		clientMock.SetHTMLResponse(
			"profiles/{steamID}/edit/settings",
			200,
			`<html><body><div id="profile_edit_config"></div></body></html>`,
		)

		err := UpdatePrivacySettings(t.Context(), clientMock, testSteamID, PrivacySettings{})
		assert.ErrorContains(t, err, "missing data-profile-edit attribute")
	})

	t.Run("malformed_json_inside_data_profile_edit", func(t *testing.T) {
		t.Parallel()

		clientMock := mock.NewHTTPStub()
		clientMock.SetHTMLResponse(
			"profiles/{steamID}/edit/settings",
			200,
			`<html><body><div id="profile_edit_config" data-profile-edit="{invalid_json}"></div></body></html>`,
		)

		err := UpdatePrivacySettings(t.Context(), clientMock, testSteamID, PrivacySettings{})
		assert.ErrorContains(t, err, "failed to parse existing privacy settings JSON")
	})

	t.Run("html_parse_failure", func(t *testing.T) {
		t.Parallel()

		mockErr := &parseErrorMock{HTTPStub: mock.NewHTTPStub()}
		err := UpdatePrivacySettings(t.Context(), mockErr, testSteamID, PrivacySettings{})
		assert.ErrorContains(t, err, "failed to parse HTML")
	})

	t.Run("post_privacy_settings_failure", func(t *testing.T) {
		t.Parallel()

		clientMock := mock.NewHTTPStub()

		settingsHTML := `<div id="profile_edit_config" data-profile-edit='{"Privacy":{"PrivacySettings":{},"eCommentPermission":1}}'></div>`
		clientMock.SetHTMLResponse("profiles/{steamID}/edit/settings", 200, settingsHTML)
		clientMock.ResponseErrs["profiles/{steamID}/ajaxsetprivacy"] = errors.New("post failure")

		err := UpdatePrivacySettings(t.Context(), clientMock, testSteamID, PrivacySettings{})
		assert.ErrorContains(t, err, "failed to post privacy settings")
	})

	t.Run("ajax_set_privacy_fails", func(t *testing.T) {
		t.Parallel()

		clientMock := mock.NewHTTPStub()

		settingsHTML := `<div id="profile_edit_config" data-profile-edit='{"Privacy":{"PrivacySettings":{},"eCommentPermission":1}}'></div>`
		clientMock.SetHTMLResponse("profiles/{steamID}/edit/settings", 200, settingsHTML)
		clientMock.SetJSONResponse("profiles/{steamID}/ajaxsetprivacy", 200, map[string]int{"success": 0})

		err := UpdatePrivacySettings(t.Context(), clientMock, testSteamID, PrivacySettings{})
		assert.ErrorContains(t, err, "privacy save failed: success=0")
	})
}

func TestUploadAvatar(t *testing.T) {
	t.Parallel()

	dummyImage := []byte("image_data_bytes_12345")

	t.Run("success_jpg_upload", func(t *testing.T) {
		t.Parallel()

		clientMock := mock.NewHTTPStub()
		clientMock.SetJSONResponse(
			"actions/FileUploader",
			200,
			map[string]any{"success": true, "hash": "new_jpg_avatar_hash"},
		)

		hash, err := UploadAvatar(t.Context(), clientMock, testSteamID, dummyImage, "image/jpeg")
		assert.NoError(t, err)
		assert.Equal(t, "new_jpg_avatar_hash", hash)

		lastCall := clientMock.GetLastCall()
		require.NotNil(t, lastCall)

		bodyBytes, _ := io.ReadAll(lastCall.Body)
		bodyStr := string(bodyBytes)
		assert.Contains(t, bodyStr, "player_avatar_image")
		assert.Contains(t, bodyStr, "76561197960265728")
		assert.Contains(t, bodyStr, "avatar.jpg")
		assert.Contains(t, bodyStr, "image_data_bytes_12345")
	})

	t.Run("success_png_upload", func(t *testing.T) {
		t.Parallel()

		clientMock := mock.NewHTTPStub()
		clientMock.SetJSONResponse("actions/FileUploader", 200, map[string]any{"success": true, "hash": "png_hash"})

		hash, err := UploadAvatar(t.Context(), clientMock, testSteamID, dummyImage, "image/png")
		assert.NoError(t, err)
		assert.Equal(t, "png_hash", hash)
	})

	t.Run("success_gif_upload", func(t *testing.T) {
		t.Parallel()

		clientMock := mock.NewHTTPStub()
		clientMock.SetJSONResponse("actions/FileUploader", 200, map[string]any{"success": true, "hash": "gif_hash"})

		hash, err := UploadAvatar(t.Context(), clientMock, testSteamID, dummyImage, "gif")
		assert.NoError(t, err)
		assert.Equal(t, "gif_hash", hash)
	})

	t.Run("empty_image", func(t *testing.T) {
		t.Parallel()

		clientMock := mock.NewHTTPStub()
		_, err := UploadAvatar(t.Context(), clientMock, testSteamID, nil, "png")
		assert.ErrorContains(t, err, "empty avatar image buffer")
	})

	t.Run("unsupported_format", func(t *testing.T) {
		t.Parallel()

		clientMock := mock.NewHTTPStub()
		_, err := UploadAvatar(t.Context(), clientMock, testSteamID, dummyImage, "image/tiff")
		assert.ErrorContains(t, err, "unsupported content-type")
	})

	t.Run("upload_request_failed", func(t *testing.T) {
		t.Parallel()

		clientMock := mock.NewHTTPStub()
		clientMock.ResponseErrs["actions/FileUploader"] = errors.New("post failure")

		_, err := UploadAvatar(t.Context(), clientMock, testSteamID, dummyImage, "png")
		assert.ErrorContains(t, err, "upload request failed")
	})

	t.Run("upload_fails_with_error_message", func(t *testing.T) {
		t.Parallel()

		clientMock := mock.NewHTTPStub()
		clientMock.SetJSONResponse(
			"actions/FileUploader",
			200,
			map[string]any{"success": false, "message": "file too large"},
		)

		_, err := UploadAvatar(t.Context(), clientMock, testSteamID, dummyImage, "png")
		assert.ErrorContains(t, err, "upload failed: file too large")
	})

	t.Run("upload_fails_with_empty_message", func(t *testing.T) {
		t.Parallel()

		clientMock := mock.NewHTTPStub()
		clientMock.SetJSONResponse(
			"actions/FileUploader",
			200,
			map[string]any{"success": false, "message": ""},
		)

		_, err := UploadAvatar(t.Context(), clientMock, testSteamID, dummyImage, "png")
		assert.ErrorContains(t, err, "upload failed: upload was not successful")
	})
}

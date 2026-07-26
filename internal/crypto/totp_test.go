// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package crypto

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lemon4ksan/g-man/internal/bytesconv"
)

func TestGenerateAuthCode(t *testing.T) {
	t.Run("Success", func(t *testing.T) {
		sharedSecret := []byte("q87IsS7v6pY4iV7kG8U9pW7f/E4=")

		var timestamp int64 = 1741514400

		code := GenerateAuthCode(sharedSecret, timestamp)

		assert.Len(t, code, 5, "expected code length to be exactly 5")

		for _, char := range bytesconv.B2S(code[:]) {
			assert.True(t, strings.ContainsRune(steamChars, char), "invalid character %c in code", char)
		}
	})

	t.Run("InvalidSecret", func(t *testing.T) {
		// Providing an invalid Base64 string to trigger the error branch
		invalidSecret := []byte("!!!")

		var timestamp int64 = 1741514400

		code := GenerateAuthCode(invalidSecret, timestamp)
		assert.Empty(t, code, "expected empty code for invalid secret")
	})
}

func TestGenerateConfirmationKey(t *testing.T) {
	t.Run("SuccessShortTag", func(t *testing.T) {
		identitySecret := "v6pY4iV7kG8U9pW7f/E4q87IsS7="

		var timestamp int64 = 1741514400

		tag := "conf"

		key := GenerateConfirmationKey([]byte(identitySecret), timestamp, tag)
		assert.NotEmpty(t, key, "confirmation key should not be empty")
	})

	t.Run("SuccessLongTag", func(t *testing.T) {
		// Testing the branch where tag length > 32
		identitySecret := "v6pY4iV7kG8U9pW7f/E4q87IsS7="

		var timestamp int64 = 1741514400

		longTag := strings.Repeat("a", 40)

		key := GenerateConfirmationKey([]byte(identitySecret), timestamp, longTag)
		assert.NotEmpty(t, key, "confirmation key with long tag should not be empty")
	})

	t.Run("InvalidSecret", func(t *testing.T) {
		invalidSecret := "!!!"

		var timestamp int64 = 1741514400

		key := GenerateConfirmationKey([]byte(invalidSecret), timestamp, "conf")
		assert.Empty(t, key, "confirmation key with invalid secret should be empty")
	})
}

func TestGetDeviceID(t *testing.T) {
	var steamID uint64 = 76561197960287930

	deviceID := GetDeviceID(steamID)

	// Validate prefix
	assert.True(t, strings.HasPrefix(deviceID, "android:"), "invalid prefix: %s", deviceID)

	// Validate UUID structure (8-4-4-4-12)
	parts := strings.Split(strings.TrimPrefix(deviceID, "android:"), "-")
	require.Len(t, parts, 5, "invalid UUID structure, expected 5 parts")

	expectedLengths := []int{8, 4, 4, 4, 12}
	for i, length := range expectedLengths {
		assert.Len(t, parts[i], length, "part %d length mismatch", i)
	}

	// Ensure different SteamIDs produce different DeviceIDs
	assert.NotEqual(t, deviceID, GetDeviceID(123456), "different SteamIDs should produce different DeviceIDs")
}

func TestDecodeSecret(t *testing.T) {
	t.Run("Base64Success", func(t *testing.T) {
		// #nosec G101 -- Test vector
		secret := "SGVsbG8=" // "Hello"

		decoded, err := DecodeSecret(secret)
		require.NoError(t, err, "base64 decode should not fail")
		assert.Equal(t, "Hello", string(decoded), "decoded base64 string mismatch")
	})

	t.Run("HexSuccess", func(t *testing.T) {
		// Valid 40-character hex string
		hexSecret := "3132333435363738393031323334353637383930"

		decoded, err := DecodeSecret(hexSecret)
		require.NoError(t, err, "hex decode should not fail")
		assert.Len(t, decoded, 20, "expected exactly 20 bytes from hex string")
		assert.Equal(t, "12345678901234567890", string(decoded))
	})

	t.Run("InvalidBase64", func(t *testing.T) {
		// String that is neither valid hex nor valid base64
		secret := "this-is-not-valid-padding!"

		_, err := DecodeSecret(secret)
		require.Error(t, err, "expected error for invalid base64 string")
	})

	t.Run("InvalidHex", func(t *testing.T) {
		// A hex string with an odd length is invalid
		// This will fail the regexp and fall back to base64, which will also fail
		hexSecret := "abcde"

		_, err := DecodeSecret(hexSecret)
		require.Error(t, err, "expected error for odd-length hex string")
	})
}

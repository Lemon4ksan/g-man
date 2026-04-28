// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package crypto

import (
	"strings"
	"testing"
)

func TestGenerateAuthCode(t *testing.T) {
	t.Run("Success", func(t *testing.T) {
		sharedSecret := "q87IsS7v6pY4iV7kG8U9pW7f/E4="

		var timestamp int64 = 1741514400

		code, err := GenerateAuthCode(sharedSecret, timestamp)
		if err != nil {
			t.Fatalf("failed to generate auth code: %v", err)
		}

		if len(code) != 5 {
			t.Errorf("expected code length 5, got %d", len(code))
		}

		for _, char := range code {
			if !strings.ContainsRune(steamChars, char) {
				t.Errorf("invalid character %c in code", char)
			}
		}
	})

	t.Run("InvalidSecret", func(t *testing.T) {
		// Providing an invalid Base64 string to trigger the error branch
		invalidSecret := "!!!"

		var timestamp int64 = 1741514400

		_, err := GenerateAuthCode(invalidSecret, timestamp)
		if err == nil {
			t.Error("expected error for invalid secret, but got nil")
		}
	})
}

func TestGenerateConfirmationKey(t *testing.T) {
	t.Run("SuccessShortTag", func(t *testing.T) {
		identitySecret := "v6pY4iV7kG8U9pW7f/E4q87IsS7="

		var timestamp int64 = 1741514400

		tag := "conf"

		key, err := GenerateConfirmationKey(identitySecret, timestamp, tag)
		if err != nil {
			t.Fatalf("failed to generate confirmation key: %v", err)
		}

		if key == "" {
			t.Fatal("confirmation key is empty")
		}
	})

	t.Run("SuccessLongTag", func(t *testing.T) {
		// Testing the branch where tag length > 32
		identitySecret := "v6pY4iV7kG8U9pW7f/E4q87IsS7="

		var timestamp int64 = 1741514400

		longTag := strings.Repeat("a", 40)

		key, err := GenerateConfirmationKey(identitySecret, timestamp, longTag)
		if err != nil {
			t.Fatalf("failed to generate confirmation key with long tag: %v", err)
		}

		if key == "" {
			t.Fatal("confirmation key with long tag is empty")
		}
	})

	t.Run("InvalidSecret", func(t *testing.T) {
		invalidSecret := "!!!"

		var timestamp int64 = 1741514400

		_, err := GenerateConfirmationKey(invalidSecret, timestamp, "conf")
		if err == nil {
			t.Error("expected error for invalid secret, but got nil")
		}
	})
}

func TestGetDeviceID(t *testing.T) {
	var steamID uint64 = 76561197960287930

	deviceID := GetDeviceID(steamID)

	// Validate prefix
	if !strings.HasPrefix(deviceID, "android:") {
		t.Errorf("invalid prefix: %s", deviceID)
	}

	// Validate UUID structure (8-4-4-4-12)
	parts := strings.Split(strings.TrimPrefix(deviceID, "android:"), "-")
	if len(parts) != 5 {
		t.Fatalf("invalid UUID structure, parts count: %d", len(parts))
	}

	expectedLengths := []int{8, 4, 4, 4, 12}
	for i, length := range expectedLengths {
		if len(parts[i]) != length {
			t.Errorf("part %d length mismatch: expected %d, got %d", i, length, len(parts[i]))
		}
	}

	// Ensure different SteamIDs produce different DeviceIDs
	if deviceID == GetDeviceID(123456) {
		t.Error("different SteamIDs should produce different DeviceIDs")
	}
}

func TestDecodeSecret(t *testing.T) {
	t.Run("Base64Success", func(t *testing.T) {
		// #nosec G101 -- Test vector
		secret := "SGVsbG8=" // "Hello"

		decoded, err := decodeSecret(secret)
		if err != nil || string(decoded) != "Hello" {
			t.Errorf("base64 decode failed: %v", err)
		}
	})

	t.Run("HexSuccess", func(t *testing.T) {
		// Valid 40-character hex string
		hexSecret := "3132333435363738393031323334353637383930"

		decoded, err := decodeSecret(hexSecret)
		if err != nil {
			t.Fatalf("hex decode failed: %v", err)
		}

		if len(decoded) != 20 {
			t.Errorf("expected 20 bytes, got %d", len(decoded))
		}
	})

	t.Run("InvalidBase64", func(t *testing.T) {
		// String that is neither valid hex nor valid base64
		secret := "this-is-not-valid-padding!"

		_, err := decodeSecret(secret)
		if err == nil {
			t.Error("expected error for invalid base64 string, but got nil")
		}
	})

	t.Run("InvalidHex", func(t *testing.T) {
		// A hex string with an odd length is invalid
		hexSecret := "abcde"

		_, err := decodeSecret(hexSecret)
		if err == nil {
			t.Error("expected error for odd-length hex string, but got nil")
		}
	})
}

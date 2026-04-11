// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package crypto

import (
	"strings"
	"testing"
)

func TestGenerateAuthCode(t *testing.T) {
	// #nosec G101 -- Test vector, not a real credential
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
}

func TestGenerateConfirmationKey(t *testing.T) {
	// #nosec G101 -- Test vector, not a real credential
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

	if len(key) < 20 {
		t.Errorf("confirmation key too short: %s", key)
	}
}

func TestGetDeviceID(t *testing.T) {
	var steamID uint64 = 76561197960287930

	deviceID := GetDeviceID(steamID)

	if !strings.HasPrefix(deviceID, "android:") {
		t.Errorf("invalid prefix: %s", deviceID)
	}

	parts := strings.Split(strings.TrimPrefix(deviceID, "android:"), "-")
	if len(parts) != 5 {
		t.Errorf("invalid UUID structure, parts: %d", len(parts))
	}

	expectedLengths := []int{8, 4, 4, 4, 12}
	for i, length := range expectedLengths {
		if len(parts[i]) != length {
			t.Errorf("part %d length mismatch: expected %d, got %d", i, length, len(parts[i]))
		}
	}
}

func TestDecodeSecret(t *testing.T) {
	t.Run("Base64", func(t *testing.T) {
		// #nosec G101 -- Test vector, not a real credential
		secret := "SGVsbG8=" // Hello

		decoded, err := decodeSecret(secret)
		if err != nil || string(decoded) != "Hello" {
			t.Errorf("base64 decode failed: %v", err)
		}
	})

	t.Run("Hex", func(t *testing.T) {
		hexSecret := "3132333435363738393031323334353637383930"

		decoded, err := decodeSecret(hexSecret)
		if err != nil {
			t.Fatalf("hex decode failed: %v", err)
		}

		if len(decoded) != 20 {
			t.Errorf("expected 20 bytes, got %d", len(decoded))
		}
	})
}

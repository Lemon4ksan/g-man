// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package crypto

import (
	"bytes"
	"crypto/aes"
	"testing"
)

func TestPKCS7Padding(t *testing.T) {
	blockSize := 16
	cases := []struct {
		name  string
		input []byte
	}{
		{"Empty", []byte{}},
		{"PartialBlock", []byte("hello")},
		{"FullBlock", bytes.Repeat([]byte{1}, blockSize)},
		{"MultipleBlocks", bytes.Repeat([]byte{2}, blockSize*2+5)},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			padded := pkcs7Pad(tc.input, blockSize)
			if len(padded)%blockSize != 0 {
				t.Errorf("padded length %d is not multiple of %d", len(padded), blockSize)
			}

			unpadded, err := pkcs7Unpad(padded, blockSize)
			if err != nil {
				t.Fatalf("unpad failed: %v", err)
			}

			if !bytes.Equal(tc.input, unpadded) {
				t.Errorf("expected %v, got %v", tc.input, unpadded)
			}
		})
	}
}

func TestSymmetricEncryptionRoundTrip(t *testing.T) {
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i)
	}
	plaintext := []byte("secret steam message 2026")

	t.Run("StandardCBC", func(t *testing.T) {
		encrypted, err := SymmetricEncrypt(plaintext, key, nil)
		if err != nil {
			t.Fatalf("encryption failed: %v", err)
		}

		decrypted, err := SymmetricDecrypt(encrypted, key, false)
		if err != nil {
			t.Fatalf("decryption failed: %v", err)
		}

		if !bytes.Equal(plaintext, decrypted) {
			t.Errorf("mismatch: got %s", decrypted)
		}
	})

	t.Run("WithHmacIv", func(t *testing.T) {
		encrypted, err := SymmetricEncryptWithHmacIv(plaintext, key)
		if err != nil {
			t.Fatalf("encryption failed: %v", err)
		}

		// Проверяем с проверкой HMAC
		decrypted, err := SymmetricDecrypt(encrypted, key, true)
		if err != nil {
			t.Fatalf("decryption with hmac check failed: %v", err)
		}

		if !bytes.Equal(plaintext, decrypted) {
			t.Errorf("mismatch: got %s", decrypted)
		}
	})
}

func TestSymmetricDecryptECB(t *testing.T) {
	key := make([]byte, 32)
	plaintext := []byte("raw ecb payload!!") // 17 bytes, will be 2 blocks padded

	block, _ := aes.NewCipher(key)
	padded := pkcs7Pad(plaintext, aes.BlockSize)
	encrypted := make([]byte, len(padded))
	for i := 0; i < len(padded); i += aes.BlockSize {
		block.Encrypt(encrypted[i:i+aes.BlockSize], padded[i:i+aes.BlockSize])
	}

	decrypted, err := SymmetricDecryptECB(encrypted, key)
	if err != nil {
		t.Fatalf("ECB decryption failed: %v", err)
	}

	if !bytes.Equal(plaintext, decrypted) {
		t.Errorf("expected %s, got %s", plaintext, decrypted)
	}
}

func TestMachineID(t *testing.T) {
	id := GenerateAccountMachineID("lemon4ksan")
	if len(id) == 0 {
		t.Fatal("generated machine id is empty")
	}

	if id[0] != 0x00 {
		t.Errorf("expected VDF map start 0x00, got 0x%x", id[0])
	}
	if !bytes.Contains(id, []byte("MessageObject")) {
		t.Error("machine id does not contain MessageObject")
	}
}

func TestWipe(t *testing.T) {
	data := []byte{1, 2, 3, 4, 5}
	Wipe(data)
	for _, b := range data {
		if b != 0 {
			t.Error("Wipe did not zero out the memory")
		}
	}
}

// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package crypto

import (
	"bytes"
	"crypto/aes"
	"crypto/rand"
	"errors"
	"fmt"
	"strings"
	"testing"
)

type errorReader struct{}

func (r *errorReader) Read(p []byte) (n int, err error) {
	return 0, errors.New("mock reader error")
}

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

func TestPKCS7UnpadErrors(t *testing.T) {
	blockSize := 16
	t.Run("EmptyData", func(t *testing.T) {
		_, err := pkcs7Unpad([]byte{}, blockSize)
		if err == nil || err.Error() != "empty data" {
			t.Errorf("expected empty data error, got %v", err)
		}
	})

	t.Run("InvalidBlockSize", func(t *testing.T) {
		_, err := pkcs7Unpad([]byte{1, 2, 3}, blockSize)
		if err == nil || !strings.Contains(err.Error(), "multiple of block size") {
			t.Errorf("expected block size error, got %v", err)
		}
	})

	t.Run("InvalidPaddingValue", func(t *testing.T) {
		// Последний байт 0 или больше blockSize - неверно
		data := make([]byte, blockSize)
		data[blockSize-1] = 0

		_, err := pkcs7Unpad(data, blockSize)
		if err == nil || err.Error() != "invalid padding" {
			t.Errorf("expected invalid padding error, got %v", err)
		}

		data[blockSize-1] = byte(blockSize + 1)

		_, err = pkcs7Unpad(data, blockSize)
		if err == nil {
			t.Error("expected error for padding > blockSize")
		}
	})

	t.Run("InconsistentPadding", func(t *testing.T) {
		data := bytes.Repeat([]byte{byte(blockSize)}, blockSize)
		data[len(data)-1] = 0x03
		data[len(data)-2] = 0x03
		data[len(data)-3] = 0x01 // error here

		_, err := pkcs7Unpad(data, blockSize)
		if err == nil || err.Error() != "invalid padding" {
			t.Errorf("expected invalid padding error for inconsistent bytes, got %v", err)
		}
	})
}

func TestGenerateSessionKey(t *testing.T) {
	t.Run("SuccessWithNonce", func(t *testing.T) {
		nonce := []byte("testnonce")

		sessionKey, encrypted, err := GenerateSessionKey(nonce)
		if err != nil {
			t.Fatalf("GenerateSessionKey with nonce failed: %v", err)
		}

		if len(sessionKey) != 32 {
			t.Errorf("expected session key of 32 bytes, got %d", len(sessionKey))
		}

		if len(encrypted) == 0 {
			t.Error("encrypted key is empty")
		}
	})

	t.Run("RandReadError", func(t *testing.T) {
		originalReader := rand.Reader

		rand.Reader = &errorReader{}
		defer func() { rand.Reader = originalReader }()

		_, _, err := GenerateSessionKey(nil)
		if err == nil {
			t.Fatal("expected an error from rand.Reader but got nil")
		}

		if !strings.Contains(err.Error(), "mock reader error") {
			t.Errorf("expected 'mock reader error', got '%v'", err)
		}
	})
}

func TestSymmetricEncryptionRoundTrip(t *testing.T) {
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i)
	}

	plaintext := []byte("secret steam message 2026")

	t.Run("StandardCBC", func(t *testing.T) {
		// Test with a specific IV
		iv := make([]byte, aes.BlockSize)
		for i := range iv {
			iv[i] = byte(i)
		}

		encrypted, err := SymmetricEncrypt(plaintext, key, iv)
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

		// Test with nil IV to trigger auto-generation
		encrypted, err = SymmetricEncrypt(plaintext, key, nil)
		if err != nil {
			t.Fatalf("encryption with nil IV failed: %v", err)
		}

		decrypted, err = SymmetricDecrypt(encrypted, key, false)
		if err != nil {
			t.Fatalf("decryption with nil IV failed: %v", err)
		}

		if !bytes.Equal(plaintext, decrypted) {
			t.Errorf("mismatch with nil IV: got %s", decrypted)
		}
	})

	t.Run("WithHmacIv", func(t *testing.T) {
		encrypted, err := SymmetricEncryptWithHmacIv(plaintext, key)
		if err != nil {
			t.Fatalf("encryption failed: %v", err)
		}

		decrypted, err := SymmetricDecrypt(encrypted, key, true)
		if err != nil {
			t.Fatalf("decryption with hmac check failed: %v", err)
		}

		if !bytes.Equal(plaintext, decrypted) {
			t.Errorf("mismatch: got %s", decrypted)
		}
	})

	t.Run("WithHmacIv_InvalidHmacCheck", func(t *testing.T) {
		longPlaintext := bytes.Repeat([]byte("a"), 48)
		encrypted, _ := SymmetricEncryptWithHmacIv(longPlaintext, key)

		encrypted[0] ^= 0xFF

		_, err := SymmetricDecrypt(encrypted, key, true)
		if err == nil {
			t.Fatal("expected error for invalid HMAC, got nil")
		}

		if !strings.Contains(err.Error(), "received invalid HMAC") {
			t.Errorf("expected HMAC error, got: %v", err)
		}
	})
}

func TestSymmetricEncryptErrors(t *testing.T) {
	key32 := make([]byte, 32)
	key16 := make([]byte, 16)
	iv15 := make([]byte, 15)
	plaintext := []byte("some data")

	t.Run("BadKeyLength", func(t *testing.T) {
		_, err := SymmetricEncrypt(plaintext, key16, nil)
		if err == nil || !strings.Contains(err.Error(), "key must be 32 bytes") {
			t.Errorf("expected key size error for SymmetricEncrypt, got %v", err)
		}
	})

	t.Run("BadIVLength", func(t *testing.T) {
		_, err := SymmetricEncrypt(plaintext, key32, iv15)
		if err == nil || !strings.Contains(err.Error(), "IV must be 16 bytes") {
			t.Errorf("expected IV size error, got %v", err)
		}
	})

	t.Run("RandReadErrorIV", func(t *testing.T) {
		originalReader := rand.Reader

		rand.Reader = &errorReader{}
		defer func() { rand.Reader = originalReader }()

		_, err := SymmetricEncrypt(plaintext, key32, nil)
		if err == nil {
			t.Fatal("expected an error from rand.Reader during IV generation but got nil")
		}
	})

	t.Run("Hmac_BadKeyLength", func(t *testing.T) {
		_, err := SymmetricEncryptWithHmacIv(plaintext, key16)
		if err == nil || !strings.Contains(err.Error(), "key must be 32 bytes") {
			t.Errorf("expected key size error for SymmetricEncryptWithHmacIv, got %v", err)
		}
	})

	t.Run("Hmac_RandReadError", func(t *testing.T) {
		originalReader := rand.Reader

		rand.Reader = &errorReader{}
		defer func() { rand.Reader = originalReader }()

		_, err := SymmetricEncryptWithHmacIv(plaintext, key32)
		if err == nil {
			t.Fatal("expected an error from rand.Reader during HMAC IV generation but got nil")
		}
	})
}

func TestSymmetricDecryptErrors(t *testing.T) {
	key32 := make([]byte, 32)
	key16 := make([]byte, 16)
	validEncrypted, _ := SymmetricEncrypt([]byte("data"), key32, nil)

	t.Run("BadKeyLength", func(t *testing.T) {
		_, err := SymmetricDecrypt(validEncrypted, key16, false)
		if err == nil || !strings.Contains(err.Error(), "key must be 32 bytes") {
			t.Errorf("expected key size error, got %v", err)
		}
	})

	t.Run("InputTooShort", func(t *testing.T) {
		shortInput := []byte{1, 2, 3}

		_, err := SymmetricDecrypt(shortInput, key32, false)
		if err == nil || !strings.Contains(err.Error(), "input too short") {
			t.Errorf("expected input size error, got %v", err)
		}
	})

	t.Run("BadCiphertextLength", func(t *testing.T) {
		badLenInput := make([]byte, aes.BlockSize+5) // 16 byte IV + 5 byte ciphertext

		_, err := SymmetricDecrypt(badLenInput, key32, false)
		if err == nil || !strings.Contains(err.Error(), "not a multiple of block size") {
			t.Errorf("expected ciphertext length error, got %v", err)
		}
	})
}

func TestSymmetricDecryptECB(t *testing.T) {
	key := make([]byte, 32)
	plaintext := []byte("raw ecb payload!!") // 17 bytes, will be 2 blocks padded

	block, _ := aes.NewCipher(key)
	padded := pkcs7Pad(plaintext, aes.BlockSize)

	encrypted := make([]byte, len(padded))
	// Manual ECB encryption for test
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

func TestSymmetricDecryptECBErrors(t *testing.T) {
	key32 := make([]byte, 32)
	key16 := make([]byte, 16)
	validBlock := make([]byte, aes.BlockSize)

	t.Run("BadKeyLength", func(t *testing.T) {
		_, err := SymmetricDecryptECB(validBlock, key16)
		if err == nil || !strings.Contains(err.Error(), "key must be 32 bytes") {
			t.Errorf("expected key size error, got %v", err)
		}
	})

	t.Run("BadInputLength", func(t *testing.T) {
		badLenInput := []byte{1, 2, 3, 4, 5}

		_, err := SymmetricDecryptECB(badLenInput, key32)
		if err == nil || !strings.Contains(err.Error(), "not a multiple of block size") {
			t.Errorf("expected input length error, got %v", err)
		}
	})
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

func TestGenerateSessionKey_Error(t *testing.T) {
	hugeNonce := make([]byte, 500)

	_, _, err := GenerateSessionKey(hugeNonce)
	if err == nil {
		t.Error("expected error due to large nonce, but got nil")
	}
}

func TestSymmetricDecrypt_PaddingError(t *testing.T) {
	key := make([]byte, 32)
	input := make([]byte, 32)
	input[31] = 0x99

	_, err := SymmetricDecrypt(input, key, false)
	if err == nil {
		t.Error("expected padding error, but got nil")
	}
}

func TestWipe(t *testing.T) {
	data := []byte{1, 2, 3, 4, 5}
	Wipe(data)

	expected := []byte{0, 0, 0, 0, 0}
	if !bytes.Equal(data, expected) {
		t.Errorf("Wipe did not zero out the memory, got: %v", data)
	}
}

func TestMustParsePublicKey_Errors(t *testing.T) {
	t.Run("InvalidDERData", func(t *testing.T) {
		badDerPem := []byte("-----BEGIN PUBLIC KEY-----\naGVsbG8=\n-----END PUBLIC KEY-----")

		defer func() {
			r := recover()
			if r == nil {
				t.Error("expected panic on invalid PKIX data, but it didn't")
			}

			if !strings.Contains(fmt.Sprint(r), "failed to parse public key") {
				t.Errorf("expected parse error panic, got: %v", r)
			}
		}()

		mustParsePublicKey(badDerPem)
	})

	t.Run("NotRSAPublicKey", func(t *testing.T) {
		ecdsaPem := []byte(`-----BEGIN PUBLIC KEY-----
MFYwEAYHKoZIzj0CAQYFK4EEAAoDQgAEJXSGKQJn3ohXt4ibm5cl5h2D0JaLUQU0
rqFGN/I3MuPqEVKOTKxJfoYGYDvoSaYFlwW7DyjvQWMOkQ1XJsnWRg==
-----END PUBLIC KEY-----
`)

		defer func() {
			r := recover()
			if r == nil {
				t.Error("expected panic on non-RSA key, but it didn't")
			}

			if !strings.Contains(fmt.Sprint(r), "failed to parse public key") {
				t.Errorf("expected 'failed to parse public key' panic, got: %v", r)
			}
		}()

		mustParsePublicKey(ecdsaPem)
	})

	t.Run("InvalidPEMBlock", func(t *testing.T) {
		defer func() {
			r := recover()
			if r == nil {
				t.Error("expected panic on invalid PEM, but it didn't")
			}
		}()

		mustParsePublicKey([]byte("not a pem at all"))
	})
}

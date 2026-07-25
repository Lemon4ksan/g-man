// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package crypto

import (
	"bytes"
	"crypto/aes"
	"crypto/rand"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
			assert.Zero(t, len(padded)%blockSize, "padded length should be multiple of block size")

			unpadded, err := pkcs7Unpad(padded, blockSize)
			require.NoError(t, err)
			assert.Equal(t, tc.input, unpadded)
		})
	}
}

func TestPKCS7UnpadErrors(t *testing.T) {
	blockSize := 16

	t.Run("EmptyData", func(t *testing.T) {
		_, err := pkcs7Unpad([]byte{}, blockSize)
		assert.EqualError(t, err, "empty data")
	})

	t.Run("InvalidBlockSize", func(t *testing.T) {
		_, err := pkcs7Unpad([]byte{1, 2, 3}, blockSize)
		assert.ErrorContains(t, err, "multiple of block size")
	})

	t.Run("InvalidPaddingValue", func(t *testing.T) {
		data := make([]byte, blockSize)
		data[blockSize-1] = 0
		_, err := pkcs7Unpad(data, blockSize)
		assert.EqualError(t, err, "invalid padding")

		data[blockSize-1] = byte(blockSize + 1)
		_, err = pkcs7Unpad(data, blockSize)
		assert.Error(t, err, "expected error for padding > blockSize")
	})

	t.Run("InconsistentPadding", func(t *testing.T) {
		data := bytes.Repeat([]byte{byte(blockSize)}, blockSize)
		data[len(data)-1] = 0x03
		data[len(data)-2] = 0x03
		data[len(data)-3] = 0x01 // inconsistent

		_, err := pkcs7Unpad(data, blockSize)
		assert.EqualError(t, err, "invalid padding")
	})
}

func TestGenerateSessionKey(t *testing.T) {
	t.Run("SuccessWithNonce", func(t *testing.T) {
		nonce := []byte("testnonce")

		sessionKey, encrypted, err := GenerateSessionKey(nonce)
		require.NoError(t, err)
		assert.Len(t, sessionKey, 32)
		assert.NotEmpty(t, encrypted)
	})

	t.Run("RandReadError", func(t *testing.T) {
		originalReader := rand.Reader

		rand.Reader = &errorReader{}
		defer func() { rand.Reader = originalReader }()

		_, _, err := GenerateSessionKey(nil)
		assert.ErrorContains(t, err, "mock reader error")
	})
}

func TestSymmetricEncryptionRoundTrip(t *testing.T) {
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i)
	}

	plaintext := []byte("secret steam message 2026")

	t.Run("StandardCBC", func(t *testing.T) {
		iv := make([]byte, aes.BlockSize)
		for i := range iv {
			iv[i] = byte(i)
		}

		encrypted, err := SymmetricEncrypt(plaintext, key, iv)
		require.NoError(t, err)

		decrypted, err := SymmetricDecrypt(encrypted, key, false)
		require.NoError(t, err)
		assert.Equal(t, plaintext, decrypted)

		// Test with nil IV auto-generation
		encrypted, err = SymmetricEncrypt(plaintext, key, nil)
		require.NoError(t, err)

		decrypted, err = SymmetricDecrypt(encrypted, key, false)
		require.NoError(t, err)
		assert.Equal(t, plaintext, decrypted)
	})

	t.Run("WithHmacIv", func(t *testing.T) {
		encrypted, err := SymmetricEncryptWithHmacIv(plaintext, key)
		require.NoError(t, err)

		decrypted, err := SymmetricDecrypt(encrypted, key, true)
		require.NoError(t, err)
		assert.Equal(t, plaintext, decrypted)
	})

	t.Run("WithHmacIv_InvalidHmacCheck", func(t *testing.T) {
		longPlaintext := bytes.Repeat([]byte("a"), 48)
		encrypted, _ := SymmetricEncryptWithHmacIv(longPlaintext, key)

		encrypted[0] ^= 0xFF // Corrupt data

		_, err := SymmetricDecrypt(encrypted, key, true)
		assert.ErrorContains(t, err, "received invalid HMAC")
	})
}

func TestSymmetricEncryptErrors(t *testing.T) {
	key32 := make([]byte, 32)
	key16 := make([]byte, 16)
	iv15 := make([]byte, 15)
	plaintext := []byte("some data")

	t.Run("BadKeyLength", func(t *testing.T) {
		_, err := SymmetricEncrypt(plaintext, key16, nil)
		assert.ErrorContains(t, err, "key must be 32 bytes")
	})

	t.Run("BadIVLength", func(t *testing.T) {
		_, err := SymmetricEncrypt(plaintext, key32, iv15)
		assert.ErrorContains(t, err, "IV must be 16 bytes")
	})

	t.Run("RandReadErrorIV", func(t *testing.T) {
		originalReader := rand.Reader

		rand.Reader = &errorReader{}
		defer func() { rand.Reader = originalReader }()

		_, err := SymmetricEncrypt(plaintext, key32, nil)
		assert.Error(t, err)
	})

	t.Run("Hmac_BadKeyLength", func(t *testing.T) {
		_, err := SymmetricEncryptWithHmacIv(plaintext, key16)
		assert.ErrorContains(t, err, "key must be 32 bytes")
	})

	t.Run("Hmac_RandReadError", func(t *testing.T) {
		originalReader := rand.Reader

		rand.Reader = &errorReader{}
		defer func() { rand.Reader = originalReader }()

		_, err := SymmetricEncryptWithHmacIv(plaintext, key32)
		assert.Error(t, err)
	})
}

func TestSymmetricDecryptErrors(t *testing.T) {
	key32 := make([]byte, 32)
	key16 := make([]byte, 16)
	validEncrypted, _ := SymmetricEncrypt([]byte("data"), key32, nil)

	t.Run("BadKeyLength", func(t *testing.T) {
		_, err := SymmetricDecrypt(validEncrypted, key16, false)
		assert.ErrorContains(t, err, "key must be 32 bytes")
	})

	t.Run("InputTooShort", func(t *testing.T) {
		_, err := SymmetricDecrypt([]byte{1, 2, 3}, key32, false)
		assert.ErrorContains(t, err, "input too short")
	})

	t.Run("BadCiphertextLength", func(t *testing.T) {
		badLenInput := make([]byte, aes.BlockSize+5)
		_, err := SymmetricDecrypt(badLenInput, key32, false)
		assert.ErrorContains(t, err, "not a multiple of block size")
	})
}

func TestSymmetricDecryptECB(t *testing.T) {
	key := make([]byte, 32)
	plaintext := []byte("raw ecb payload!!")

	block, _ := aes.NewCipher(key)
	padded := pkcs7Pad(plaintext, aes.BlockSize)

	encrypted := make([]byte, len(padded))
	for i := 0; i < len(padded); i += aes.BlockSize {
		block.Encrypt(encrypted[i:i+aes.BlockSize], padded[i:i+aes.BlockSize])
	}

	decrypted, err := SymmetricDecryptECB(encrypted, key)
	require.NoError(t, err)
	assert.Equal(t, plaintext, decrypted)
}

func TestSymmetricDecryptECBErrors(t *testing.T) {
	key32 := make([]byte, 32)
	key16 := make([]byte, 16)

	t.Run("BadKeyLength", func(t *testing.T) {
		_, err := SymmetricDecryptECB(make([]byte, aes.BlockSize), key16)
		assert.ErrorContains(t, err, "key must be 32 bytes")
	})

	t.Run("BadInputLength", func(t *testing.T) {
		_, err := SymmetricDecryptECB([]byte{1, 2, 3, 4, 5}, key32)
		assert.ErrorContains(t, err, "not a multiple of block size")
	})
}

func TestMachineID(t *testing.T) {
	id := GenerateAccountMachineID("lemon4ksan")
	assert.NotEmpty(t, id)
	assert.Equal(t, uint8(0x00), id[0], "expected VDF map start 0x00")
	assert.Contains(t, string(id), "MessageObject")
}

func TestGenerateSessionKey_Error(t *testing.T) {
	hugeNonce := make([]byte, 500)
	_, _, err := GenerateSessionKey(hugeNonce)
	assert.Error(t, err, "expected error due to large nonce")
}

func TestSymmetricDecrypt_PaddingError(t *testing.T) {
	key := make([]byte, 32)
	input := make([]byte, 32)
	input[31] = 0x99 // Corrupt last byte to break PKCS7 check

	_, err := SymmetricDecrypt(input, key, false)
	assert.Error(t, err, "expected padding error")
}

func TestWipe(t *testing.T) {
	data := []byte{1, 2, 3, 4, 5}
	Wipe(data)
	assert.Equal(t, make([]byte, 5), data)
}

func TestMustParsePublicKey_Errors(t *testing.T) {
	t.Run("InvalidDERData", func(t *testing.T) {
		badDerPem := []byte("-----BEGIN PUBLIC KEY-----\naGVsbG8=\n-----END PUBLIC KEY-----")
		assert.Panics(t, func() { mustParsePublicKey(badDerPem) })
	})

	t.Run("NotRSAPublicKey", func(t *testing.T) {
		ecdsaPem := []byte(`-----BEGIN PUBLIC KEY-----
MFYwEAYHKoZIzj0CAQYFK4EEAAoDQgAEJXSGKQJn3ohXt4ibm5cl5h2D0JaLUQU0
rqFGN/I3MuPqEVKOTKxJfoYGYDvoSaYFlwW7DyjvQWMOkQ1XJsnWRg==
-----END PUBLIC KEY-----`)
		assert.Panics(t, func() { mustParsePublicKey(ecdsaPem) })
	})

	t.Run("InvalidPEMBlock", func(t *testing.T) {
		assert.Panics(t, func() { mustParsePublicKey([]byte("not a pem at all")) })
	})
}

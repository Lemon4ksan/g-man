// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package crypto provides cryptographic utilities for the Steam client.
//
// It implements Steam-specific security operations including RSA-OAEP session
// key generation, symmetric AES-256-CBC and AES-256-ECB encryption, and Steam
// Guard Mobile Authenticator TOTP algorithms.
//
// Common operations include:
//   - Generating temporary encrypted session keys using [GenerateSessionKey].
//   - Performing AES encryption and decryption using [SymmetricEncrypt] and [SymmetricDecrypt].
//   - Constructing deterministic hardware identifiers using [GenerateAccountMachineID].
//   - Generating 2FA authentication codes via [GenerateAuthCode].
package crypto

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha1"
	"crypto/x509"
	_ "embed"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
)

// Public key loaded from system.pem (RSA)
var pubKeySystem *rsa.PublicKey

//go:embed system.pem
var systemPem []byte

func init() {
	mustParsePublicKey(systemPem)
}

// GenerateSessionKey creates a 32-byte random session key, optionally appends a nonce,
// and encrypts it with the system public key using RSA-OAEP (SHA-1).
//
// If the nonce is provided, the function appends it to the generated session key
// before encrypting the combined buffer. An empty or nil nonce is ignored and
// only the session key is encrypted.
//
// It returns an error if the cryptographically secure random number generator
// fails, or if the RSA encryption fails.
func GenerateSessionKey(nonce []byte) (sessionKey, encrypted []byte, err error) {
	sessionKey = make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, sessionKey); err != nil {
		return nil, nil, err
	}

	toEncrypt := sessionKey
	if len(nonce) > 0 {
		toEncrypt = make([]byte, len(sessionKey)+len(nonce))
		copy(toEncrypt, sessionKey)
		copy(toEncrypt[len(sessionKey):], nonce)
	}

	encrypted, err = rsa.EncryptOAEP(sha1.New(), rand.Reader, pubKeySystem, toEncrypt, nil)
	if err != nil {
		return nil, nil, err
	}

	return sessionKey, encrypted, nil
}

// SymmetricEncrypt performs AES-256-CBC encryption on the input payload.
// The initialization vector (IV) is encrypted with AES-256-ECB and prepended
// to the resulting ciphertext.
//
// If the iv argument is nil, the function automatically generates a secure
// random vector of the standard block size.
//
// It returns an error if the provided key is not exactly 32 bytes, if the iv is
// non-nil but is not exactly 16 bytes, or if the secure random generator fails.
func SymmetricEncrypt(input, key, iv []byte) ([]byte, error) {
	if len(key) != 32 {
		return nil, errors.New("key must be 32 bytes for AES-256")
	}

	if iv == nil {
		iv = make([]byte, aes.BlockSize)
		if _, err := io.ReadFull(rand.Reader, iv); err != nil {
			return nil, err
		}
	} else if len(iv) != aes.BlockSize {
		return nil, errors.New("IV must be 16 bytes")
	}

	block, _ := aes.NewCipher(key)

	padded := pkcs7Pad(input, aes.BlockSize)
	cbcBytes := make([]byte, len(padded))
	cipher.NewCBCEncrypter(block, iv).CryptBlocks(cbcBytes, padded)

	ecbIV := make([]byte, aes.BlockSize, aes.BlockSize+len(cbcBytes))
	block.Encrypt(ecbIV, iv)

	return append(ecbIV, cbcBytes...), nil
}

// SymmetricEncryptWithHmacIv encrypts the input payload using a derived initialization vector.
// It constructs the IV from an HMAC-SHA1 of a random 3-byte prefix and the plaintext,
// using the first 16 bytes of the key as the HMAC secret.
//
// It returns an error if the key is not exactly 32 bytes, if the system random
// generator fails, or if the underlying [SymmetricEncrypt] fails.
func SymmetricEncryptWithHmacIv(input, key []byte) ([]byte, error) {
	if len(key) != 32 {
		return nil, errors.New("key must be 32 bytes")
	}

	var random [3]byte
	if _, err := io.ReadFull(rand.Reader, random[:]); err != nil {
		return nil, err
	}

	h := hmac.New(sha1.New, key[:16])
	h.Write(random[:])
	h.Write(input)

	var ivBuf [16]byte
	copy(ivBuf[:13], h.Sum(nil)[:13])
	copy(ivBuf[13:], random[:])

	return SymmetricEncrypt(input, key, ivBuf[:])
}

// SymmetricDecrypt decrypts ciphertext produced by [SymmetricEncrypt] or [SymmetricEncryptWithHmacIv].
//
// If checkHmac is true, the function verifies the integrity of the decrypted payload
// against the HMAC signature embedded within the initialization vector.
//
// It returns an error if the key is not exactly 32 bytes, if the input ciphertext is
// shorter than the block size, if the ciphertext length is not a multiple of the
// block size, if padding removal fails, or if HMAC verification fails.
func SymmetricDecrypt(input, key []byte, checkHmac bool) ([]byte, error) {
	if len(key) != 32 {
		return nil, errors.New("key must be 32 bytes")
	}

	if len(input) < aes.BlockSize {
		return nil, errors.New("input too short")
	}

	cbcBytes := input[aes.BlockSize:]
	if len(cbcBytes)%aes.BlockSize != 0 {
		return nil, errors.New("ciphertext length is not a multiple of block size")
	}

	iv := make([]byte, aes.BlockSize)
	padded := make([]byte, len(cbcBytes))

	block, _ := aes.NewCipher(key)
	block.Decrypt(iv, input[:aes.BlockSize])
	cipher.NewCBCDecrypter(block, iv).CryptBlocks(padded, cbcBytes)

	plaintext, err := pkcs7Unpad(padded, aes.BlockSize)
	if err != nil {
		return nil, err
	}

	if checkHmac {
		h := hmac.New(sha1.New, key[:16])
		h.Write(iv[13:])
		h.Write(plaintext)

		if !hmac.Equal(iv[:13], h.Sum(nil)[:13]) {
			return nil, errors.New("received invalid HMAC")
		}
	}

	return plaintext, nil
}

// SymmetricDecryptECB decrypts data encrypted with AES-256-ECB using PKCS7 padding.
//
// It returns an error if the key is not exactly 32 bytes, if the input ciphertext is
// not a multiple of the block size, or if padding removal fails.
func SymmetricDecryptECB(input, key []byte) ([]byte, error) {
	if len(key) != 32 {
		return nil, errors.New("key must be 32 bytes")
	}

	if len(input)%aes.BlockSize != 0 {
		return nil, errors.New("input length is not a multiple of block size")
	}

	block, _ := aes.NewCipher(key)

	plaintext := make([]byte, len(input))
	for i := 0; i < len(input); i += aes.BlockSize {
		block.Decrypt(plaintext[i:i+aes.BlockSize], input[i:i+aes.BlockSize])
	}

	return pkcs7Unpad(plaintext, aes.BlockSize)
}

// GenerateAccountMachineID creates a deterministic machine identifier based on the Steam account name.
// It uses predefined formatting strings and hashing algorithms to produce a consistent ID
// for recognized device identification.
func GenerateAccountMachineID(accountName string) []byte {
	val1 := "SteamUser Hash BB3 " + accountName
	val2 := "SteamUser Hash FF2 " + accountName
	val3 := "SteamUser Hash 3B3 " + accountName

	return CreateVDFMachineID(val1, val2, val3)
}

// CreateVDFMachineID packs three string hashes into the Valve VDF binary format.
// It serializes the identifiers into a structured, null-terminated VDF map structure
// that matches the Steam client hardware registration format.
func CreateVDFMachineID(v1, v2, v3 string) []byte {
	sha1Hex := func(s string) string {
		h := sha1.New()
		h.Write([]byte(s))

		return hex.EncodeToString(h.Sum(nil))
	}

	buf := new(bytes.Buffer)
	buf.WriteByte(0x00) // Type Map
	buf.WriteString("MessageObject")
	buf.WriteByte(0x00)

	fields := []string{"BB3", "FF2", "3B3"}
	vals := []string{v1, v2, v3}

	for i, field := range fields {
		buf.WriteByte(0x01) // Type String
		buf.WriteString(field)
		buf.WriteByte(0x00)
		buf.WriteString(sha1Hex(vals[i]))
		buf.WriteByte(0x00)
	}

	buf.Write([]byte{0x08, 0x08}) // End of maps

	return buf.Bytes()
}

// Wipe overwrites the given byte slice with zero bytes to clear sensitive data from memory.
func Wipe(b []byte) {
	for i := range b {
		b[i] = 0
	}
}

func pkcs7Pad(data []byte, blockSize int) []byte {
	padding := blockSize - (len(data) % blockSize)
	result := make([]byte, len(data)+padding)
	copy(result, data)

	p := byte(padding)
	for i := len(data); i < len(result); i++ {
		result[i] = p
	}

	return result
}

func pkcs7Unpad(data []byte, blockSize int) ([]byte, error) {
	if len(data) == 0 {
		return nil, errors.New("empty data")
	}

	if len(data)%blockSize != 0 {
		return nil, errors.New("data length is not a multiple of block size")
	}

	padding := int(data[len(data)-1])
	if padding == 0 || padding > blockSize {
		return nil, errors.New("invalid padding")
	}

	for i := range padding {
		if data[len(data)-1-i] != byte(padding) {
			return nil, errors.New("invalid padding")
		}
	}

	return data[:len(data)-padding], nil
}

func mustParsePublicKey(data []byte) {
	block, _ := pem.Decode(data)
	if block == nil || block.Type != "PUBLIC KEY" {
		panic("failed to decode PEM block containing public key")
	}

	pub, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		panic(fmt.Errorf("failed to parse public key: %w", err))
	}

	pubKeySystem = pub.(*rsa.PublicKey)
}

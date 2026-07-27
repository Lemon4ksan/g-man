// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

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

var (
	// ErrInvalidKeyLength is returned when an AES key is not exactly 32 bytes long.
	ErrInvalidKeyLength = errors.New("crypto: key must be 32 bytes for AES-256")
	// ErrInvalidIVLength is returned when a custom initialization vector is not 16 bytes long.
	ErrInvalidIVLength = errors.New("crypto: IV must be 16 bytes")
	// ErrInputTooShort is returned when ciphertext is shorter than the AES block size.
	ErrInputTooShort = errors.New("crypto: input payload too short")
	// ErrInvalidBlockSize is returned when ciphertext length is not a multiple of the AES block size.
	ErrInvalidBlockSize = errors.New("crypto: payload length is not a multiple of AES block size")
	// ErrInvalidHMAC is returned when HMAC verification fails during decryption.
	ErrInvalidHMAC = errors.New("crypto: received invalid HMAC signature")
	// ErrEmptyData is returned when unpadding an empty byte slice.
	ErrEmptyData = errors.New("crypto: empty data payload")
	// ErrInvalidPadding is returned when PKCS7 padding validation fails.
	ErrInvalidPadding = errors.New("crypto: invalid PKCS7 padding")
)

var pubKeySystem *rsa.PublicKey

//go:embed system.pem
var systemPem []byte

func init() {
	mustParsePublicKey(systemPem)
}

// GenerateSessionKey constructs a 32-byte random session key, appends an optional nonce, and encrypts it using Steam's RSA public key (OAEP SHA-1).
//
// Returns:
//   - sessionKey: Plaintext 32-byte symmetric AES key.
//   - encrypted: RSA-OAEP encrypted session payload ready for logon transmission.
//   - error: Non-nil if crypto/rand or RSA encryption fails.
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

// SymmetricEncrypt encrypts payload using AES-256-CBC.
// The initial IV is encrypted using AES-256-ECB and prepended to the resulting ciphertext stream.
// If iv is nil, a cryptographically random 16-byte vector is generated.
func SymmetricEncrypt(input, key, iv []byte) ([]byte, error) {
	if len(key) != 32 {
		return nil, ErrInvalidKeyLength
	}

	if iv == nil {
		iv = make([]byte, aes.BlockSize)
		if _, err := io.ReadFull(rand.Reader, iv); err != nil {
			return nil, err
		}
	} else if len(iv) != aes.BlockSize {
		return nil, ErrInvalidIVLength
	}

	block, _ := aes.NewCipher(key)

	padded := pkcs7Pad(input, aes.BlockSize)
	cbcBytes := make([]byte, len(padded))
	cipher.NewCBCEncrypter(block, iv).CryptBlocks(cbcBytes, padded)

	ecbIV := make([]byte, aes.BlockSize, aes.BlockSize+len(cbcBytes))
	block.Encrypt(ecbIV, iv)

	return append(ecbIV, cbcBytes...), nil
}

// SymmetricEncryptWithHmacIv encrypts payload using AES-256-CBC with a derived IV constructed from HMAC-SHA1 of a random 3-byte prefix and input plaintext.
func SymmetricEncryptWithHmacIv(input, key []byte) ([]byte, error) {
	if len(key) != 32 {
		return nil, ErrInvalidKeyLength
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

// SymmetricDecrypt decrypts AES-256-CBC ciphertext produced by SymmetricEncrypt or SymmetricEncryptWithHmacIv.
// If checkHmac is true, validates payload integrity against the HMAC signature embedded within the IV.
func SymmetricDecrypt(input, key []byte, checkHmac bool) ([]byte, error) {
	if len(key) != 32 {
		return nil, ErrInvalidKeyLength
	}

	if len(input) < aes.BlockSize {
		return nil, ErrInputTooShort
	}

	cbcBytes := input[aes.BlockSize:]
	if len(cbcBytes)%aes.BlockSize != 0 {
		return nil, ErrInvalidBlockSize
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
			return nil, ErrInvalidHMAC
		}
	}

	return plaintext, nil
}

// SymmetricDecryptECB decrypts AES-256-ECB encrypted data with PKCS7 padding removal.
func SymmetricDecryptECB(input, key []byte) ([]byte, error) {
	if len(key) != 32 {
		return nil, ErrInvalidKeyLength
	}

	if len(input)%aes.BlockSize != 0 {
		return nil, ErrInvalidBlockSize
	}

	block, _ := aes.NewCipher(key)

	plaintext := make([]byte, len(input))
	for i := 0; i < len(input); i += aes.BlockSize {
		block.Decrypt(plaintext[i:i+aes.BlockSize], input[i:i+aes.BlockSize])
	}

	return pkcs7Unpad(plaintext, aes.BlockSize)
}

// GenerateAccountMachineID generates a deterministic Valve VDF Machine ID bound to the specified account name.
func GenerateAccountMachineID(accountName string) []byte {
	val1 := "SteamUser Hash BB3 " + accountName
	val2 := "SteamUser Hash FF2 " + accountName
	val3 := "SteamUser Hash 3B3 " + accountName

	return CreateVDFMachineID(val1, val2, val3)
}

// CreateVDFMachineID packs three SHA1 string hashes into Valve's binary VDF map format for hardware registration.
func CreateVDFMachineID(v1, v2, v3 string) []byte {
	sha1Hex := func(s string) string {
		h := sha1.New()
		h.Write([]byte(s))

		return hex.EncodeToString(h.Sum(nil))
	}

	buf := new(bytes.Buffer)
	buf.WriteByte(0x00)
	buf.WriteString("MessageObject")
	buf.WriteByte(0x00)

	fields := []string{"BB3", "FF2", "3B3"}
	vals := []string{v1, v2, v3}

	for i, field := range fields {
		buf.WriteByte(0x01)
		buf.WriteString(field)
		buf.WriteByte(0x00)
		buf.WriteString(sha1Hex(vals[i]))
		buf.WriteByte(0x00)
	}

	buf.Write([]byte{0x08, 0x08})

	return buf.Bytes()
}

// Wipe overwrites sensitive byte slices with zeros in memory.
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
		return nil, ErrEmptyData
	}

	if len(data)%blockSize != 0 {
		return nil, ErrInvalidBlockSize
	}

	padding := int(data[len(data)-1])
	if padding == 0 || padding > blockSize {
		return nil, ErrInvalidPadding
	}

	for i := range padding {
		if data[len(data)-1-i] != byte(padding) {
			return nil, ErrInvalidPadding
		}
	}

	return data[:len(data)-padding], nil
}

func mustParsePublicKey(data []byte) {
	block, _ := pem.Decode(data)
	if block == nil || block.Type != "PUBLIC KEY" {
		panic("crypto: failed to decode PEM block containing public key")
	}

	pub, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		panic(fmt.Errorf("crypto: failed to parse public key: %w", err))
	}

	pubKeySystem = pub.(*rsa.PublicKey)
}

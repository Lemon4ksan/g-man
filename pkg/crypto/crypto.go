// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package crypto

import (
	"bytes"
	"crypto"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha1"
	"crypto/x509"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"fmt"
	"io"

	_ "embed"
)

// Public key loaded from system.pem (RSA)
var pubKeySystem *rsa.PublicKey

//go:embed system.pem
var systemPem []byte

func init() {
	// Read and parse the PEM file (expected to be in the same directory)
	block, _ := pem.Decode(systemPem)
	if block == nil || block.Type != "PUBLIC KEY" {
		panic("failed to decode PEM block containing public key")
	}
	pub, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		panic(fmt.Errorf("failed to parse public key: %w", err))
	}
	var ok bool
	pubKeySystem, ok = pub.(*rsa.PublicKey)
	if !ok {
		panic("public key is not RSA")
	}
}

// verifySignature verifies an RSA signature using the system public key.
// algorithm should be "RSA-SHA1" (or any hash supported by crypto/rsa).
// Currently only SHA1 is implemented; extend as needed.
func VerifySignature(data, sig []byte, algorithm string) (bool, error) {
	var hash []byte
	switch algorithm {
	case "RSA-SHA1", "":
		h := sha1.Sum(data)
		hash = h[:]
	default:
		return false, fmt.Errorf("unsupported algorithm: %s", algorithm)
	}
	err := rsa.VerifyPKCS1v15(pubKeySystem, crypto.SHA1, hash, sig)
	if err != nil {
		return false, err
	}
	return true, nil
}

// GenerateSessionKey creates a 32-byte random session key, optionally appends a nonce,
// and encrypts the concatenation with the system public key using RSA-OAEP (SHA1).
// Returns the plain session key and the encrypted blob.
func GenerateSessionKey(nonce []byte) (sessionKey, encrypted []byte, err error) {
	sessionKey = make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, sessionKey); err != nil {
		return nil, nil, err
	}
	toEncrypt := sessionKey
	if nonce != nil {
		toEncrypt = append(toEncrypt, nonce...)
	}
	hash := sha1.New()
	encrypted, err = rsa.EncryptOAEP(hash, rand.Reader, pubKeySystem, toEncrypt, nil)
	if err != nil {
		return nil, nil, err
	}
	return sessionKey, encrypted, nil
}

// symmetricEncrypt performs AES-256-CBC encryption with a random or provided IV.
// The IV itself is first encrypted with AES-256-ECB (no padding) and prepended.
// Input is padded to block size using PKCS7.
func SymmetricEncrypt(input, key, iv []byte) ([]byte, error) {
	if len(key) != 32 {
		return nil, errors.New("key must be 32 bytes for AES-256")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}

	// IV handling
	if iv == nil {
		iv = make([]byte, aes.BlockSize)
		if _, err := io.ReadFull(rand.Reader, iv); err != nil {
			return nil, err
		}
	} else if len(iv) != aes.BlockSize {
		return nil, errors.New("IV must be 16 bytes")
	}

	// Encrypt IV with ECB (no padding)
	ecbIV := make([]byte, aes.BlockSize)
	block.Encrypt(ecbIV, iv)

	// Pad input to block size with PKCS7
	paddedInput := pkcs7Pad(input, aes.BlockSize)

	// CBC encryption
	cbcCipher := cipher.NewCBCEncrypter(block, iv)
	ciphertext := make([]byte, len(paddedInput))
	cbcCipher.CryptBlocks(ciphertext, paddedInput)

	// Concatenate: encrypted IV + ciphertext
	result := append(ecbIV, ciphertext...)
	return result, nil
}

// SymmetricEncryptWithHmacIv is a custom encryption that constructs the IV from
// an HMAC-SHA1 of (random + plaintext) and a 3-byte random, using the first 16 bytes of the key for HMAC.
func SymmetricEncryptWithHmacIv(input, key []byte) ([]byte, error) {
	if len(key) != 32 {
		return nil, errors.New("key must be 32 bytes")
	}
	// Generate 3 random bytes
	random := make([]byte, 3)
	if _, err := io.ReadFull(rand.Reader, random); err != nil {
		return nil, err
	}

	// HMAC-SHA1 using first 16 bytes of key
	hmacKey := key[:16]
	h := hmac.New(sha1.New, hmacKey)
	h.Write(random)
	h.Write(input)
	partialHmac := h.Sum(nil)[:13] // take first 13 bytes (16-3)

	// Build IV: partialHmac (13 bytes) + random (3 bytes)
	iv := append(partialHmac, random...)

	return SymmetricEncrypt(input, key, iv)
}

// SymmetricDecrypt decrypts data produced by SymmetricEncrypt or SymmetricEncryptWithHmacIv.
// If checkHmac is true, it verifies the HMAC embedded in the IV.
func SymmetricDecrypt(input, key []byte, checkHmac bool) ([]byte, error) {
	if len(key) != 32 {
		return nil, errors.New("key must be 32 bytes")
	}
	if len(input) < aes.BlockSize {
		return nil, errors.New("input too short")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}

	// Decrypt first 16 bytes (encrypted IV) with ECB (no padding)
	encIV := input[:aes.BlockSize]
	iv := make([]byte, aes.BlockSize)
	block.Decrypt(iv, encIV)

	// CBC decrypt the rest
	ciphertext := input[aes.BlockSize:]
	if len(ciphertext)%aes.BlockSize != 0 {
		return nil, errors.New("ciphertext length is not a multiple of block size")
	}
	cbcCipher := cipher.NewCBCDecrypter(block, iv)
	plaintextPadded := make([]byte, len(ciphertext))
	cbcCipher.CryptBlocks(plaintextPadded, ciphertext)

	// Remove PKCS7 padding
	plaintext, err := pkcs7Unpad(plaintextPadded, aes.BlockSize)
	if err != nil {
		return nil, err
	}

	if checkHmac {
		// IV structure: first 13 bytes = partial HMAC, last 3 bytes = random
		if len(iv) != 16 {
			return nil, errors.New("invalid IV length")
		}
		partialHmac := iv[:13]
		random := iv[13:]

		hmacKey := key[:16]
		h := hmac.New(sha1.New, hmacKey)
		h.Write(random)
		h.Write(plaintext)
		expectedPartial := h.Sum(nil)[:13]
		if !hmac.Equal(partialHmac, expectedPartial) {
			return nil, errors.New("received invalid HMAC from remote host")
		}
	}

	return plaintext, nil
}

// SymmetricDecryptECB decrypts data that was encrypted with AES-256-ECB + PKCS7 padding.
func SymmetricDecryptECB(input, key []byte) ([]byte, error) {
	if len(key) != 32 {
		return nil, errors.New("key must be 32 bytes")
	}
	if len(input)%aes.BlockSize != 0 {
		return nil, errors.New("input length is not a multiple of block size")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}

	// ECB mode: decrypt block by block
	plaintext := make([]byte, len(input))
	for i := 0; i < len(input); i += aes.BlockSize {
		block.Decrypt(plaintext[i:i+aes.BlockSize], input[i:i+aes.BlockSize])
	}

	// Remove PKCS7 padding
	return pkcs7Unpad(plaintext, aes.BlockSize)
}

// GenerateRandomMachineID creates a completely random device ID.
func GenerateRandomMachineID() []byte {
	randStr := func() string {
		b := make([]byte, 16)
		rand.Read(b)
		return hex.EncodeToString(b)
	}
	return CreateVDFMachineID(randStr(), randStr(), randStr())
}

// GenerateAccountMachineID creates a deterministic ID based on the account name.
func GenerateAccountMachineID(accountName string) []byte {
	format := "SteamUser Hash %s %s"
	val1 := fmt.Sprintf(format, "BB3", accountName)
	val2 := fmt.Sprintf(format, "FF2", accountName)
	val3 := fmt.Sprintf(format, "3B3", accountName)
	return CreateVDFMachineID(val1, val2, val3)
}

// CreateVDFMachineID packs three hashes into the Valve VDF binary format.
func CreateVDFMachineID(v1, v2, v3 string) []byte {
	buf := new(bytes.Buffer)
	sha1Hex := func(s string) string {
		h := sha1.New()
		h.Write([]byte(s))
		return hex.EncodeToString(h.Sum(nil))
	}

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

func Wipe(b []byte) {
	for i := range b {
		b[i] = 0
	}
}

// pkcs7Pad adds PKCS7 padding to data to achieve the given block size.
func pkcs7Pad(data []byte, blockSize int) []byte {
	padding := blockSize - (len(data) % blockSize)
	result := make([]byte, len(data)+padding)
	copy(result, data)
	for i := len(data); i < len(result); i++ {
		result[i] = byte(padding)
	}
	return result
}

// pkcs7Unpad removes PKCS7 padding, verifying its correctness.
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

// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package crypto

import (
	"crypto/sha1"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"strconv"

	"github.com/lemon4ksan/g-man/internal/bytesconv"
)

const steamChars = "23456789BCDFGHJKMNPQRTVWXY"

func hmacSha1Stack(key, msg []byte) [20]byte {
	var kPad [64]byte
	if len(key) <= 64 {
		copy(kPad[:], key)
	} else {
		h := sha1.Sum(key)
		copy(kPad[:], h[:])
	}

	var ipad, opad [64]byte
	for i := 0; i < 64; i++ {
		ipad[i] = kPad[i] ^ 0x36
		opad[i] = kPad[i] ^ 0x5c
	}

	var innerBuf [128]byte
	copy(innerBuf[:64], ipad[:])
	copy(innerBuf[64:], msg)
	innerHash := sha1.Sum(innerBuf[:64+len(msg)])

	var outerBuf [84]byte
	copy(outerBuf[:64], opad[:])
	copy(outerBuf[64:], innerHash[:])

	return sha1.Sum(outerBuf[:])
}

// GenerateAuthCode generates a 5-digit Steam Guard two-factor authentication code for the given timestamp.
//
// The sharedSecret argument must be a base64-encoded or 40-character hex-encoded string.
//
// It returns an error if the shared secret cannot be decoded from base64 or hexadecimal.
func GenerateAuthCode(sharedSecret string, timestamp int64) (string, error) {
	var (
		secretBuf [20]byte
		secret    []byte
	)

	if len(sharedSecret) == 40 {
		n, err := hex.Decode(secretBuf[:], bytesconv.S2B(sharedSecret))
		if err != nil {
			return "", err
		}

		secret = secretBuf[:n]
	} else {
		s, err := base64.StdEncoding.DecodeString(sharedSecret)
		if err != nil {
			return "", err
		}

		secret = s
	}

	t := uint64(timestamp / 30)

	var msgBuf [8]byte
	binary.BigEndian.PutUint64(msgBuf[:], t)

	sum := hmacSha1Stack(secret, msgBuf[:])

	start := sum[19] & 0x0F
	fullCode := binary.BigEndian.Uint32(sum[start:start+4]) & 0x7FFFFFFF

	var code [5]byte
	for i := range 5 {
		code[i] = steamChars[fullCode%uint32(len(steamChars))]
		fullCode /= uint32(len(steamChars))
	}

	return string(code[:]), nil
}

// GenerateConfirmationKey generates a base64-encoded key required to confirm mobile actions.
//
// The identitySecret must be a base64-encoded or 40-character hex-encoded string.
// The tag parameter represents the action type (such as "conf", "allow", or "cancel").
// If the tag exceeds 32 bytes, only the first 32 bytes of the tag are used.
//
// It returns an error if the identity secret cannot be decoded from base64 or hexadecimal.
func GenerateConfirmationKey(identitySecret string, timestamp int64, tag string) (string, error) {
	var (
		secretBuf [20]byte
		secret    []byte
	)

	if len(identitySecret) == 40 {
		n, err := hex.Decode(secretBuf[:], bytesconv.S2B(identitySecret))
		if err != nil {
			return "", err
		}

		secret = secretBuf[:n]
	} else {
		s, err := base64.StdEncoding.DecodeString(identitySecret)
		if err != nil {
			return "", err
		}

		secret = s
	}

	dataLen := 8
	if len(tag) > 32 {
		dataLen += 32
	} else {
		dataLen += len(tag)
	}

	var buf [40]byte
	binary.BigEndian.PutUint64(buf[:8], uint64(timestamp))
	copy(buf[8:], bytesconv.S2B(tag))

	sum := hmacSha1Stack(secret, buf[:dataLen])

	return base64.StdEncoding.EncodeToString(sum[:]), nil
}

// GetDeviceID generates a unique, deterministic device identifier based on the SteamID.
// It returns a formatted UUID string with an "android:" prefix.
func GetDeviceID(steamID uint64) string {
	h := sha1.New()

	var idBuf [20]byte

	b := strconv.AppendUint(idBuf[:0], steamID, 10)
	h.Write(b)

	var hashBuf [20]byte

	sum := h.Sum(hashBuf[:0])

	var hexBuf [40]byte
	hex.Encode(hexBuf[:], sum)

	var result [44]byte
	copy(result[:8], "android:")
	copy(result[8:16], hexBuf[:8])
	result[16] = '-'
	copy(result[17:21], hexBuf[8:12])
	result[21] = '-'
	copy(result[22:26], hexBuf[12:16])
	result[26] = '-'
	copy(result[27:31], hexBuf[16:20])
	result[31] = '-'
	copy(result[32:44], hexBuf[20:32])

	return string(result[:])
}

func decodeSecret(secret string) ([]byte, error) {
	if len(secret) == 40 {
		return hex.DecodeString(secret)
	}

	return base64.StdEncoding.DecodeString(secret)
}

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
func GenerateAuthCode(secret []byte, timestamp int64) [5]byte {
	if len(secret) == 0 {
		return [5]byte{}
	}

	var keyBytes []byte
	if len(secret) == 20 {
		keyBytes = secret
	} else {
		var err error

		keyBytes, err = DecodeSecret(bytesconv.B2S(secret))
		if err != nil {
			return [5]byte{}
		}
	}

	if len(keyBytes) == 0 {
		return [5]byte{}
	}

	t := uint64(timestamp / 30)

	var msgBuf [8]byte
	binary.BigEndian.PutUint64(msgBuf[:], t)

	sum := hmacSha1Stack(keyBytes, msgBuf[:])

	start := sum[19] & 0x0F
	fullCode := binary.BigEndian.Uint32(sum[start:start+4]) & 0x7FFFFFFF

	var code [5]byte
	for i := range 5 {
		code[i] = steamChars[fullCode%uint32(len(steamChars))]
		fullCode /= uint32(len(steamChars))
	}

	return code
}

// GenerateConfirmationKey generates a base64-encoded key required to confirm mobile actions.
func GenerateConfirmationKey(secret []byte, timestamp int64, tag string) [28]byte {
	if len(secret) == 0 {
		return [28]byte{}
	}

	var keyBytes []byte
	if len(secret) == 20 {
		keyBytes = secret
	} else {
		var err error

		keyBytes, err = DecodeSecret(bytesconv.B2S(secret))
		if err != nil {
			return [28]byte{}
		}
	}

	if len(keyBytes) == 0 {
		return [28]byte{}
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

	sum := hmacSha1Stack(keyBytes, buf[:dataLen])

	var dst [28]byte
	base64.StdEncoding.Encode(dst[:], sum[:])

	return dst
}

// GetDeviceID generates a unique, deterministic device identifier based on the SteamID.
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

// DecodeSecret decodes a Steam TOTP or confirmation secret string.
// Supports 40-character Hex strings, standard Base64, and Raw/URL-safe Base64 formats.
func DecodeSecret(secret string) ([]byte, error) {
	if len(secret) == 0 {
		return nil, base64.CorruptInputError(0)
	}

	if len(secret) == 40 {
		if b, err := hex.DecodeString(secret); err == nil {
			return b, nil
		}
	}

	if b, err := base64.StdEncoding.DecodeString(secret); err == nil {
		return b, nil
	}

	if b, err := base64.RawStdEncoding.DecodeString(secret); err == nil {
		return b, nil
	}

	if b, err := base64.URLEncoding.DecodeString(secret); err == nil {
		return b, nil
	}

	return base64.RawURLEncoding.DecodeString(secret)
}

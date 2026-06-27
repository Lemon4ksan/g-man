// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package connector

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"

	"github.com/lemon4ksan/g-man/pkg/crypto"
)

// Magic are the 4 bytes that prefix every Steam TCP packet header.
const Magic = "VT01"

// SteamFramer implements network.Framer for Steam's custom TCP protocol.
// It handles length-prefixed message framing: [4-byte length][4-byte magic][payload].
type SteamFramer struct{}

// ReadFrame reads a frame from the given io.Reader using the Steam framer.
func (s SteamFramer) ReadFrame(r io.Reader) ([]byte, error) {
	var header [8]byte

	// Read the fixed-size header first.
	if _, err := io.ReadFull(r, header[:]); err != nil {
		return nil, err
	}

	if string(header[4:8]) != Magic {
		return nil, errors.New("steam framer: invalid magic bytes")
	}

	length := binary.LittleEndian.Uint32(header[0:4])
	if length > 10*1024*1024 { // 10MB sanity limit
		return nil, fmt.Errorf("steam framer: packet too large (%d bytes)", length)
	}

	// Read the variable-length payload.
	payload := make([]byte, length)
	if _, err := io.ReadFull(r, payload); err != nil {
		return nil, err
	}

	return payload, nil
}

// WriteFrame writes a frame to the given io.Writer using the Steam framer.
func (s SteamFramer) WriteFrame(w io.Writer, data []byte) error {
	if len(data) > 10*1024*1024 {
		return errors.New("steam framer: data exceeds maximum packet size")
	}

	var header [8]byte
	binary.LittleEndian.PutUint32(header[0:4], uint32(len(data)))
	copy(header[4:8], Magic)

	// If the writer supports gather writes (like net.Conn with net.Buffers), we can optimize.
	// Unfortunately, io.Writer doesn't support this directly.
	// We'll try to cast to net.Conn and use net.Buffers if possible.
	if conn, ok := w.(net.Conn); ok {
		buffers := net.Buffers{header[:], data}
		_, err := buffers.WriteTo(conn)
		return err
	}

	// Fallback for generic io.Writer
	if _, err := w.Write(header[:]); err != nil {
		return err
	}

	if _, err := w.Write(data); err != nil {
		return err
	}

	return nil
}

// SteamCipher implements network.Cipher for Steam's symmetric encryption (AES + HMAC).
type SteamCipher struct {
	sessionKey []byte
}

// NewSteamCipher creates a new SteamCipher with the given session key.
func NewSteamCipher(key []byte) *SteamCipher {
	return &SteamCipher{sessionKey: key}
}

// Encrypt encrypts the given data using the Steam cipher.
func (c *SteamCipher) Encrypt(data []byte) ([]byte, error) {
	return crypto.SymmetricEncryptWithHmacIv(data, c.sessionKey)
}

// Decrypt decrypts the given data using the Steam cipher.
func (c *SteamCipher) Decrypt(data []byte) ([]byte, error) {
	return crypto.SymmetricDecrypt(data, c.sessionKey, true)
}

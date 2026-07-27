// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package framer provides network framer implementations for use in g-man.
package framer

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"sync"

	"github.com/lemon4ksan/g-man/internal/crypto"
)

const (
	magic              = "VT01"
	magicUint32 uint32 = 0x31305456

	maxPooledCapacity = 128 * 1024
)

// FrameBuffer holds a byte slice for use in the framer pool.
type FrameBuffer struct {
	B []byte
}

// Equal implements testify equality check against []byte or another FrameBuffer.
func (fb *FrameBuffer) Equal(other any) bool {
	if fb == nil {
		return other == nil
	}

	switch v := other.(type) {
	case *FrameBuffer:
		if v == nil {
			return false
		}

		return bytes.Equal(fb.B, v.B)

	case []byte:
		return bytes.Equal(fb.B, v)
	}

	return false
}

var frameBufferPool = sync.Pool{
	New: func() any {
		return &FrameBuffer{
			B: make([]byte, 0, 64*1024),
		}
	},
}

// AcquireFrameBuffer acquires a FrameBuffer from the pool, resizing if necessary.
func AcquireFrameBuffer(length int) *FrameBuffer {
	fb := frameBufferPool.Get().(*FrameBuffer)
	if cap(fb.B) < length {
		fb.B = make([]byte, length)
	} else {
		fb.B = fb.B[:length]
	}

	return fb
}

// ReleaseFrameBuffer releases the given FrameBuffer back to the pool.
func ReleaseFrameBuffer(fb *FrameBuffer) {
	if fb == nil || cap(fb.B) > maxPooledCapacity {
		return
	}

	fb.B = fb.B[:0]
	frameBufferPool.Put(fb)
}

// SteamFramer implements network.Framer for Steam's custom TCP protocol.
type SteamFramer struct{}

// ReadFrame reads a length-prefixed frame from r.
func (s SteamFramer) ReadFrame(r io.Reader) (*FrameBuffer, error) {
	var header [8]byte
	if _, err := io.ReadFull(r, header[:]); err != nil {
		return nil, err
	}

	if binary.LittleEndian.Uint32(header[4:8]) != magicUint32 {
		return nil, errors.New("steam framer: invalid magic bytes")
	}

	length := binary.LittleEndian.Uint32(header[0:4])
	if length > 10*1024*1024 {
		return nil, fmt.Errorf("steam framer: packet too large (%d bytes)", length)
	}

	fb := AcquireFrameBuffer(int(length))
	if _, err := io.ReadFull(r, fb.B); err != nil {
		ReleaseFrameBuffer(fb)
		return nil, err
	}

	return fb, nil
}

// WriteFrame writes a frame to the given io.Writer using the Steam framer.
func (s SteamFramer) WriteFrame(w io.Writer, data []byte) error {
	if len(data) > 10*1024*1024 {
		return errors.New("steam framer: data exceeds maximum packet size")
	}

	var header [8]byte
	binary.LittleEndian.PutUint32(header[0:4], uint32(len(data)))
	copy(header[4:8], magic)

	if conn, ok := w.(net.Conn); ok {
		buffers := net.Buffers{header[:], data}
		_, err := buffers.WriteTo(conn)
		return err
	}

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
func (c *SteamCipher) Decrypt(fb *FrameBuffer) (*FrameBuffer, error) {
	if fb == nil || len(fb.B) == 0 {
		return nil, errors.New("steam cipher: empty frame buffer")
	}

	plaintext, err := crypto.SymmetricDecrypt(fb.B, c.sessionKey, true)
	if err != nil {
		return nil, err
	}

	decryptedFB := AcquireFrameBuffer(len(plaintext))
	copy(decryptedFB.B, plaintext)

	return decryptedFB, nil
}

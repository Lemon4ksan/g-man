// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package framer implements length-prefixed packet framing and symmetric encryption for Steam socket connections.
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

var (
	// ErrInvalidMagic is returned when a packet header lacks the expected "VT01" magic bytes.
	ErrInvalidMagic = errors.New("framer: invalid magic bytes")
	// ErrPacketTooLarge is returned when packet payload length exceeds maximum allowed bounds (10MB).
	ErrPacketTooLarge = errors.New("framer: packet exceeds maximum size limit")
	// ErrEmptyFrameBuffer is returned when attempting to decrypt an empty frame buffer.
	ErrEmptyFrameBuffer = errors.New("cipher: empty frame buffer")
)

// FrameBuffer encapsulates pooled memory buffers used during packet framing.
type FrameBuffer struct {
	B []byte
}

// Equal checks equality against raw byte slices or another FrameBuffer.
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

// AcquireFrameBuffer fetches a FrameBuffer from the global pool, resizing if necessary.
func AcquireFrameBuffer(length int) *FrameBuffer {
	fb := frameBufferPool.Get().(*FrameBuffer)
	if cap(fb.B) < length {
		fb.B = make([]byte, length)
	} else {
		fb.B = fb.B[:length]
	}

	return fb
}

// ReleaseFrameBuffer recycles a FrameBuffer back to the pool if its capacity is within safe memory limits.
func ReleaseFrameBuffer(fb *FrameBuffer) {
	if fb == nil || cap(fb.B) > maxPooledCapacity {
		return
	}

	fb.B = fb.B[:0]
	frameBufferPool.Put(fb)
}

// SteamFramer handles length-prefixed packet framing over raw TCP streams.
type SteamFramer struct{}

// ReadFrame reads a "VT01" length-prefixed packet from the reader.
func (s SteamFramer) ReadFrame(r io.Reader) (*FrameBuffer, error) {
	var header [8]byte
	if _, err := io.ReadFull(r, header[:]); err != nil {
		return nil, err
	}

	if binary.LittleEndian.Uint32(header[4:8]) != magicUint32 {
		return nil, ErrInvalidMagic
	}

	length := binary.LittleEndian.Uint32(header[0:4])
	if length > 10*1024*1024 {
		return nil, fmt.Errorf("%w (%d bytes)", ErrPacketTooLarge, length)
	}

	fb := AcquireFrameBuffer(int(length))
	if _, err := io.ReadFull(r, fb.B); err != nil {
		ReleaseFrameBuffer(fb)
		return nil, err
	}

	return fb, nil
}

// WriteFrame writes data prepended with length and "VT01" magic header to writer.
func (s SteamFramer) WriteFrame(w io.Writer, data []byte) error {
	if len(data) > 10*1024*1024 {
		return ErrPacketTooLarge
	}

	var header [8]byte
	binary.LittleEndian.PutUint32(header[0:4], uint32(len(data)))
	copy(header[4:8], magic)

	if conn, ok := w.(net.Conn); ok {
		fb := AcquireFrameBuffer(8 + len(data))
		defer ReleaseFrameBuffer(fb)

		copy(fb.B[0:8], header[:])
		copy(fb.B[8:], data)

		_, err := conn.Write(fb.B)

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

// SteamCipher manages AES symmetric encryption and HMAC verification over framed packets.
type SteamCipher struct {
	sessionKey []byte
}

// NewSteamCipher constructs a SteamCipher instance using sessionKey.
func NewSteamCipher(key []byte) *SteamCipher {
	return &SteamCipher{sessionKey: key}
}

// Encrypt encrypts data using AES-256-CBC with derived HMAC IV.
func (c *SteamCipher) Encrypt(data []byte) ([]byte, error) {
	return crypto.SymmetricEncryptWithHmacIv(data, c.sessionKey)
}

// Decrypt decrypts ciphertext inside fb using sessionKey and validates HMAC integrity.
func (c *SteamCipher) Decrypt(fb *FrameBuffer) (*FrameBuffer, error) {
	if fb == nil || len(fb.B) == 0 {
		return nil, ErrEmptyFrameBuffer
	}

	plaintext, err := crypto.SymmetricDecrypt(fb.B, c.sessionKey, true)
	if err != nil {
		return nil, err
	}

	decryptedFB := AcquireFrameBuffer(len(plaintext))
	copy(decryptedFB.B, plaintext)

	return decryptedFB, nil
}

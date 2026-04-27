// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package api

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"strconv"
	"unicode/utf16"
)

const (
	kvTypeNone         uint8 = 0
	kvTypeString       uint8 = 1
	kvTypeInt32        uint8 = 2
	kvTypeFloat32      uint8 = 3
	kvTypePointer      uint8 = 4
	kvTypeWideString   uint8 = 5
	kvTypeColor        uint8 = 6
	kvTypeUInt64       uint8 = 7
	kvTypeEnd          uint8 = 8
	kvTypeInt64        uint8 = 10
	kvTypeAlternateEnd uint8 = 11
)

type bvdfParser struct {
	data   []byte
	offset int
}

func (p *bvdfParser) parse() (any, error) {
	obj := make(map[string]any)

	for {
		if p.offset >= len(p.data) {
			return nil, errors.New("api: unexpected EOF in binary vdf")
		}

		kind := p.data[p.offset]
		p.offset++

		if kind == kvTypeEnd || kind == kvTypeAlternateEnd {
			break
		}

		name, err := p.readCString()
		if err != nil {
			return nil, err
		}

		if kind == kvTypeNone && name == "" && len(obj) == 0 {
			name, err = p.readCString()
			if err != nil {
				return nil, err
			}
		}

		var value any
		switch kind {
		case kvTypeNone:
			value, err = p.parse()
		case kvTypeString:
			value, err = p.readCString()
		case kvTypeWideString:
			value, err = p.readWideString()
		case kvTypeInt32, kvTypeColor, kvTypePointer:
			value, err = p.readInt32()
		case kvTypeUInt64:
			value, err = p.readUint64()
		case kvTypeInt64:
			value, err = p.readInt64()
		case kvTypeFloat32:
			value, err = p.readFloat32()
		default:
			return nil, fmt.Errorf("api: unknown binary vdf type %d at offset %d", kind, p.offset)
		}

		if err != nil {
			return nil, err
		}

		if name != "" {
			obj[name] = value
		}
	}

	return convertToSliceIfNeeded(obj), nil
}

func (p *bvdfParser) readCString() (string, error) {
	end := bytes.IndexByte(p.data[p.offset:], 0)
	if end == -1 {
		return "", errors.New("api: unterminated c-string in binary vdf")
	}

	str := string(p.data[p.offset : p.offset+end])
	p.offset += end + 1

	return str, nil
}

func (p *bvdfParser) readWideString() (string, error) {
	var u16s []uint16

	for {
		if p.offset+2 > len(p.data) {
			return "", errors.New("api: eof reading wide string")
		}

		code := binary.LittleEndian.Uint16(p.data[p.offset:])
		p.offset += 2

		if code == 0 {
			break
		}

		u16s = append(u16s, code)
	}

	return string(utf16.Decode(u16s)), nil
}

func (p *bvdfParser) readInt32() (int32, error) {
	if p.offset+4 > len(p.data) {
		return 0, errors.New("api: eof reading int32")
	}

	val := binary.LittleEndian.Uint32(p.data[p.offset:])
	p.offset += 4

	return int32(val), nil
}

func (p *bvdfParser) readUint64() (uint64, error) {
	if p.offset+8 > len(p.data) {
		return 0, errors.New("api: eof reading uint64")
	}

	val := binary.LittleEndian.Uint64(p.data[p.offset:])
	p.offset += 8

	return val, nil
}

func (p *bvdfParser) readInt64() (int64, error) {
	if p.offset+8 > len(p.data) {
		return 0, errors.New("api: eof reading int64")
	}

	val := binary.LittleEndian.Uint64(p.data[p.offset:])
	p.offset += 8

	return int64(val), nil
}

func (p *bvdfParser) readFloat32() (float32, error) {
	if p.offset+4 > len(p.data) {
		return 0, errors.New("api: eof reading float32")
	}

	bits := binary.LittleEndian.Uint32(p.data[p.offset:])
	p.offset += 4

	return math.Float32frombits(bits), nil
}

func convertToSliceIfNeeded(obj map[string]any) any {
	if len(obj) == 0 {
		return obj
	}

	for i := range len(obj) {
		if _, ok := obj[strconv.Itoa(i)]; !ok {
			return obj
		}
	}

	res := make([]any, len(obj))
	for i := range len(obj) {
		res[i] = obj[strconv.Itoa(i)]
	}

	return res
}

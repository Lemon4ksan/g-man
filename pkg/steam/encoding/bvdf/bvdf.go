// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package bvdf provides tools to parse and unmarshal Valve Binary KeyValues (VDF) format files.
package bvdf

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"maps"
	"math"
	"strconv"
	"unicode/utf16"

	"github.com/mitchellh/mapstructure"
)

const (
	kvTypeNone       uint8 = 0
	kvTypeString     uint8 = 1
	kvTypeInt32      uint8 = 2
	kvTypeFloat32    uint8 = 3
	kvTypePointer    uint8 = 4
	kvTypeWideString uint8 = 5
	kvTypeColor      uint8 = 6
	kvTypeUInt64     uint8 = 7
	kvTypeEnd        uint8 = 8
	kvTypeEndAlt     uint8 = 11
	kvTypeInt64      uint8 = 13
)

const (
	// AppInfoMagic40 represents the magic signature of Steam appinfo.vdf version 40.
	AppInfoMagic40 = 0x07564428
	// AppInfoMagic41 represents the magic signature of Steam appinfo.vdf version 41.
	AppInfoMagic41 = 0x07564429
	// PackageInfoMagic39 represents the magic signature of Steam packageinfo.vdf version 39.
	PackageInfoMagic39 = 0x06565527
	// PackageInfoMagic40 represents the magic signature of Steam packageinfo.vdf version 40.
	PackageInfoMagic40 = 0x06565528
	// PackageInfoMagicBase represents the common base magic signature for packageinfo.vdf formats.
	PackageInfoMagicBase = 0x065655
)

// ErrFormat is returned when the parser encounters invalid binary structures or signatures.
var ErrFormat = errors.New("binary vdf: format error")

// Parser holds the active state of a Valve Binary KeyValues (VDF) decoder.
// Direct use of this struct is typically discouraged. Users should call
// [Unmarshal], [UnmarshalOffset], or [Parse] to perform decoding.
type Parser struct {
	data        []byte
	offset      int
	stringTable []string
}

// Unmarshal decodes Valve Binary KeyValues from a reader and stores the result in a target.
// It returns [ErrFormat] if the payload structure is invalid, or decoder errors if mapstructure decoding fails.
// The target argument must be a non-nil pointer to a map or a struct.
// If the reader is nil or empty, Unmarshal returns a read error or [ErrFormat].
func Unmarshal(r io.Reader, target any) error {
	data, err := io.ReadAll(r)
	if err != nil {
		return err
	}

	p := &Parser{data: data, offset: 0}

	res, err := p.parse()
	if err != nil {
		return err
	}

	parsed, ok := res.(map[string]any)
	if !ok {
		return fmt.Errorf("%w: root of binary vdf is not an object", ErrFormat)
	}

	config := &mapstructure.DecoderConfig{
		WeaklyTypedInput: true,
		Result:           target,
		Squash:           true,
	}

	decoder, err := mapstructure.NewDecoder(config)
	if err != nil {
		return err
	}

	return decoder.Decode(parsed)
}

// UnmarshalOffset decodes Valve Binary KeyValues starting from the specified index.
// It updates the offset pointer with the new byte boundary position upon successful decoding.
// It returns [ErrFormat] or structure mapping errors if the data is corrupted.
// Both data and target must be non-nil. The offset pointer must not be nil or it will panic.
func UnmarshalOffset(data []byte, offset *int, target any) error {
	p := &Parser{data: data, offset: *offset}

	res, err := p.parse()
	if err != nil {
		return fmt.Errorf("%w: %w", ErrFormat, err)
	}

	*offset = p.offset

	parsed, ok := res.(map[string]any)
	if !ok {
		return fmt.Errorf("%w: root of binary vdf is not an object", ErrFormat)
	}

	config := &mapstructure.DecoderConfig{
		WeaklyTypedInput: true,
		Result:           target,
		Squash:           true,
	}

	decoder, err := mapstructure.NewDecoder(config)
	if err != nil {
		return err
	}

	return decoder.Decode(parsed)
}

// Parse detects the specific Valve Binary KeyValues format and decodes it.
// It recognizes appinfo and packageinfo file headers and routes parsing accordingly.
// It returns [ErrFormat] or specific file format validation errors if parsing fails.
// If the data slice is nil or empty, Parse returns an unexpected end of file error.
func Parse(data []byte) (any, error) {
	if len(data) >= 4 {
		magic := binary.LittleEndian.Uint32(data[0:4])
		if magic == AppInfoMagic40 || magic == AppInfoMagic41 {
			return ParseAppInfo(data)
		}

		magicBase := magic >> 8
		if magicBase == PackageInfoMagicBase {
			version := magic & 0xFF
			if version == 39 || version == 40 {
				return ParsePackageInfo(data)
			}
		}
	}

	p := &Parser{
		data:   data,
		offset: 0,
	}

	return p.parse()
}

// ParseAppInfo decodes Steam appinfo.vdf binary data into Go structures.
// It expects data to match [AppInfoMagic40] or [AppInfoMagic41] magic signatures.
// It returns formatting errors if headers are too short or if individual app entries are corrupted.
// The data slice must contain a valid appinfo header and payload.
func ParseAppInfo(data []byte) (any, error) {
	if len(data) < 16 {
		return nil, errors.New("bvdf: appinfo header too short")
	}

	magic := binary.LittleEndian.Uint32(data[0:4])
	universe := binary.LittleEndian.Uint32(data[4:8])

	var (
		stringTableOffset uint64
		appsEndOffset     int
		restOffset        int
	)

	switch magic {
	case AppInfoMagic40:
		appsEndOffset = len(data)
		restOffset = 8
	case AppInfoMagic41:
		stringTableOffset = binary.LittleEndian.Uint64(data[8:16])
		appsEndOffset = int(stringTableOffset)
		restOffset = 16
	default:
		return nil, fmt.Errorf("bvdf: invalid appinfo magic 0x%x", magic)
	}

	var (
		stringTable []string
		err         error
	)

	if magic == AppInfoMagic41 {
		if appsEndOffset > len(data) {
			return nil, errors.New("bvdf: string table offset beyond data length")
		}

		stringTable, err = parseStringTable(data[appsEndOffset:])
		if err != nil {
			return nil, err
		}
	}

	obj := make(map[string]any)
	offset := restOffset

	for offset < appsEndOffset && offset+4 <= appsEndOffset {
		appID := binary.LittleEndian.Uint32(data[offset : offset+4])
		if appID == 0 {
			break
		}

		if offset+68 > appsEndOffset {
			return nil, errors.New("bvdf: unexpected EOF reading app entry header")
		}

		size := binary.LittleEndian.Uint32(data[offset+4 : offset+8])
		if size < 60 {
			return nil, fmt.Errorf("bvdf: invalid app entry size %d (less than 60)", size)
		}

		vdfSize := int(size) - 60
		vdfDataStart := offset + 68
		vdfDataEnd := vdfDataStart + vdfSize

		if vdfDataEnd > appsEndOffset {
			return nil, errors.New("bvdf: unexpected EOF reading app VDF data")
		}

		vdfBytes := data[vdfDataStart:vdfDataEnd]

		appParser := &Parser{
			data:        vdfBytes,
			offset:      0,
			stringTable: stringTable,
		}

		appObj, err := appParser.parse()
		if err != nil {
			return nil, err
		}

		obj[strconv.FormatUint(uint64(appID), 10)] = appObj
		offset = vdfDataEnd
	}

	rootKey := fmt.Sprintf("appinfo_universe_%d", universe)

	return map[string]any{
		rootKey: obj,
	}, nil
}

// ParsePackageInfo decodes Steam packageinfo.vdf binary data into Go structures.
// It expects data to match [PackageInfoMagicBase] and a supported version like version 39 or 40.
// It returns formatting errors if headers are too short or if package metadata parsing fails.
// The data slice must contain a valid packageinfo header and payload.
func ParsePackageInfo(data []byte) (any, error) {
	if len(data) < 8 {
		return nil, errors.New("bvdf: packageinfo header too short")
	}

	magic := binary.LittleEndian.Uint32(data[0:4])
	universe := binary.LittleEndian.Uint32(data[4:8])

	version := magic & 0xFF
	magicBase := magic >> 8

	if magicBase != PackageInfoMagicBase {
		return nil, fmt.Errorf("bvdf: invalid packageinfo magic base 0x%x", magicBase)
	}

	if version != 39 && version != 40 {
		return nil, fmt.Errorf("bvdf: invalid packageinfo version %d", version)
	}

	hasToken := version >= 40

	var headerSize int
	if hasToken {
		headerSize = 36 // PACKAGEINFO_ENTRY_HEADER_SIZE_V40 (ID + Hash + ChangeNumber + Token)
	} else {
		headerSize = 28 // PACKAGEINFO_ENTRY_HEADER_SIZE_V39 (ID + Hash + ChangeNumber)
	}

	offset := 8
	obj := make(map[string]any)

	for offset < len(data) && offset+4 <= len(data) {
		packageID := binary.LittleEndian.Uint32(data[offset : offset+4])
		if packageID == 0xFFFFFFFF {
			break
		}

		if offset+headerSize > len(data) {
			return nil, errors.New("bvdf: unexpected EOF reading package entry header")
		}

		changeNumberOffset := offset + 24
		changeNumber := binary.LittleEndian.Uint32(data[changeNumberOffset : changeNumberOffset+4])

		pkgParser := &Parser{
			data:   data,
			offset: offset + headerSize,
		}

		pkgObjVal, err := pkgParser.parse()
		if err != nil {
			return nil, err
		}

		packageWithMeta := make(map[string]any)
		packageWithMeta["packageid"] = int32(packageID)
		packageWithMeta["change_number"] = uint64(changeNumber)

		hashBytes := data[offset+4 : offset+24]
		packageWithMeta["sha1"] = hex.EncodeToString(hashBytes)

		if pkgObj, ok := pkgObjVal.(map[string]any); ok {
			maps.Copy(packageWithMeta, pkgObj)
		} else if pkgSlice, ok := pkgObjVal.([]any); ok {
			for idx, v := range pkgSlice {
				packageWithMeta[strconv.Itoa(idx)] = v
			}
		}

		obj[strconv.FormatUint(uint64(packageID), 10)] = packageWithMeta
		offset = pkgParser.offset
	}

	rootKey := fmt.Sprintf("packageinfo_universe_%d", universe)

	return map[string]any{
		rootKey: obj,
	}, nil
}

func parseStringTable(data []byte) ([]string, error) {
	if len(data) < 4 {
		return nil, errors.New("bvdf: string table too short")
	}

	count := binary.LittleEndian.Uint32(data[0:4])
	offset := 4

	strings := make([]string, 0, count)
	for range count {
		if offset >= len(data) {
			return nil, errors.New("bvdf: unexpected EOF reading string table")
		}

		end := bytes.IndexByte(data[offset:], 0)
		if end == -1 {
			return nil, errors.New("bvdf: unterminated string in string table")
		}

		str := string(data[offset : offset+end])
		strings = append(strings, str)
		offset += end + 1
	}

	return strings, nil
}

func (p *Parser) parse() (any, error) {
	obj := make(map[string]any)

	for {
		if err := p.ensureBytes(1, "unexpected EOF"); err != nil {
			return nil, err
		}

		kvType := p.data[p.offset]
		p.offset++

		if isEndMarker(kvType) {
			break
		}

		name, err := p.readKey()
		if err != nil {
			return nil, err
		}

		if p.isHeaderWithEmptyName(kvType, name, len(obj)) {
			name, err = p.readKey()
			if err != nil {
				return nil, err
			}
		}

		value, err := p.parseValue(kvType)
		if err != nil {
			return nil, err
		}

		if name != "" {
			obj[name] = value
		}
	}

	return convertToSliceIfNeeded(obj), nil
}

func (p *Parser) parseValue(kvType uint8) (any, error) {
	switch kvType {
	case kvTypeNone:
		return p.parse()
	case kvTypeString:
		return p.readCString()
	case kvTypeWideString:
		return p.readWideString()
	case kvTypeInt32, kvTypeColor, kvTypePointer:
		return p.readInt32()
	case kvTypeUInt64:
		return p.readUint64()
	case kvTypeInt64:
		return p.readInt64()
	case kvTypeFloat32:
		return p.readFloat32()
	default:
		return nil, fmt.Errorf("unknown type %d at offset %d", kvType, p.offset)
	}
}

func (p *Parser) ensureBytes(needed int, errMsg string) error {
	if p.offset+needed > len(p.data) {
		return errors.New("bvdf: " + errMsg)
	}

	return nil
}

func isEndMarker(kvType uint8) bool {
	return kvType == kvTypeEnd || kvType == kvTypeEndAlt
}

func (p *Parser) isHeaderWithEmptyName(kvType uint8, name string, objSize int) bool {
	if kvType == kvTypeNone && name == "" && objSize == 0 {
		if p.offset < len(p.data) {
			nextByte := p.data[p.offset]
			if nextByte < 14 {
				return false
			}
		}

		return true
	}

	return false
}

func (p *Parser) readKey() (string, error) {
	if len(p.stringTable) > 0 {
		if err := p.ensureBytes(4, "eof reading string table index"); err != nil {
			return "", err
		}

		index := binary.LittleEndian.Uint32(p.data[p.offset:])

		p.offset += 4
		if index >= uint32(len(p.stringTable)) {
			return "", fmt.Errorf("bvdf: invalid string table index %d (size %d)", index, len(p.stringTable))
		}

		return p.stringTable[index], nil
	}

	return p.readCString()
}

func (p *Parser) readCString() (string, error) {
	end := bytes.IndexByte(p.data[p.offset:], 0)
	if end == -1 {
		return "", errors.New("unterminated c-string")
	}

	str := string(p.data[p.offset : p.offset+end])
	p.offset += end + 1

	return str, nil
}

func (p *Parser) readWideString() (string, error) {
	var u16s []uint16

	for {
		if err := p.ensureBytes(2, "eof reading wide string"); err != nil {
			return "", err
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

func (p *Parser) readInt32() (int32, error) {
	if err := p.ensureBytes(4, "eof reading int32"); err != nil {
		return 0, err
	}

	val := binary.LittleEndian.Uint32(p.data[p.offset:])
	p.offset += 4

	return int32(val), nil
}

func (p *Parser) readUint64() (uint64, error) {
	if err := p.ensureBytes(8, "eof reading uint64"); err != nil {
		return 0, err
	}

	val := binary.LittleEndian.Uint64(p.data[p.offset:])
	p.offset += 8

	return val, nil
}

func (p *Parser) readInt64() (int64, error) {
	if err := p.ensureBytes(8, "eof reading int64"); err != nil {
		return 0, err
	}

	val := binary.LittleEndian.Uint64(p.data[p.offset:])
	p.offset += 8

	return int64(val), nil
}

func (p *Parser) readFloat32() (float32, error) {
	if err := p.ensureBytes(4, "eof reading float32"); err != nil {
		return 0, err
	}

	bits := binary.LittleEndian.Uint32(p.data[p.offset:])
	p.offset += 4

	return math.Float32frombits(bits), nil
}

func convertToSliceIfNeeded(obj map[string]any) any {
	if len(obj) == 0 {
		return obj
	}

	for index := range len(obj) {
		key := strconv.Itoa(index)
		if _, exists := obj[key]; !exists {
			return obj
		}
	}

	slice := make([]any, len(obj))
	for index := range len(obj) {
		key := strconv.Itoa(index)
		slice[index] = obj[key]
	}

	return slice
}

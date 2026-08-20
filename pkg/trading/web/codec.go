// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package web

import (
	"bytes"
	"errors"
	"fmt"
	"sync"

	json "github.com/goccy/go-json"

	"github.com/lemon4ksan/foundation/silicon/bytesconv"
	"github.com/lemon4ksan/g-man/pkg/trading"
)

var (
	// ErrInvalidJSONObjectBrace indicates JSON is missing opening brace.
	ErrInvalidJSONObjectBrace = errors.New("invalid json object: missing opening brace")
	// ErrInvalidJSONObjectEOF indicates unexpected EOF while scanning JSON.
	ErrInvalidJSONObjectEOF = errors.New("invalid json object: unexpected EOF")
	// ErrInvalidJSONObjectKey indicates invalid key string.
	ErrInvalidJSONObjectKey = errors.New("invalid json object: expected key string")
	// ErrInvalidJSONObjectKeyTerm indicates unterminated key string.
	ErrInvalidJSONObjectKeyTerm = errors.New("invalid json object: unterminated key string")
	// ErrInvalidJSONObjectColon indicates missing colon after key.
	ErrInvalidJSONObjectColon = errors.New("invalid json object: expected colon")
	// ErrInvalidJSONObjectNoKeys indicates no numeric keys were found.
	ErrInvalidJSONObjectNoKeys = errors.New("invalid json object: no valid numeric keys found")
)

var flexBufPool = sync.Pool{
	New: func() any {
		return new(bytes.Buffer)
	},
}

func scanJSONObjectElements(data []byte) ([][]byte, error) {
	i := 0
	n := len(data)

	for i < n && data[i] != '{' {
		i++
	}

	if i >= n {
		return nil, ErrInvalidJSONObjectBrace
	}

	i++

	var (
		stackBuf [32][]byte
		elements = stackBuf[:0]
		maxIdx   = -1
	)

	for i < n {
		for i < n && (data[i] == ' ' || data[i] == '\t' || data[i] == '\r' || data[i] == '\n' || data[i] == ',') {
			i++
		}

		if i >= n {
			return nil, ErrInvalidJSONObjectEOF
		}

		if data[i] == '}' {
			break
		}

		if data[i] != '"' {
			return nil, ErrInvalidJSONObjectKey
		}

		i++

		keyStart := i
		for i < n && data[i] != '"' {
			i++
		}

		if i >= n {
			return nil, ErrInvalidJSONObjectKeyTerm
		}

		keyBytes := data[keyStart:i]
		i++

		idxVal, ok := bytesconv.ParseUintFast(keyBytes)
		idx := uint64(idxVal)
		if !ok {
			for i < n && data[i] != ':' {
				i++
			}

			if i < n {
				i++
			}

			i = skipJSONValue(data, i)

			continue
		}

		for i < n && data[i] != ':' {
			i++
		}

		if i >= n {
			return nil, ErrInvalidJSONObjectColon
		}

		i++

		for i < n && (data[i] == ' ' || data[i] == '\t' || data[i] == '\r' || data[i] == '\n') {
			i++
		}

		valStart := i
		i = skipJSONValue(data, i)
		valEnd := i

		if valEnd > valStart {
			idxInt := int(idx)
			if idxInt > maxIdx {
				maxIdx = idxInt
			}

			if idxInt >= len(elements) {
				newLen := idxInt + 1
				if cap(elements) < newLen {
					newCap := max(cap(elements) * 2, newLen)

					newElems := make([][]byte, newLen, newCap)
					copy(newElems, elements)
					elements = newElems
				} else {
					elements = elements[:newLen]
				}
			}

			elements[idxInt] = data[valStart:valEnd]
		}
	}

	if maxIdx < 0 {
		return nil, ErrInvalidJSONObjectNoKeys
	}

	return elements[:maxIdx+1], nil
}

func skipJSONValue(data []byte, i int) int {
	n := len(data)
	if i >= n {
		return i
	}

	depth := 0
	inString := false
	escaped := false

	for i < n {
		c := data[i]

		if escaped {
			escaped = false
			i++

			continue
		}

		if c == '\\' && inString {
			escaped = true
			i++

			continue
		}

		if c == '"' {
			inString = !inString

			i++
			if !inString && depth == 0 {
				return i
			}

			continue
		}

		if inString {
			i++
			continue
		}

		switch c {
		case '{', '[':
			depth++
		case '}', ']':
			depth--
			if depth == 0 {
				return i + 1
			}
		case ',', ' ', '\t', '\r', '\n':
			if depth == 0 {
				return i
			}
		}

		i++
	}

	return i
}

func unmarshalFlexibleArray[T any](data []byte) ([]T, error) {
	data = bytes.TrimSpace(data)
	if len(data) == 0 {
		return nil, nil
	}

	switch data[0] {
	case '"':
		return nil, nil

	case '[':
		var arr []T
		if err := json.Unmarshal(data, &arr); err != nil {
			return nil, err
		}

		return arr, nil

	case '{':
		rawElements, err := scanJSONObjectElements(data)
		if err != nil {
			return nil, err
		}

		if len(rawElements) == 0 {
			return nil, nil
		}

		buf := flexBufPool.Get().(*bytes.Buffer)
		buf.Reset()

		defer flexBufPool.Put(buf)

		buf.WriteByte('[')

		for i, raw := range rawElements {
			if i > 0 {
				buf.WriteByte(',')
			}

			if len(raw) > 0 {
				buf.Write(raw)
			} else {
				buf.WriteString("null")
			}
		}

		buf.WriteByte(']')

		var res []T
		if err := json.Unmarshal(buf.Bytes(), &res); err != nil {
			return nil, err
		}

		return res, nil

	default:
		return nil, fmt.Errorf("failed to unmarshal flexible array: %s", string(data))
	}
}

type flexibleDescriptions []trading.Description

func (fd *flexibleDescriptions) UnmarshalJSON(data []byte) error {
	res, err := unmarshalFlexibleArray[trading.Description](data)
	if err != nil {
		return err
	}

	*fd = res

	return nil
}

type flexibleTags []assetClassTag

func (ft *flexibleTags) UnmarshalJSON(data []byte) error {
	res, err := unmarshalFlexibleArray[assetClassTag](data)
	if err != nil {
		return err
	}

	*ft = res

	return nil
}

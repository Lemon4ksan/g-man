// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package rest

import (
	"errors"
	"fmt"
	"net/url"
	"reflect"
	"strconv"
	"strings"
	"time"
)

// Uint64String handles uint64 values that are sent as strings in JSON.
// It also handles raw numbers, null, and empty strings.
type Uint64String uint64

func (u *Uint64String) UnmarshalJSON(b []byte) error {
	s := strings.Trim(string(b), `"`)
	if s == "" || s == "null" {
		*u = 0
		return nil
	}

	val, err := strconv.ParseUint(s, 10, 64)
	if err != nil {
		return fmt.Errorf("Uint64String: %w", err)
	}

	*u = Uint64String(val)

	return nil
}

// Int64String handles int64 values that are sent as strings in JSON.
// It also handles raw numbers, null, and empty strings.
type Int64String int64

func (i *Int64String) UnmarshalJSON(b []byte) error {
	s := strings.Trim(string(b), `"`)
	if s == "" || s == "null" {
		*i = 0
		return nil
	}

	val, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return fmt.Errorf("Int64String: %w", err)
	}

	*i = Int64String(val)

	return nil
}

// Float64String handles float64 values that are sent as strings in JSON.
type Float64String float64

func (f *Float64String) UnmarshalJSON(b []byte) error {
	s := strings.Trim(string(b), `"`)
	if s == "" || s == "null" {
		*f = 0
		return nil
	}

	val, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return fmt.Errorf("Float64String: %w", err)
	}

	*f = Float64String(val)

	return nil
}

// BoolInt handles booleans that Steam sends as 1 (true) or 0 (false).
// It also handles string variations ("1", "0", "true", "false").
type BoolInt bool

func (bi *BoolInt) UnmarshalJSON(b []byte) error {
	s := strings.ToLower(strings.Trim(string(b), `"`))
	switch s {
	case "1", "true":
		*bi = true
	case "0", "false", "", "null":
		*bi = false
	default:
		// Attempt to parse any other number; non-zero is true
		val, err := strconv.Atoi(s)
		if err == nil {
			*bi = val != 0
			return nil
		}

		*bi = false
	}

	return nil
}

// Timestamp handles Unix timestamps that Steam sends as strings or numbers.
type Timestamp time.Time

func (t *Timestamp) UnmarshalJSON(b []byte) error {
	s := strings.Trim(string(b), `"`)
	if s == "" || s == "null" || s == "0" {
		*t = Timestamp(time.Time{})
		return nil
	}

	unix, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return fmt.Errorf("Timestamp: %w", err)
	}

	*t = Timestamp(time.Unix(unix, 0).UTC())

	return nil
}

// Time returns the underlying time.Time.
func (t *Timestamp) Time() time.Time {
	return time.Time(*t)
}

// StructToValues converts a struct into url.Values using "url" tags.
// It supports string, int, uint, bool, and float types.
//
// Example:
//
//	type Params struct {
//		baseParams // Anonymous embedding
//		SteamID uint64 `url:"steamid"`
//		Count   int    `url:"count,omitempty"`
//		Extra      struct {
//			Internal string `url:"internal"`
//		} `url:",inline"` // Explicit inline
//	}
//	v, _ := rest.StructToValues(Params{SteamID: 7656119...})
func StructToValues(s any) (url.Values, error) {
	if s == nil {
		return nil, nil
	}

	if vals, ok := s.(url.Values); ok {
		return vals, nil
	}

	v := reflect.ValueOf(s)
	if v.Kind() == reflect.Pointer {
		v = v.Elem()
	}

	if v.Kind() != reflect.Struct {
		return nil, errors.New("unsupported type: input must be a struct or a pointer to a struct")
	}

	values := make(url.Values)
	if err := fillValues(v, values); err != nil {
		return nil, err
	}

	return values, nil
}

func fillValues(v reflect.Value, values url.Values) error {
	t := v.Type()
	for i := range v.NumField() {
		field := t.Field(i)
		fieldValue := v.Field(i)

		tag := field.Tag.Get("url")
		parts := strings.Split(tag, ",")
		key := parts[0]

		// Determine if this field should be inlined
		isInline := false
		for _, opt := range parts[1:] {
			if opt == "inline" {
				isInline = true
				break
			}
		}

		// Handle embedded (anonymous) structs or explicit inline tag
		if (field.Anonymous || isInline) && fieldValue.Kind() == reflect.Struct {
			if err := fillValues(fieldValue, values); err != nil {
				return err
			}

			continue
		}

		if tag == "" || tag == "-" {
			continue
		}

		omitempty := len(parts) > 1 && parts[1] == "omitempty"
		if omitempty && fieldValue.IsZero() {
			continue
		}

		// Handle Slices/Arrays
		if fieldValue.Kind() == reflect.Slice || fieldValue.Kind() == reflect.Array {
			for j := 0; j < fieldValue.Len(); j++ {
				strValue, err := toString(fieldValue.Index(j))
				if err != nil {
					return fmt.Errorf("field %s[%d]: %w", field.Name, j, err)
				}

				values.Add(key, strValue)
			}

			continue
		}

		// Handle Scalars
		strValue, err := toString(fieldValue)
		if err != nil {
			return fmt.Errorf("field %s: %w", field.Name, err)
		}

		values.Set(key, strValue)
	}

	return nil
}

func toString(v reflect.Value) (string, error) {
	// Check if the type implements fmt.Stringer (useful for custom types like id.ID)
	if v.CanInterface() {
		if s, ok := v.Interface().(interface{ String() string }); ok {
			return s.String(), nil
		}
	}

	switch v.Kind() {
	case reflect.String:
		return v.String(), nil
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return strconv.FormatInt(v.Int(), 10), nil
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return strconv.FormatUint(v.Uint(), 10), nil
	case reflect.Bool:
		return strconv.FormatBool(v.Bool()), nil
	case reflect.Float32, reflect.Float64:
		return strconv.FormatFloat(v.Float(), 'f', -1, 64), nil
	default:
		return "", fmt.Errorf("unsupported type: %s", v.Kind())
	}
}

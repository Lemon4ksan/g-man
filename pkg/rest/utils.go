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
)

// StructToValues converts a struct into url.Values using "url" tags.
// It supports string, int, uint, bool, and float types.
//
// Example:
//
//	type Params struct {
//	    SteamID uint64 `url:"steamid"`
//	    Count   int    `url:"count,omitempty"`
//	}
//	v, _ := rest.StructToValues(Params{SteamID: 7656119...})
func StructToValues(s any) (url.Values, error) {
	if s == nil {
		return nil, nil
	}
	if vals, ok := s.(url.Values); ok {
		return vals, nil
	}

	values := make(url.Values)
	v := reflect.ValueOf(s)

	if v.Kind() == reflect.Pointer {
		v = v.Elem()
	}

	if v.Kind() != reflect.Struct {
		return nil, errors.New("input must be a struct or a pointer to a struct")
	}

	t := v.Type()
	for i := range v.NumField() {
		field := t.Field(i)
		tag := field.Tag.Get("url")
		if tag == "" || tag == "-" {
			continue
		}

		parts := strings.Split(tag, ",")
		key := parts[0]
		omitempty := len(parts) > 1 && parts[1] == "omitempty"

		fieldValue := v.Field(i)
		if omitempty && fieldValue.IsZero() {
			continue
		}

		var strValue string
		switch fieldValue.Kind() {
		case reflect.String:
			strValue = fieldValue.String()
		case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
			strValue = strconv.FormatInt(fieldValue.Int(), 10)
		case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
			strValue = strconv.FormatUint(fieldValue.Uint(), 10)
		case reflect.Bool:
			strValue = strconv.FormatBool(fieldValue.Bool())
		case reflect.Float32, reflect.Float64:
			strValue = strconv.FormatFloat(fieldValue.Float(), 'f', -1, 64)
		default:
			return nil, fmt.Errorf("unsupported type for field %s: %s", field.Name, fieldValue.Kind())
		}
		values.Set(key, strValue)
	}
	return values, nil
}

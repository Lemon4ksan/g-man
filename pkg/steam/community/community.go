// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package community provides high-level helpers for HTTP interactions with steamcommunity.com.
package community

import (
	"context"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/lemon4ksan/aoni"
	"github.com/lemon4ksan/aoni/codec/values"
	"github.com/lemon4ksan/aoni/mod"
	"github.com/lemon4ksan/aoni/request"

	"github.com/lemon4ksan/g-man/pkg/steam/community/client"
	"github.com/lemon4ksan/g-man/pkg/steam/encoding"
)

const BaseURL = client.BaseURL

type Requester = client.Requester

type SessionProvider = client.SessionProvider

var NewClient = client.New

// FastFormEncoder allows DTO structs to serialize into form strings without map allocations.
type FastFormEncoder interface {
	EncodeFormString() (string, error)
}

// Decorate wraps a Requester with additional default request modifiers.
func Decorate(r Requester, mods ...aoni.RequestModifier) Requester {
	if len(mods) == 0 {
		return r
	}

	return &decoratedRequester{
		Requester:   r,
		defaultMods: mods,
	}
}

// GetTo executes a GET request and unmarshals JSON response data into Resp.
func GetTo[Resp any](
	ctx context.Context,
	r Requester,
	path string,
	mods ...aoni.RequestModifier,
) (*Resp, error) {
	mods = append([]aoni.RequestModifier{
		mod.WithDecoder(encoding.SteamJSONDecoder),
		mod.WithAccept("application/json, text/javascript; q=0.01"),
		mod.WithHeader("X-Requested-With", "XMLHttpRequest"),
	}, mods...)

	return request.GetTo[Resp](ctx, r, path, mods...)
}

// GetHTML executes a GET request and returns an HTML body stream.
func GetHTML(ctx context.Context, r Requester, path string, mods ...aoni.RequestModifier) (io.ReadCloser, error) {
	mods = append([]aoni.RequestModifier{
		mod.WithAccept("text/html,application/xhtml+xml,application/xml;q=0.9,image/avif,image/webp,*/*;q=0.8"),
	}, mods...)

	resp, err := r.Request(ctx, http.MethodGet, path, mods...)
	if err != nil {
		return nil, err
	}

	return resp.Body, nil
}

// PostTo executes a POST request with JSON payload.
func PostTo[Resp any](
	ctx context.Context,
	r Requester,
	path string,
	body any,
	mods ...aoni.RequestModifier,
) (*Resp, error) {
	var query url.Values
	if sid := r.SessionID(BaseURL); sid != "" {
		query = url.Values{"sessionid": {sid}}
	}

	mods = append([]aoni.RequestModifier{
		mod.WithDecoder(encoding.SteamJSONDecoder),
		mod.WithQuery(query),
		mod.WithAccept("application/json"),
		mod.WithContentType("application/json; charset=UTF-8"),
	}, mods...)

	return request.PostTo[Resp](ctx, r, path, body, mods...)
}

// PostFormTo executes a POST request with URL-encoded form data.
func PostFormTo[Resp any](
	ctx context.Context,
	r Requester,
	path string,
	body any,
	mods ...aoni.RequestModifier,
) (*Resp, error) {
	var rawForm string

	if encoder, ok := body.(FastFormEncoder); ok {
		var err error

		rawForm, err = encoder.EncodeFormString()
		if err != nil {
			return nil, err
		}

		if sid := r.SessionID(BaseURL); sid != "" && !urlValuesHasKey(rawForm, "sessionid") {
			if rawForm != "" {
				rawForm += "&sessionid=" + url.QueryEscape(sid)
			} else {
				rawForm = "sessionid=" + url.QueryEscape(sid)
			}
		}
	} else {
		var params url.Values
		if body != nil {
			var err error

			params, err = values.StructToValues(body)
			if err != nil {
				return nil, err
			}
		} else {
			params = make(url.Values)
		}

		if params.Get("sessionid") == "" {
			params.Set("sessionid", r.SessionID(BaseURL))
		}

		rawForm = params.Encode()
	}

	mods = append([]aoni.RequestModifier{
		mod.WithDecoder(encoding.SteamJSONDecoder),
		mod.WithBodyBytes([]byte(rawForm)),
		mod.WithAccept("application/json, text/javascript; q=0.01"),
		mod.WithContentType("application/x-www-form-urlencoded; charset=UTF-8"),
	}, mods...)

	return request.PostTo[Resp](ctx, r, path, nil, mods...)
}

func urlValuesHasKey(formStr, key string) bool {
	return strings.Contains(formStr, key+"=")
}

type decoratedRequester struct {
	Requester
	defaultMods []aoni.RequestModifier
}

func (d *decoratedRequester) Request(
	ctx context.Context,
	method, path string,
	mods ...aoni.RequestModifier,
) (*http.Response, error) {
	return d.Requester.Request(ctx, method, path, append(d.defaultMods, mods...)...)
}

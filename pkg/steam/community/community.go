// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package community provides a high-level client and helpers for interacting with the Steam Community website.
package community

import (
	"context"
	"io"
	"net/http"
	"net/url"

	"github.com/lemon4ksan/aoni"
	"github.com/lemon4ksan/aoni/codec/values"
	"github.com/lemon4ksan/aoni/mod"
	"github.com/lemon4ksan/aoni/request"

	"github.com/lemon4ksan/g-man/pkg/steam/community/client"
	"github.com/lemon4ksan/g-man/pkg/steam/encoding"
)

// BaseURL is the default base URL for Steam Community requests, mapped from [client.BaseURL].
const BaseURL = client.BaseURL

// Requester defines the requirements for executing Steam Community requests.
type Requester = client.Requester

// SessionProvider defines how to retrieve active Steam session identifiers.
type SessionProvider = client.SessionProvider

// NewClient creates a new [Requester] instance using the constructor from [client.New].
var NewClient = client.New

// FastFormEncoder allows DTO structs to serialize directly into URL-encoded strings with zero map allocations.
type FastFormEncoder interface {
	EncodeFormString() (string, error)
}

// Decorate wraps an existing [Requester] to append default global request modifiers to every request.
func Decorate(r Requester, mods ...aoni.RequestModifier) Requester {
	if len(mods) == 0 {
		return r
	}

	return &decoratedRequester{
		Requester:   r,
		defaultMods: mods,
	}
}

// GetTo executes a GET request and decodes the response body into a new [Resp] instance.
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

// GetHTML executes a GET request optimized for raw HTML content.
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

// PostTo executes a POST request with a JSON-encoded body and decodes the response into a new [Resp] instance.
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

// PostFormTo executes a POST request with URL-encoded form data and decodes the response into a new [Resp] instance.
// It uses FastFormEncoder if implemented by body to eliminate url.Values map allocations.
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
	return url.Values{}.Has(key)
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

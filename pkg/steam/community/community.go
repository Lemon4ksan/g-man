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

	"github.com/lemon4ksan/g-man/pkg/steam/community/client"
	"github.com/lemon4ksan/g-man/pkg/steam/encoding"
)

// BaseURL is the default base URL for Steam Community requests, mapped from [client.BaseURL].
const BaseURL = client.BaseURL

// Requester defines the requirements for executing Steam Community requests.
// It is a type alias for [client.Requester]. Use [NewClient] to instantiate a default client,
// or [Decorate] to wrap an existing requester with default request modifiers.
type Requester = client.Requester

// SessionProvider defines how to retrieve active Steam session identifiers.
// It is a type alias for [client.SessionProvider]. Typically implemented by components
// that manage user authentication states.
type SessionProvider = client.SessionProvider

// NewClient creates a new [Requester] instance using the constructor from [client.New].
var NewClient = client.New

// Decorate wraps an existing [Requester] to append default global request modifiers to every request.
// It returns the original requester unchanged if the slice of modifiers is empty or nil.
// If the original requester is nil, the decorated wrapper will panic upon calling its methods.
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
// It injects Steam Community-specific request headers and uses [encoding.SteamJSONDecoder] for decoding.
// It returns network, decoding, or Steam-specific response validation errors.
// It will panic if the provided [Requester] is nil.
func GetTo[Resp any](
	ctx context.Context,
	r Requester,
	path string,
	mods ...aoni.RequestModifier,
) (*Resp, error) {
	mods = append([]aoni.RequestModifier{
		aoni.WithDecoder(encoding.SteamJSONDecoder),
		aoni.WithAccept("application/json, text/javascript; q=0.01"),
		aoni.WithHeader("X-Requested-With", "XMLHttpRequest"),
	}, mods...)

	return aoni.GetTo[Resp](ctx, r, path, mods...)
}

// GetHTML executes a GET request optimized for raw HTML content.
// The caller is responsible for closing the returned [io.ReadCloser] to prevent resource leaks.
// It returns network errors or Steam-specific response errors encountered during the request.
// It will panic if the provided [Requester] is nil.
func GetHTML(ctx context.Context, r Requester, path string, mods ...aoni.RequestModifier) (io.ReadCloser, error) {
	mods = append([]aoni.RequestModifier{
		aoni.WithAccept("text/html,application/xhtml+xml,application/xml;q=0.9,image/avif,image/webp,*/*;q=0.8"),
	}, mods...)

	resp, err := r.Request(ctx, http.MethodGet, path, mods...)
	if err != nil {
		return nil, err
	}

	return resp.Body, nil
}

// PostTo executes a POST request with a JSON-encoded body and decodes the response into a new [Resp] instance.
// It injects the active session identifier as a "sessionid" URL query parameter.
// It uses [encoding.SteamJSONDecoder] for decoding the response.
//
// To send other body formats, pre-serialize the payload and pass it as an [io.Reader] (e.g. [strings.NewReader]),
// then override the Content-Type header using [WithContentType].
// To decode other response formats (such as XML or YAML), pass a decoder modifier, e.g. [WithXMLDecoder] or [WithYAMLDecoder].
//
// Use [PostFormTo] instead of passing [aoni.WithFormBody] or [aoni.WithFormValues] request modifiers.
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
		aoni.WithDecoder(encoding.SteamJSONDecoder),
		aoni.WithQuery(query),
		aoni.WithAccept("application/json"),
		aoni.WithContentType("application/json; charset=UTF-8"),
	}, mods...)

	return aoni.PostTo[Resp](ctx, r, path, body, mods...)
}

// PostFormTo executes a POST request with URL-encoded form data and decodes the response into a new [Resp] instance.
// It automatically serializes reqMsg to form values using [aoni.StructToValues] and injects
// the active session identifier as a "sessionid" form field if not already present.
// It uses [encoding.SteamJSONDecoder] for decoding the response.
//
// This helper doesn't accept [io.Reader] body.
// Use [aoni.PostTo] with [aoni.WithFormBody] or [aoni.WithFormValues] for that.
func PostFormTo[Resp any](
	ctx context.Context,
	r Requester,
	path string,
	body any,
	mods ...aoni.RequestModifier,
) (*Resp, error) {
	var params url.Values

	if body != nil {
		var err error

		params, err = aoni.StructToValues(body)
		if err != nil {
			return nil, err
		}
	} else {
		params = make(url.Values)
	}

	if params.Get("sessionid") == "" {
		params.Set("sessionid", r.SessionID(BaseURL))
	}

	mods = append([]aoni.RequestModifier{
		aoni.WithDecoder(encoding.SteamJSONDecoder),
		aoni.WithFormValues(params),
		aoni.WithAccept("application/json, text/javascript; q=0.01"),
		aoni.WithContentType("application/x-www-form-urlencoded; charset=UTF-8"),
	}, mods...)

	return aoni.PostTo[Resp](ctx, r, path, nil, mods...)
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

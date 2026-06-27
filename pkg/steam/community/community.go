// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package community

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"

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

// Get executes a GET request and decodes the resulting JSON response into a new [Resp] instance.
// It automatically configures the request headers and uses the [encoding.SteamJSONDecoder] for decoding.
// It passes reqMsg as URL query parameters if it is not nil.
// It returns network, decoding, or Steam-specific response validation errors.
// It will panic if the provided [Requester] is nil.
func Get[Resp any](
	ctx context.Context,
	r Requester,
	path string,
	reqMsg any,
	mods ...aoni.RequestModifier,
) (*Resp, error) {
	var allMods []aoni.RequestModifier

	allMods = append(allMods, aoni.WithDecoder(encoding.SteamJSONDecoder))
	allMods = append(allMods, aoni.WithAccept("application/json, text/javascript; q=0.01"))
	allMods = append(allMods, aoni.WithHeader("X-Requested-With", "XMLHttpRequest"))

	if reqMsg != nil {
		allMods = append(allMods, aoni.WithQuery(reqMsg))
	}

	allMods = append(allMods, mods...)

	return aoni.GetJSON[Resp](ctx, r, path, allMods...)
}

// GetHTML executes a GET request optimized for raw HTML content.
// The caller is responsible for closing the returned [io.ReadCloser] to prevent resource leaks.
// It returns network errors or Steam-specific response errors encountered during the request.
// It will panic if the provided [Requester] is nil.
func GetHTML(ctx context.Context, r Requester, path string, mods ...aoni.RequestModifier) (io.ReadCloser, error) {
	allMods := make([]aoni.RequestModifier, 0, 1+len(mods))
	allMods = append(
		allMods,
		aoni.WithAccept("text/html,application/xhtml+xml,application/xml;q=0.9,image/avif,image/webp,*/*;q=0.8"),
	)
	allMods = append(allMods, mods...)

	resp, err := r.Request(ctx, http.MethodGet, path, allMods...)
	if err != nil {
		return nil, err
	}

	return resp.Body, nil
}

// PostForm executes a POST request containing URL-encoded form data.
// It automatically encodes the reqMsg argument as form parameters and injects the session identifier from [Requester.SessionID].
// If reqMsg is nil, it initializes and sends an empty set of form parameters with the session identifier.
// It returns network, parsing, or decoding errors, and decodes the JSON response using [encoding.SteamJSONDecoder].
// It will panic if the provided [Requester] is nil.
func PostForm[Resp any](
	ctx context.Context,
	r Requester,
	path string,
	reqMsg any,
	mods ...aoni.RequestModifier,
) (*Resp, error) {
	var params url.Values

	if reqMsg != nil {
		var err error

		params, err = aoni.StructToValues(reqMsg)
		if err != nil {
			return nil, err
		}
	} else {
		params = make(url.Values)
	}

	if params.Get("sessionid") == "" {
		params.Set("sessionid", r.SessionID(BaseURL))
	}

	allMods := make([]aoni.RequestModifier, 0, 3+len(mods))
	allMods = append(allMods, aoni.WithContentType("application/x-www-form-urlencoded; charset=UTF-8"))
	allMods = append(allMods, aoni.WithAccept("application/json, text/javascript; q=0.01"))
	allMods = append(allMods, aoni.WithDecoder(encoding.SteamJSONDecoder))
	allMods = append(allMods, mods...)

	return aoni.PostForm[Resp](ctx, r, path, strings.NewReader(params.Encode()), allMods...)
}

// PostJSON executes a POST request containing a JSON-encoded body.
// It automatically serializes reqMsg to JSON and injects the session identifier as a "sessionid" URL query parameter.
// If reqMsg is nil, the request is executed with an empty body.
// It returns network, encoding, or decoding errors, and decodes the JSON response using [encoding.SteamJSONDecoder].
// It will panic if the provided [Requester] is nil.
func PostJSON[Resp any](
	ctx context.Context,
	r Requester,
	path string,
	reqMsg any,
	mods ...aoni.RequestModifier,
) (*Resp, error) {
	var query url.Values
	if sid := r.SessionID(BaseURL); sid != "" {
		query = url.Values{"sessionid": {sid}}
	}

	var bodyBytes []byte
	if reqMsg != nil {
		var err error

		bodyBytes, err = json.Marshal(reqMsg)
		if err != nil {
			return nil, err
		}
	}

	allMods := make([]aoni.RequestModifier, 0, 4+len(mods))
	allMods = append(allMods, aoni.WithQuery(query))
	allMods = append(allMods, aoni.WithContentType("application/json; charset=UTF-8"))
	allMods = append(allMods, aoni.WithAccept("application/json"))
	allMods = append(allMods, aoni.WithDecoder(encoding.SteamJSONDecoder))
	allMods = append(allMods, mods...)

	return aoni.PostJSON[Resp](ctx, r, path, bytes.NewReader(bodyBytes), allMods...)
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
	allMods := make([]aoni.RequestModifier, 0, len(d.defaultMods)+len(mods))
	allMods = append(allMods, d.defaultMods...)
	allMods = append(allMods, mods...)

	return d.Requester.Request(ctx, method, path, allMods...)
}

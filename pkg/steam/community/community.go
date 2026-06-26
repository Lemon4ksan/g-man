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

// BaseURL is the base url for community requests.
const BaseURL = client.BaseURL

// Requester defines the requirements for making Community requests.
type Requester = client.Requester

// NewClient creates a new Community Client.
var NewClient = client.New

// WithREST sets the REST client for the Community Client.
var WithREST = client.WithREST

// WithLogger sets the logger for the Community Client.
var WithLogger = client.WithLogger

// Decorate wraps an existing Requester and adds global request modifiers.
func Decorate(r Requester, mods ...aoni.RequestModifier) Requester {
	if len(mods) == 0 {
		return r
	}

	return &decoratedRequester{
		Requester:   r,
		defaultMods: mods,
	}
}

// Get performs a GET request and unmarshals the resulting JSON into the Resp type.
//
// If the reqMsg argument is nil, query parameters are omitted.
func Get[Resp any](
	ctx context.Context,
	r Requester,
	path string,
	reqMsg any,
	mods ...aoni.RequestModifier,
) (*Resp, error) {
	if reqMsg != nil {
		mods = append([]aoni.RequestModifier{aoni.WithQuery(reqMsg)}, mods...)
	}

	mods = append([]aoni.RequestModifier{
		aoni.WithAccept("application/json, text/javascript; q=0.01"),
		aoni.WithHeader("X-Requested-With", "XMLHttpRequest"),
	}, mods...)

	return Execute[Resp](ctx, r, http.MethodGet, path, mods...)
}

// GetHTML performs a GET request specifically for raw HTML content.
//
// If the reqMsg argument is nil, query parameters are omitted.
func GetHTML(ctx context.Context, r Requester, path string, mods ...aoni.RequestModifier) (io.ReadCloser, error) {
	mods = append([]aoni.RequestModifier{
		aoni.WithAccept("text/html,application/xhtml+xml,application/xml;q=0.9,image/avif,image/webp,*/*;q=0.8"),
	}, mods...)

	resp, err := r.Request(ctx, http.MethodGet, path, mods...)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	return resp.Body, nil
}

// PostForm performs a POST request with application/x-www-form-urlencoded data.
// It automatically injects the "sessionid" into the form parameters.
//
// If the reqMsg argument is nil, form parameters are initialized containing only the session ID.
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

	mods = append([]aoni.RequestModifier{
		aoni.WithBody(strings.NewReader(params.Encode())),
		aoni.WithContentType("application/x-www-form-urlencoded; charset=UTF-8"),
		aoni.WithAccept("application/json, text/javascript; q=0.01"),
	}, mods...)

	return Execute[Resp](ctx, r, http.MethodPost, path, mods...)
}

// PostJSON performs a POST request with a JSON body.
// It automatically injects the "sessionid" into the URL query parameters.
//
// If the reqMsg argument is nil, the request payload is omitted.
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

	if len(query) > 0 {
		mods = append([]aoni.RequestModifier{aoni.WithQuery(query)}, mods...)
	}

	if reqMsg != nil {
		bodyBytes, err := json.Marshal(reqMsg)
		if err != nil {
			return nil, err
		}

		mods = append([]aoni.RequestModifier{aoni.WithBody(bytes.NewReader(bodyBytes))}, mods...)
	}

	mods = append([]aoni.RequestModifier{
		aoni.WithContentType("application/json; charset=UTF-8"),
		aoni.WithAccept("application/json"),
	}, mods...)

	return Execute[Resp](ctx, r, http.MethodPost, path, mods...)
}

// Execute sends an HTTP request and decodes the response into a struct of type Resp.
func Execute[Resp any](
	ctx context.Context,
	r Requester,
	method, path string,
	mods ...aoni.RequestModifier,
) (*Resp, error) {
	resp, err := r.Request(ctx, method, path, mods...)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var zero Resp
	if _, ok := any(&zero).(*[]byte); ok {
		raw, err := io.ReadAll(resp.Body)
		if err != nil {
			return nil, err
		}

		p := any(&raw).(*Resp)

		return p, nil
	}

	result := new(Resp)
	if err := encoding.SteamJSONDecoder.Decode(resp.Body, result); err != nil {
		return nil, err
	}

	return result, nil
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

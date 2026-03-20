// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

/*
Package api provides a specialized framework for interacting with Steam Web APIs.
It bridges the gap between raw network transport and domain-specific logic by
handling Steam's inconsistent response formats and parameter requirements.

# Core Features

  - Multiple Format Support: Seamlessly handles JSON, VDF (KeyValues), and Protobuf.
  - Automatic Unwrapping: Steam's Web API often wraps results in a top-level "response"
    object. This package automatically detects and unwraps it during decoding.
  - Functional Options: A flexible API for building requests using [WithHTTPMethod],
    [WithVersion], and custom headers.
  - Parameter Marshalling: Automatically converts Go structs into URL-encoded query
    parameters via reflect-based tags.

# Response Unwrapping

A common pain point with Steam APIs is the "response" wrapper:
	{
	  "response": {
	    "players": [...]
	  }
	}
The UnmarshalJSON and UnmarshalVDF functions in this package detect the presence
of this key and decode the inner content directly into your target struct,
reducing boilerplate code in your service layers.

# Usage Example: Structured API Call

You can use the functional options to configure a transport request and then unmarshal
the result into a Go struct:

	type GetPlayerSummaryResponse struct {
		Players []Player `json:"players"`
	}

	// Create a target and request
	target := api.HttpTarget{URL: "https://api.steampowered.com/ISteamUser/GetPlayerSummaries/v2/"}
	req := tr.NewRequest(target, nil)

	// Configure and execute (pseudo-code)
	err := client.Do(req, target,
		api.WithQueryParam("steamids", "76561198..."),
		api.WithFormat(api.FormatJSON),
	)

	// Decode result
	var result GetPlayerSummaryResponse
	err = api.UnmarshalResponse(rawBody, &result, api.FormatJSON)

# Usage Example: Struct to URL Parameters

When dealing with many filters or complex queries, you can define a struct and
convert it to [url.Values]:

	type SearchParams struct {
		SearchText string `url:"filter"`
		Limit      int    `url:"count,omitempty"`
		Strict     bool   `url:"strict"`
	}

	params := SearchParams{SearchText: "G-Man", Strict: true}
	values, err := api.StructToValues(params)
	// values now contains: filter=G-Man&strict=true

# Error Handling

The package provides structured error types for Steam-specific failures:
  - EResultError: Wraps Steam's internal EResult enum into the Go error interface.
  - SteamAPIError: Handles structured errors from modern Steam interfaces,
    including authentication state and HTTP status codes.
*/
package api

// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

/*
Package api provides a specialized framework for interacting with Steam Web APIs.
It bridges the gap between raw network transport and domain-specific logic by
handling Steam's inconsistent response formats and parameter requirements.

# Core Features

  - Multiple Format Support: Seamlessly handles JSON, VDF (KeyValues), Protobuf,
    and Binary KeyValues (KV).
  - Customizable Decoding: The [UnmarshalRegistry] allows developers to register
    custom decoders or override default behavior for specific [ResponseFormat] types.
  - Automatic Unwrapping: Steam's Web API often wraps results in a top-level "response"
    object. Default decoders in this package automatically detect and unwrap it.
  - Functional Options: A flexible API for building requests using [WithHTTPMethod],
    [WithVersion], and custom headers.

# Extensible Decoding System

Unlike static decoders, this package uses an [UnmarshalRegistry]. This allows
for handling proprietary formats or modifying parsing logic without changing the
library's source code:

	registry := api.NewUnmarshalRegistry()
	registry.Register(api.FormatJSON, func(data []byte, target any) error {
		// Custom JSON logic here
		return json.Unmarshal(data, target)
	})

# Response Unwrapping

A common pain point with Steam APIs is the "response" wrapper:

	{
	  "response": {
	    "players": [...]
	  }
	}

The default implementations of [UnmarshalJSON], [UnmarshalVDFText], and [UnmarshalBinaryKV]
detect the presence of this key and decode the inner content directly into your
target struct.

# Usage Example: Structured API Call

You can use the functional options to configure a transport request and then unmarshal
the result using the registry:

	type GetPlayerSummaryResponse struct {
		Players []Player `json:"players"`
	}

	// Initialize registry (usually once per application)
	registry := api.NewUnmarshalRegistry()

	// Create a target and request
	target := api.HttpTarget{URL: "https://api.steampowered.com/ISteamUser/GetPlayerSummaries/v2/"}
	req := tr.NewRequest(target, nil)

	//Configure and execute (pseudo-code)
	resp, err := client.Do(req,
		api.WithQueryParam("steamids", "76561198..."),
		api.WithFormat(api.FormatJSON),
	)

	// Decode result using the registry
	var result GetPlayerSummaryResponse
	err = registry.Unmarshal(resp.Body, &result, api.FormatJSON)

# Error Handling

The package provides structured error types for Steam-specific failures:
  - EResultError: Wraps Steam's internal EResult enum into the Go error interface.
  - SteamAPIError: Handles structured errors from modern Steam interfaces,
    including authentication state and HTTP status codes.
*/
package api

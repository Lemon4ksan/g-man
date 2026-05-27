// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

/*
Package rest provides a lightweight, generic wrapper around net/http with
first-class support for proxy rotation and structured request building.

The package focuses on reducing boilerplate when dealing with REST APIs
by using Go generics for automatic decoding and a "RequestModifier" pattern
for flexible request customization.

# Key Components

  - [Client]: The primary, immutable REST client used to perform structured requests.
  - [ProxyRotator]: An [HTTPDoer] implementation that rotates requests across multiple proxies.
  - [RequestModifier]: A function type used to dynamically customize outgoing HTTP requests.
  - [BaseResponse]: An interface for unwrapping common API envelope formats.

# Basic Usage Example

Here is a complete, self-contained example demonstrating how to initialize a client
and perform a generic JSON GET request:

	package main

	import (
		"context"
		"fmt"
		"net/http"
		"time"
		"github.com/lemon4ksan/g-man/pkg/rest"
	)

	type TimeResponse struct {
		Time string `json:"time"`
	}

	func main() {
		ctx := context.Background()
		httpClient := &http.Client{Timeout: 10 * time.Second}
		client := rest.NewClient(httpClient)

		// Perform a generic JSON GET request
		res, err := rest.GetJSON[TimeResponse](ctx, client, "https://time.jsontest.com", nil)
		if err != nil {
			fmt.Println("Request failed:", err)
			return
		}
		fmt.Println("Server time:", res.Time)
	}
*/
package rest

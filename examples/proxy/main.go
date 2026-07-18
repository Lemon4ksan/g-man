// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"crypto/tls"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"github.com/lemon4ksan/aoni"
	"github.com/lemon4ksan/miyako/log"

	"github.com/lemon4ksan/g-man/pkg/steam"
	"github.com/lemon4ksan/g-man/pkg/steam/socket"
	"github.com/lemon4ksan/g-man/pkg/steam/socket/connector"
)

func SetupProxyClient(logger log.Logger, cmProxy string, webProxies []string) (*steam.Client, error) {
	// PART 1: Configure proxy for Connection Manager (CM) socket
	socketCfg := socket.DefaultConfig()

	// Set a dedicated proxy server to maintain a stable TCP session with Steam authentication servers
	socketCfg.Connector.ProxyURL = cmProxy
	socketCfg.Connector.ConnectTimeout = 30 * time.Second

	// Configure standard dialers to operate through a proxy server
	socketCfg.Connector.Dialers = connector.DefaultDialers()

	// PART 2: Proxy rotation for stateless HTTP WebAPI requests
	var rotatableClients []aoni.ClientWithProxy

	for _, proxyURL := range webProxies {
		// Configure transport settings for each proxy in the pool
		parsedURL, err := url.Parse(proxyURL)
		if err != nil {
			logger.Error("Skipping invalid proxy configuration", log.String("url", proxyURL), log.Err(err))
			continue
		}

		transport := &http.Transport{
			Proxy:                 http.ProxyURL(parsedURL),
			MaxIdleConns:          10,
			IdleConnTimeout:       30 * time.Second,
			TLSHandshakeTimeout:   10 * time.Second,
			ExpectContinueTimeout: 1 * time.Second,
			TLSClientConfig:       &tls.Config{InsecureSkipVerify: false},
		}

		httpClient := &http.Client{
			Transport: transport,
			Timeout:   15 * time.Second,
		}

		rotatableClients = append(rotatableClients, aoni.ClientWithProxy{Client: httpClient, ProxyURL: proxyURL})
	}

	if len(rotatableClients) == 0 {
		return nil, errors.New("no valid web proxies available for rotation")
	}

	// Initialize the proxy rotator with a strategy to automatically remove unhealthy nodes from the pool
	rotatorConfig := aoni.ProxyRotatorConfig{
		MaxFails:            3,                // Maximum of 3 consecutive errors before marking the proxy as unhealthy
		RetryAfter:          45 * time.Second, // Cool-off period to temporarily remove the unhealthy proxy from the pool
		HealthCheckURL:      "https://api.steampowered.com/ISteamDirectory/GetCMList/v1",
		HealthCheckInterval: 2 * time.Minute,
	}

	proxyRotator, err := aoni.NewProxyRotator(rotatorConfig, rotatableClients...)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize proxy rotator: %w", err)
	}

	// Enable sticky session support based on Steam session cookies
	// This ensures that requests within a single session route through the same proxy
	stickyRotator := proxyRotator.WithStickySessions(aoni.StickyKeyFromCookie("sessionid"))

	// Create retry middleware: in case of a proxy network failure, the request automatically retries on another node
	retryMiddleware := aoni.RetryMiddleware(aoni.RetryOptions{
		MaxRetries: 3,
		Backoff:    500 * time.Millisecond,
	}, aoni.ProxyRetryCondition(proxyRotator))

	// Build the call chain: Base client -> Logging -> Retry layer -> Rotator
	chainedDoer := aoni.Chain(stickyRotator, aoni.LoggingMiddleware(logger), retryMiddleware)

	// Initialize the final REST client that will make the calls
	restClient := aoni.NewClient(chainedDoer)

	// PART 3: Initialize the main Steam client
	clientCfg := steam.DefaultConfig()
	clientCfg.Socket = socketCfg
	// clientCfg.HTTP = chainedDoer // Our custom transport for all background HTTP tasks
	// clientCfg.REST = restClient

	client, err := steam.NewClient(clientCfg, steam.WithLogger(logger), steam.WithREST(restClient))
	if err != nil {
		return nil, fmt.Errorf("failed to create steam client: %w", err)
	}

	return client, nil
}

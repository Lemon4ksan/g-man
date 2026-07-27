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
	"github.com/lemon4ksan/aoni/middleware"
	"github.com/lemon4ksan/aoni/netutil/proxy"
	"github.com/lemon4ksan/miyako/log"

	"github.com/lemon4ksan/g-man/pkg/steam"
	"github.com/lemon4ksan/g-man/pkg/steam/socket"
)

var ErrNoWebProxies = errors.New("proxy: no valid web proxies available for rotation")

// SetupProxyClient demonstrates configuring dedicated socket proxy routing and rotating HTTP proxies with sticky sessions via aoni.
func SetupProxyClient(logger log.Logger, cmProxy string, webProxies []string) (*steam.Client, error) {
	socketCfg := socket.DefaultConfig()
	socketCfg.Connector.ProxyURL = cmProxy
	socketCfg.Connector.ConnectTimeout = 30 * time.Second
	socketCfg.Connector.Dialers = socket.DefaultDialers()

	var rotatableClients []proxy.WithClient

	for _, proxyURL := range webProxies {
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

		rotatableClients = append(rotatableClients, proxy.WithClient{Client: httpClient, ProxyURL: proxyURL})
	}

	if len(rotatableClients) == 0 {
		return nil, ErrNoWebProxies
	}

	rotatorConfig := proxy.RotatorConfig{
		MaxFails:            3,
		RetryAfter:          45 * time.Second,
		HealthCheckURL:      "https://api.steampowered.com/ISteamDirectory/GetCMList/v1",
		HealthCheckInterval: 2 * time.Minute,
	}

	proxyRotator, err := proxy.NewRotator(rotatorConfig, rotatableClients...)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize proxy rotator: %w", err)
	}

	stickyRotator := proxyRotator.WithStickySessions(proxy.StickyKeyFromCookie("sessionid"))

	retryMiddleware := middleware.Retry(middleware.RetryOptions{
		MaxRetries: 3,
		Backoff:    500 * time.Millisecond,
	}, proxy.RetryCondition(proxyRotator))

	chainedDoer := middleware.Chain(stickyRotator, middleware.Log(logger), retryMiddleware)
	restClient := aoni.NewClient(chainedDoer)

	clientCfg := steam.DefaultConfig()
	clientCfg.Socket = socketCfg

	client, err := steam.NewClient(clientCfg, steam.WithLogger(logger), steam.WithREST(restClient))
	if err != nil {
		return nil, fmt.Errorf("failed to create steam client: %w", err)
	}

	return client, nil
}

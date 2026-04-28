// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package rest

import (
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"sync/atomic"
	"time"
)

// ProxyConfig contains parameters for configuring an HTTP client with proxy support.
type ProxyConfig struct {
	ProxyURL           string        // Format: http://user:pass@ip:port or socks5://ip:port
	Timeout            time.Duration // Overall request timeout (recommended 15-30s for proxies)
	InsecureSkipVerify bool          // Disable SSL verification
}

// NewProxyClient creates a standard *http.Client configured to work through a proxy.
// It safely manages the connection pool to avoid memory leaks when running bots.
func NewProxyClient(cfg ProxyConfig) (*http.Client, error) {
	timeout := cfg.Timeout
	if timeout == 0 {
		timeout = 15 * time.Second
	}

	transport := &http.Transport{
		MaxIdleConns:          100,
		MaxIdleConnsPerHost:   10,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
		// #nosec G402 -- InsecureSkipVerify is configurable by the user for proxy compatibility.
		TLSClientConfig: &tls.Config{InsecureSkipVerify: cfg.InsecureSkipVerify},
	}

	if cfg.ProxyURL != "" {
		u, err := url.Parse(cfg.ProxyURL)
		if err != nil {
			return nil, fmt.Errorf("rest: invalid proxy URL %q: %w", cfg.ProxyURL, err)
		}

		// Go natively supports http://, https:// и socks5://
		transport.Proxy = http.ProxyURL(u)
	}

	return &http.Client{
		Transport: transport,
		Timeout:   timeout,
	}, nil
}

// ProxyRotatorConfig defines proxy health checking parameters.
type ProxyRotatorConfig struct {
	MaxFails   uint32        // How many errors in a row are allowed before shutdown (for example, 3)
	RetryAfter time.Duration // The time for which the proxy is excluded from the list (for example, 1 minute)
}

type trackedClient struct {
	client      HTTPDoer
	failCount   atomic.Uint32
	unhealthy   atomic.Bool
	recoveredAt atomic.Int64
}

// ProxyRotator allows distributing requests between multiple proxies.
// Implements the HTTPDoer interface, so it can be passed to [NewClient].
type ProxyRotator struct {
	clients []*trackedClient
	config  ProxyRotatorConfig
	current atomic.Uint64
}

// NewProxyRotator initializes the rotator (Round-Robin).
func NewProxyRotator(config ProxyRotatorConfig, clients ...HTTPDoer) (*ProxyRotator, error) {
	if len(clients) == 0 {
		return nil, errors.New("rest: proxy rotator requires at least one client")
	}

	if config.MaxFails == 0 {
		config.MaxFails = 3
	}

	if config.RetryAfter == 0 {
		config.RetryAfter = 30 * time.Second
	}

	tracked := make([]*trackedClient, len(clients))
	for i, c := range clients {
		tracked[i] = &trackedClient{client: c}
	}

	return &ProxyRotator{
		clients: tracked,
		config:  config,
	}, nil
}

// Do performs an HTTP request using the next available client in the rotation (Round-Robin).
func (r *ProxyRotator) Do(req *http.Request) (*http.Response, error) {
	var lastErr error

	n := uint64(len(r.clients))

	for range n {
		idx := r.current.Add(1) % n
		tc := r.clients[idx]

		if !r.isAvailable(tc) {
			continue
		}

		resp, err := tc.client.Do(req)

		if r.isProxyFault(resp, err) {
			r.markFailed(tc)

			lastErr = err
			if err == nil && resp != nil {
				_ = resp.Body.Close()
				lastErr = fmt.Errorf("rest: proxy returned status %d", resp.StatusCode)
			}

			continue
		}

		r.markSuccess(tc)

		return resp, err
	}

	if lastErr != nil {
		return nil, fmt.Errorf("rest: all proxies failed, last error: %w", lastErr)
	}

	return nil, errors.New("rest: no healthy proxies available")
}

func (r *ProxyRotator) isAvailable(tc *trackedClient) bool {
	if !tc.unhealthy.Load() {
		return true
	}

	if time.Now().UnixNano() >= tc.recoveredAt.Load() {
		return true
	}

	return false
}

func (r *ProxyRotator) markFailed(tc *trackedClient) {
	fails := tc.failCount.Add(1)
	if fails >= r.config.MaxFails {
		tc.unhealthy.Store(true)
		// Устанавливаем время восстановления
		recoveryTime := time.Now().Add(r.config.RetryAfter).UnixNano()
		tc.recoveredAt.Store(recoveryTime)
	}
}

func (r *ProxyRotator) markSuccess(tc *trackedClient) {
	tc.failCount.Store(0)
	tc.unhealthy.Store(false)
}

func (r *ProxyRotator) isProxyFault(resp *http.Response, err error) bool {
	if err != nil {
		var netErr net.Error
		if errors.As(err, &netErr) {
			return true
		}

		return true
	}

	if resp != nil {
		if resp.StatusCode == http.StatusProxyAuthRequired { // 407
			return true
		}

		if resp.StatusCode == http.StatusTooManyRequests { // 429
			return true
		}

		if resp.StatusCode == http.StatusBadGateway ||
			resp.StatusCode == http.StatusGatewayTimeout ||
			resp.StatusCode == http.StatusServiceUnavailable {
			return true
		}
	}

	return false
}

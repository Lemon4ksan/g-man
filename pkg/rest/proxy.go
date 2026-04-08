// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package rest

import (
	"crypto/tls"
	"fmt"
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
		TLSClientConfig:       &tls.Config{InsecureSkipVerify: cfg.InsecureSkipVerify},
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

// ProxyRotator allows distributing requests between multiple proxies.
// Implements the HTTPDoer interface, so it can be passed to [NewClient].
type ProxyRotator struct {
	clients []HTTPDoer
	current atomic.Uint64
}

// NewProxyRotator initializes the rotator (Round-Robin).
func NewProxyRotator(clients ...HTTPDoer) (*ProxyRotator, error) {
	if len(clients) == 0 {
		return nil, fmt.Errorf("rest: proxy rotator requires at least one client")
	}
	return &ProxyRotator{
		clients: clients,
	}, nil
}

func (r *ProxyRotator) Do(req *http.Request) (*http.Response, error) {
	idx := r.current.Add(1) % uint64(len(r.clients))
	return r.clients[idx].Do(req)
}

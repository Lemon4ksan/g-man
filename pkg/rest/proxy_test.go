// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package rest

import (
	"errors"
	"io"
	"net/http"
	"sync"
	"testing"
	"time"
)

type mockDoer struct {
	id         int
	calls      int
	forceError bool
	statusCode int
}

func (m *mockDoer) Do(req *http.Request) (*http.Response, error) {
	m.calls++

	var err error
	if m.forceError {
		err = errors.New("forced error")
	}

	if m.statusCode == 0 {
		m.statusCode = http.StatusOK
	}

	return &http.Response{StatusCode: m.statusCode, Body: io.NopCloser(nil)}, err
}

func TestNewProxyClient(t *testing.T) {
	t.Run("Default timeout", func(t *testing.T) {
		cfg := ProxyConfig{}

		client, err := NewProxyClient(cfg)
		if err != nil {
			t.Fatal(err)
		}

		if client.Timeout != 15*time.Second {
			t.Errorf("expected default timeout 15s, got %v", client.Timeout)
		}
	})

	t.Run("Custom config", func(t *testing.T) {
		proxyAddr := "http://user:pass@1.2.3.4:8080"
		cfg := ProxyConfig{
			ProxyURL:           proxyAddr,
			Timeout:            5 * time.Second,
			InsecureSkipVerify: true,
		}

		client, err := NewProxyClient(cfg)
		if err != nil {
			t.Fatal(err)
		}

		if client.Timeout != 5*time.Second {
			t.Errorf("expected timeout 5s, got %v", client.Timeout)
		}

		transport := client.Transport.(*http.Transport)
		if !transport.TLSClientConfig.InsecureSkipVerify {
			t.Error("expected InsecureSkipVerify to be true")
		}

		req, _ := http.NewRequest("GET", "http://google.com", nil)

		proxyURL, err := transport.Proxy(req)
		if err != nil {
			t.Fatalf("failed to get proxy URL from transport: %v", err)
		}

		if proxyURL.String() != proxyAddr {
			t.Errorf("expected proxy %s, got %s", proxyAddr, proxyURL.String())
		}
	})

	t.Run("Invalid proxy URL", func(t *testing.T) {
		cfg := ProxyConfig{
			ProxyURL: " ://invalid-url",
		}

		_, err := NewProxyClient(cfg)
		if err == nil {
			t.Error("expected error for invalid proxy URL, got nil")
		}
	})

	t.Run("No proxy", func(t *testing.T) {
		cfg := ProxyConfig{ProxyURL: ""}

		client, err := NewProxyClient(cfg)
		if err != nil {
			t.Fatal(err)
		}

		transport := client.Transport.(*http.Transport)
		if transport.Proxy != nil {
			req, _ := http.NewRequest("GET", "http://google.com", nil)

			p, _ := transport.Proxy(req)
			if p != nil {
				t.Errorf("expected no proxy, got %v", p)
			}
		}
	})
}

func TestProxyRotator(t *testing.T) {
	t.Run("Empty clients error", func(t *testing.T) {
		_, err := NewProxyRotator(ProxyRotatorConfig{})
		if err == nil || err.Error() != "rest: proxy rotator requires at least one client" {
			t.Errorf("expected specific error, got %v", err)
		}
	})

	t.Run("Round-Robin logic", func(t *testing.T) {
		m1 := &mockDoer{id: 1}
		m2 := &mockDoer{id: 2}
		m3 := &mockDoer{id: 3}

		rotator, err := NewProxyRotator(ProxyRotatorConfig{}, m1, m2, m3)
		if err != nil {
			t.Fatal(err)
		}

		req, _ := http.NewRequest("GET", "http://test", nil)

		for i := 0; i < 4; i++ {
			_, err := rotator.Do(req)
			if err != nil {
				t.Fatal(err)
			}
		}

		if m1.calls != 1 {
			t.Errorf("m1 expected 1 call, got %d", m1.calls)
		}

		if m2.calls != 2 {
			t.Errorf("m2 expected 2 calls, got %d", m2.calls)
		}

		if m3.calls != 1 {
			t.Errorf("m3 expected 1 call, got %d", m3.calls)
		}
	})

	t.Run("Concurrency safety", func(t *testing.T) {
		count := 10
		clients := make([]HTTPDoer, count)

		mocks := make([]*mockDoer, count)
		for i := 0; i < count; i++ {
			mocks[i] = &mockDoer{id: i}
			clients[i] = mocks[i]
		}

		rotator, _ := NewProxyRotator(ProxyRotatorConfig{}, clients...)

		var wg sync.WaitGroup

		iterations := 1000
		wg.Add(iterations)

		req, _ := http.NewRequest("GET", "http://test", nil)

		for i := 0; i < iterations; i++ {
			go func() {
				defer wg.Done()

				_, _ = rotator.Do(req)
			}()
		}

		wg.Wait()

		totalCalls := 0
		for _, m := range mocks {
			totalCalls += m.calls
		}

		if totalCalls != iterations {
			t.Errorf("expected total %d calls, got %d", iterations, totalCalls)
		}
	})
}

func TestProxyRotator_HealthCheck(t *testing.T) {
	m1 := &mockDoer{id: 1}
	m2 := &mockDoer{id: 2, forceError: true}

	cfg := ProxyRotatorConfig{
		MaxFails:   2,
		RetryAfter: 100 * time.Millisecond,
	}
	rotator, _ := NewProxyRotator(cfg, m1, m2)

	req, _ := http.NewRequest("GET", "http://test", nil)

	for i := 0; i < 5; i++ {
		_, _ = rotator.Do(req)
	}

	for i := 0; i < 10; i++ {
		resp, err := rotator.Do(req)
		if err != nil {
			continue
		}

		if resp != nil && m1.calls == 0 {
			t.Error("expected calls to go to m1 only")
		}
	}

	time.Sleep(150 * time.Millisecond)

	foundM2 := false
	for i := 0; i < 5; i++ {
		rotator.Do(req)

		if m2.calls > 2 {
			foundM2 = true
			break
		}
	}

	if !foundM2 {
		t.Error("m2 should have been retried after cooldown")
	}
}

func TestProxyRotator_RetryOnProxyError(t *testing.T) {
	m1 := &mockDoer{id: 1, statusCode: 407}
	m2 := &mockDoer{id: 2, statusCode: 200}

	rotator, _ := NewProxyRotator(ProxyRotatorConfig{MaxFails: 1}, m1, m2)

	req, _ := http.NewRequest("GET", "http://steam", nil)

	resp, err := rotator.Do(req)
	if err != nil {
		t.Fatalf("expected success after rotation, got %v", err)
	}

	if resp.StatusCode != 200 {
		t.Errorf("expected 200 from second proxy, got %d", resp.StatusCode)
	}

	req, _ = http.NewRequest("GET", "http://steam", nil)

	_, err = rotator.Do(req)
	if err != nil {
		t.Fatalf("expected success after rotation, got %v", err)
	}

	if !rotator.clients[0].unhealthy.Load() {
		t.Error("proxy 1 should be unhealthy after 407 error")
	}
}

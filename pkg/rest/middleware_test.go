// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package rest

import (
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestRetryMiddleware(t *testing.T) {
	t.Run("Retry on failure and preserve body", func(t *testing.T) {
		m1 := &mockDoer{id: 1, statusCode: 502}
		rotator, _ := NewProxyRotator(ProxyRotatorConfig{}, m1)

		opts := RetryOptions{
			MaxRetries: 3,
			Backoff:    5 * time.Millisecond,
		}

		retryMiddleware := RetryMiddleware(opts, rotator)
		client := retryMiddleware(m1)

		bodyText := "test body"
		req, _ := http.NewRequest("POST", "http://test", strings.NewReader(bodyText))

		go func() {
			time.Sleep(10 * time.Millisecond)

			m1.SetStatusCode(200)
		}()

		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("expected success after retry, got %v", err)
		}
		defer resp.Body.Close()

		if m1.GetCalls() < 2 {
			t.Errorf("expected at least 2 calls, got %d", m1.GetCalls())
		}

		if resp.StatusCode != 200 {
			t.Errorf("expected 200, got %d", resp.StatusCode)
		}
	})

	t.Run("Max retries exceeded", func(t *testing.T) {
		m1 := &mockDoer{id: 1, forceError: true}
		rotator, _ := NewProxyRotator(ProxyRotatorConfig{}, m1)

		opts := RetryOptions{
			MaxRetries: 1,
			Backoff:    1 * time.Millisecond,
		}

		client := RetryMiddleware(opts, rotator)(m1)
		req, _ := http.NewRequest("GET", "http://test", nil)

		_, err := client.Do(req)
		if err == nil {
			t.Fatal("expected error after max retries, got nil")
		}

		if m1.GetCalls() != 2 { // Initial + 1 retry
			t.Errorf("expected 2 calls, got %d", m1.GetCalls())
		}
	})
}

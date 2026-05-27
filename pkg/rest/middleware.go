// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package rest

import (
	"bytes"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"time"

	"golang.org/x/time/rate"

	"github.com/lemon4ksan/g-man/pkg/log"
)

// LoggingMiddleware returns a middleware that logs HTTP requests using the provided logger.
func LoggingMiddleware(logger log.Logger) Middleware {
	return func(next HTTPDoer) HTTPDoer {
		return DoerFunc(func(req *http.Request) (*http.Response, error) {
			start := time.Now()
			resp, err := next.Do(req)

			logger.Info("http request",
				log.String("method", req.Method),
				log.String("url", req.URL.String()),
				log.Duration("duration", time.Since(start)),
				log.Err(err),
			)

			return resp, err
		})
	}
}

// RateLimitMiddleware returns a middleware that limits the rate of requests.
// It uses a token bucket algorithm to control the request frequency.
func RateLimitMiddleware(rps float64, burst int) Middleware {
	limiter := rate.NewLimiter(rate.Limit(rps), burst)

	return func(next HTTPDoer) HTTPDoer {
		return DoerFunc(func(req *http.Request) (*http.Response, error) {
			if err := limiter.Wait(req.Context()); err != nil {
				return nil, fmt.Errorf("rest: rate limit wait failed: %w", err)
			}

			return next.Do(req)
		})
	}
}

// RetryOptions defines the configuration for the [RetryMiddleware].
type RetryOptions struct {
	// MaxRetries is the maximum number of attempts before giving up.
	MaxRetries uint32
	// Backoff is the initial delay duration before the first retry.
	Backoff time.Duration
}

// RetryMiddleware returns a middleware that retries requests on proxy-related faults.
// It automatically buffers the request body to allow multiple attempts.
func RetryMiddleware(opts RetryOptions, rotator *ProxyRotator) Middleware {
	if opts.MaxRetries == 0 {
		opts.MaxRetries = 3
	}

	if opts.Backoff == 0 {
		opts.Backoff = 1 * time.Second
	}

	return func(next HTTPDoer) HTTPDoer {
		return DoerFunc(func(req *http.Request) (*http.Response, error) {
			var (
				body []byte
				err  error
			)

			// Buffer body for potential retries
			if req.Body != nil && req.Body != http.NoBody {
				body, err = io.ReadAll(req.Body)
				if err != nil {
					return nil, fmt.Errorf("rest: failed to read request body for retry: %w", err)
				}

				_ = req.Body.Close()
			}

			backoff := opts.Backoff

			for i := uint32(0); i <= opts.MaxRetries; i++ {
				// Re-create body reader for each attempt
				if body != nil {
					req.Body = io.NopCloser(bytes.NewReader(body))
				}

				resp, err := next.Do(req)

				// Use the rotator's logic to check if we should retry
				if i < opts.MaxRetries && rotator.isProxyFault(resp, err) {
					if resp != nil {
						_ = resp.Body.Close()
					}

					// Add jitter (randomized +/- 10% of backoff)
					r, err := rand.Int(rand.Reader, big.NewInt(int64(backoff/5)))
					if err != nil {
						return nil, fmt.Errorf("rest: failed to generate jitter: %w", err)
					}

					jitter := time.Duration(r.Int64())
					sleepTime := backoff + (jitter - backoff/10)

					select {
					case <-req.Context().Done():
						return nil, req.Context().Err()
					case <-time.After(sleepTime):
						backoff *= 2 // Exponential backoff
						continue
					}
				}

				return resp, err
			}

			return nil, errors.New("rest: max retries exceeded")
		})
	}
}

// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package openid provides automated OpenID authentication for third-party websites
// that use "Sign in through Steam".
package openid

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"

	"github.com/PuerkitoBio/goquery"
	"github.com/lemon4ksan/g-man/pkg/rest"
)

var (
	// ErrNotSignedIn indicates that the provided Steam cookies are missing,
	// invalid, or expired, resulting in a redirect to the Steam login page.
	ErrNotSignedIn = errors.New("openid: not signed in to Steam (cookies expired or invalid)")

	// ErrNoForm indicates that the hidden OpenID submission form could not be found
	// on the Steam Community authorization page.
	ErrNoForm = errors.New("openid: could not find OpenID login form")

	// ErrWrongHost indicates that the initial request did not redirect to the
	// Steam Community OpenID provider as expected.
	ErrWrongHost = errors.New("openid: was not redirected to steamcommunity.com")
)

const defaultUserAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36"

// Login performs an automated OpenID authorization flow on a third-party website
// using active Steam session cookies.
//
// The function returns a configured *rest.Client which contains a CookieJar populated
// with the target website's authorization cookies. This client can be used for
// subsequent API requests to the third-party service.
//
// Example:
//
//	// Get cookies from your active auth.WebSession
//	cookies := mySession.Client().(*rest.Client).Jar().Cookies(steamURL)
//
//	// Login to a trading site
//	siteClient, err := openid.Login(ctx, "https://csgo-trading-site.com/login", cookies)
func Login(ctx context.Context, targetURL string, steamCookies []*http.Cookie) (*rest.Client, error) {
	jar, err := cookiejar.New(nil)
	if err != nil {
		return nil, fmt.Errorf("openid: failed to create cookie jar: %w", err)
	}

	steamURL, _ := url.Parse("https://steamcommunity.com")
	jar.SetCookies(steamURL, steamCookies)

	httpClient := &http.Client{
		Jar: jar,
		// Ensure we follow redirects (which is default, but good to be explicit for OpenID)
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return nil
		},
	}

	client := rest.NewClient(httpClient).WithHeader("User-Agent", defaultUserAgent)

	// Hit the target site's login URL. This should redirect us to Steam's OpenID page.
	resp, err := client.Request(ctx, http.MethodGet, targetURL, nil, nil)
	if err != nil {
		return nil, fmt.Errorf("openid: initial request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.Request.URL.Host != "steamcommunity.com" {
		return nil, fmt.Errorf("%w: ended up at %s", ErrWrongHost, resp.Request.URL.Host)
	}

	// Read body into memory so we can parse it
	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("openid: failed to read response body: %w", err)
	}

	doc, err := goquery.NewDocumentFromReader(bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, fmt.Errorf("openid: failed to parse HTML: %w", err)
	}

	// Check if Steam is asking us to log in (meaning cookies are bad)
	if doc.Find("#loginForm").Length() > 0 {
		return nil, ErrNotSignedIn
	}

	// Find the hidden OpenID submission form
	form := doc.Find("#openidForm")
	if form.Length() == 0 {
		return nil, ErrNoForm
	}

	// Extract all hidden input fields from the form
	formData := url.Values{}
	form.Find("input").Each(func(i int, s *goquery.Selection) {
		name, nameExists := s.Attr("name")
		value, _ := s.Attr("value")
		if nameExists {
			formData.Set(name, value)
		}
	})

	postURL := "https://steamcommunity.com/openid/login"
	if action, exists := form.Attr("action"); exists && action != "" {
		postURL = action
	}

	// Submit the form back to Steam. Steam will validate and redirect us
	// back to the third-party site with the OpenID assertion payload.
	formMod := func(req *http.Request) {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("Referer", resp.Request.URL.String())
	}

	postResp, err := client.Request(ctx, http.MethodPost, postURL, []byte(formData.Encode()), nil, formMod)
	if err != nil {
		return nil, fmt.Errorf("openid: form submission failed: %w", err)
	}
	defer postResp.Body.Close()

	// The third-party site's cookies are now securely stored in client's CookieJar.
	return client, nil
}

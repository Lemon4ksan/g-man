// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package openid

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strings"

	"github.com/PuerkitoBio/goquery"
)

var (
	ErrNotSignedIn = errors.New("openid: not signed in to Steam (cookies expired or invalid)")
	ErrNoForm      = errors.New("openid: could not find OpenID login form")
	ErrWrongHost   = errors.New("openid: was not redirected to steamcommunity.com")
)

// Login performs automatic OpenID authorization on a third-party website
// using Steam session cookies (steamLoginSecure, sessionid, etc.).
//
// The function returns a fully configured *http.Client, which contains
// a CookieJar with the target website's authorization cookies. You can use
// this client for further requests to the third-party API.
func Login(ctx context.Context, targetURL string, steamCookies []*http.Cookie) (*http.Client, error) {
	jar, err := cookiejar.New(nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create cookie jar: %w", err)
	}

	steamURL, _ := url.Parse("https://steamcommunity.com")
	jar.SetCookies(steamURL, steamCookies)

	client := &http.Client{
		Jar: jar,
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, targetURL, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36")

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("initial request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.Request.URL.Host != "steamcommunity.com" {
		return nil, fmt.Errorf("%w: ended up at %s", ErrWrongHost, resp.Request.URL.Host)
	}

	doc, err := goquery.NewDocumentFromReader(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to parse HTML: %w", err)
	}

	if doc.Find("#loginForm").Length() > 0 {
		return nil, ErrNotSignedIn
	}

	form := doc.Find("#openidForm")
	if form.Length() == 0 {
		return nil, ErrNoForm
	}

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

	postReq, err := http.NewRequestWithContext(ctx, http.MethodPost, postURL, strings.NewReader(formData.Encode()))
	if err != nil {
		return nil, err
	}

	postReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	postReq.Header.Set("Referer", resp.Request.URL.String())
	postReq.Header.Set("User-Agent", req.Header.Get("User-Agent"))

	postResp, err := client.Do(postReq)
	if err != nil {
		return nil, fmt.Errorf("openid submit failed: %w", err)
	}
	defer postResp.Body.Close()

	return client, nil
}

// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package openid executes automated Steam OpenID authentication flows against third-party websites.
package openid

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/cookiejar"
	"net/url"

	"github.com/PuerkitoBio/goquery"
	"github.com/lemon4ksan/aoni"
)

var (
	// ErrNotSignedIn indicates the provided Steam session cookies are expired or invalid.
	ErrNotSignedIn = errors.New("openid: not signed in to Steam (cookies expired or invalid)")
	// ErrNoForm indicates the OpenID submission form was missing on steamcommunity.com.
	ErrNoForm = errors.New("openid: could not find OpenID login form")
	// ErrWrongHost indicates initial authorization redirects ended outside steamcommunity.com.
	ErrWrongHost = errors.New("openid: was not redirected to steamcommunity.com")
)

// Login performs OpenID authentication on a target site using active Steam session cookies.
//
// Returns:
//   - Configured *aoni.Client populated with the target site's authenticated cookies.
func Login(ctx context.Context, targetURL string, steamCookies []*http.Cookie) (*aoni.Client, error) {
	parsedTarget, err := url.Parse(targetURL)
	if err != nil {
		return nil, fmt.Errorf("openid: invalid target URL: %w", err)
	}

	client, stdClient, err := createClientWithCookies(steamCookies)
	if err != nil {
		return nil, err
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, targetURL, nil)
	if err != nil {
		return nil, fmt.Errorf("openid: failed to create request: %w", err)
	}

	resp, err := stdClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("openid: initial request failed: %w", err)
	}

	defer resp.Body.Close()

	redirected, err := verifyRedirect(parsedTarget.Host, resp.Request.URL)
	if err != nil {
		return nil, err
	}

	if !redirected {
		return client, nil
	}

	doc, err := goquery.NewDocumentFromReader(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("openid: failed to parse HTML: %w", err)
	}

	form, err := parseOpenIDForm(doc)
	if err != nil {
		return nil, err
	}

	formData := extractFormInputs(form)
	postURL := resolveActionURL(resp.Request.URL, form)

	postReq, err := http.NewRequestWithContext(ctx, http.MethodPost, postURL, nil)
	if err != nil {
		return nil, fmt.Errorf("openid: failed to create post request: %w", err)
	}

	postReq.Header.Set("Referer", resp.Request.URL.String())
	postReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	postReq.URL.RawQuery = formData.Encode()

	postResp, err := stdClient.Do(postReq)
	if err != nil {
		return nil, fmt.Errorf("openid: form submission failed: %w", err)
	}

	_ = postResp.Body.Close()

	return client, nil
}

func createClientWithCookies(steamCookies []*http.Cookie) (*aoni.Client, *http.Client, error) {
	steamCommURL, _ := url.Parse("https://steamcommunity.com")
	steamStoreURL, _ := url.Parse("https://store.steampowered.com")

	jar, err := cookiejar.New(nil)
	if err != nil {
		return nil, nil, fmt.Errorf("openid: failed to create cookie jar: %w", err)
	}

	jar.SetCookies(steamCommURL, steamCookies)
	jar.SetCookies(steamStoreURL, steamCookies)

	stdClient := &http.Client{
		Jar:       jar,
		Transport: http.DefaultTransport,
	}

	return aoni.NewClient(stdClient), stdClient, nil
}

func verifyRedirect(originalTargetHost string, responseURL *url.URL) (bool, error) {
	if responseURL.Host == originalTargetHost {
		return false, nil
	}

	if responseURL.Host != "steamcommunity.com" {
		return false, fmt.Errorf("%w: ended up at %s", ErrWrongHost, responseURL.Host)
	}

	return true, nil
}

func parseOpenIDForm(doc *goquery.Document) (*goquery.Selection, error) {
	if doc.Find("#loginForm").Length() > 0 {
		return nil, ErrNotSignedIn
	}

	form := doc.Find("#openidForm")
	if form.Length() == 0 {
		return nil, ErrNoForm
	}

	return form, nil
}

func extractFormInputs(form *goquery.Selection) url.Values {
	formData := url.Values{}

	form.Find("input").Each(func(_ int, inputSel *goquery.Selection) {
		name, exists := inputSel.Attr("name")
		if !exists || name == "" {
			return
		}

		value, _ := inputSel.Attr("value")
		formData.Set(name, value)
	})

	if formData.Get("action") == "" {
		formData.Set("action", "steam_openid_login")
	}

	return formData
}

func resolveActionURL(currentURL *url.URL, form *goquery.Selection) string {
	defaultURL := "https://steamcommunity.com/openid/login"

	action, exists := form.Attr("action")
	if !exists || action == "" {
		return defaultURL
	}

	parsedAction, err := url.Parse(action)
	if err != nil {
		return defaultURL
	}

	return currentURL.ResolveReference(parsedAction).String()
}

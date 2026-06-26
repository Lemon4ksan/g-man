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

	"github.com/PuerkitoBio/goquery"
	"github.com/lemon4ksan/aoni"
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

// Login performs an automated OpenID authorization flow on a third-party website
// using active Steam session cookies.
//
// The function returns a configured [aoni.Client] which contains a CookieJar populated
// with the target website's authorization cookies. This client can be used for
// subsequent API requests to the third-party service.
func Login(ctx context.Context, targetURL string, steamCookies []*http.Cookie) (*aoni.Client, error) {
	parsedTarget, err := url.Parse(targetURL)
	if err != nil {
		return nil, fmt.Errorf("openid: invalid target URL: %w", err)
	}

	client, err := createClientWithCookies(steamCookies)
	if err != nil {
		return nil, err
	}

	resp, err := client.Request(ctx, http.MethodGet, targetURL)
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

	_, err = aoni.PostForm[aoni.NoResponse](
		ctx, client, postURL, formData,
		aoni.WithHeader("Referer", resp.Request.URL.String()),
	)
	if err != nil {
		return nil, fmt.Errorf("openid: form submission failed: %w", err)
	}

	return client, nil
}

func createClientWithCookies(steamCookies []*http.Cookie) (*aoni.Client, error) {
	steamCommURL, _ := url.Parse("https://steamcommunity.com")
	steamStoreURL, _ := url.Parse("https://store.steampowered.com")

	jar, err := cookiejar.New(nil)
	if err != nil {
		return nil, fmt.Errorf("openid: failed to create cookie jar: %w", err)
	}

	jar.SetCookies(steamCommURL, steamCookies)
	jar.SetCookies(steamStoreURL, steamCookies)

	return aoni.DefaultClient.WithCookieJar(jar), nil
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

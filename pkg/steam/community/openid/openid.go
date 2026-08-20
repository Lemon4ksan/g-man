// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package openid executes automated Steam OpenID authentication flows against third-party websites.
package openid

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"

	"github.com/lemon4ksan/aoni"
	"golang.org/x/net/html"
)

var (
	// ErrNotSignedIn indicates the provided Steam session cookies are expired or invalid.
	ErrNotSignedIn = errors.New("openid: not signed in to Steam (cookies expired or invalid)")
	// ErrNoForm indicates the OpenID submission form was missing on steamcommunity.com.
	ErrNoForm = errors.New("openid: could not find OpenID login form")
	// ErrWrongHost indicates initial authorization redirects ended outside steamcommunity.com.
	ErrWrongHost = errors.New("openid: was not redirected to steamcommunity.com")
)

type openIDForm struct {
	action string
	inputs url.Values
}

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

	form, err := parseOpenIDForm(resp.Body)
	if err != nil {
		return nil, err
	}

	postURL := resolveActionURL(resp.Request.URL, form.action)

	postReq, err := http.NewRequestWithContext(ctx, http.MethodPost, postURL, nil)
	if err != nil {
		return nil, fmt.Errorf("openid: failed to create post request: %w", err)
	}

	postReq.Header.Set("Referer", resp.Request.URL.String())
	postReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	postReq.URL.RawQuery = form.inputs.Encode()

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

func parseOpenIDForm(r io.Reader) (openIDForm, error) {
	doc, err := html.Parse(r)
	if err != nil {
		return openIDForm{}, fmt.Errorf("openid: failed to parse HTML: %w", err)
	}

	var loginFormFound bool
	var openidFormNode *html.Node
	var findForms func(*html.Node)

	findForms = func(n *html.Node) {
		if n.Type == html.ElementNode && n.Data == "form" {
			for _, attr := range n.Attr {
				if attr.Key == "id" {
					switch attr.Val {
					case "loginForm":
						loginFormFound = true
					case "openidForm":
						openidFormNode = n
					}
				}
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			findForms(c)
		}
	}
	findForms(doc)

	if loginFormFound {
		return openIDForm{}, ErrNotSignedIn
	}

	if openidFormNode == nil {
		return openIDForm{}, ErrNoForm
	}

	var action string
	for _, attr := range openidFormNode.Attr {
		if attr.Key == "action" {
			action = attr.Val
			break
		}
	}

	inputs := url.Values{}
	var extractInputs func(*html.Node)
	extractInputs = func(n *html.Node) {
		if n.Type == html.ElementNode && n.Data == "input" {
			var name, value string
			for _, attr := range n.Attr {
				switch attr.Key {
				case "name":
					name = attr.Val
				case "value":
					value = attr.Val
				}
			}
			if name != "" {
				inputs.Set(name, value)
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			extractInputs(c)
		}
	}
	extractInputs(openidFormNode)

	if inputs.Get("action") == "" {
		inputs.Set("action", "steam_openid_login")
	}

	return openIDForm{
		action: action,
		inputs: inputs,
	}, nil
}

func resolveActionURL(currentURL *url.URL, action string) string {
	defaultURL := "https://steamcommunity.com/openid/login"
	if action == "" {
		return defaultURL
	}

	parsedAction, err := url.Parse(action)
	if err != nil {
		return defaultURL
	}

	return currentURL.ResolveReference(parsedAction).String()
}

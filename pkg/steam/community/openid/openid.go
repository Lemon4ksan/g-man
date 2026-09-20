// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package openid executes automated Steam OpenID authentication flows against third-party websites,
// and provides Relying Party (RP) utilities for verifying Steam OpenID callbacks.
package openid

import (
	"context"
	"errors"
	"fmt"
	"io"
	"maps"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strings"

	"github.com/lemon4ksan/aoni"
	"github.com/lemon4ksan/aoni/x/codec/decode"
	"github.com/lemon4ksan/aoni/mod"
	"github.com/lemon4ksan/aoni/option"
	"golang.org/x/net/html"
)

var (
	// ErrNotSignedIn indicates the provided Steam session cookies are expired or invalid.
	ErrNotSignedIn = errors.New("openid: not signed in to Steam (cookies expired or invalid)")
	// ErrNoForm indicates the OpenID submission form was missing on steamcommunity.com.
	ErrNoForm = errors.New("openid: could not find OpenID login form")
	// ErrWrongHost indicates initial authorization redirects ended outside steamcommunity.com.
	ErrWrongHost = errors.New("openid: was not redirected to steamcommunity.com")
	// ErrAuthenticationFailed indicates Steam rejected the check_authentication request.
	ErrAuthenticationFailed = errors.New("openid: authentication failed (is_valid:false or missing)")
)

const (
	steamLoginURL = "https://steamcommunity.com/openid/login"
	openIDNS      = "http://specs.openid.net/auth/2.0"
	identifier    = "http://specs.openid.net/auth/2.0/identifier_select"
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

	client, err := createClientWithCookies(steamCookies)
	if err != nil {
		return nil, err
	}

	resp, err := client.Get(ctx, targetURL)
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

	postResp, err := client.Post(
		ctx,
		postURL,
		nil,
		mod.WithHeader("Referer", resp.Request.URL.String()),
		mod.WithFormValues(form.inputs),
	)
	if err != nil {
		return nil, fmt.Errorf("openid: form submission failed: %w", err)
	}

	_ = postResp.Body.Close()

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

	stdClient := &http.Client{
		Jar:       jar,
		Transport: http.DefaultTransport,
	}

	return aoni.NewClient(stdClient, option.WithRedirectLimit(10)), nil
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

	var (
		loginFormFound bool
		openidFormNode *html.Node
		findForms      func(*html.Node)
	)

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
	defaultURL := steamLoginURL
	if action == "" {
		return defaultURL
	}

	parsedAction, err := url.Parse(action)
	if err != nil {
		return defaultURL
	}

	return currentURL.ResolveReference(parsedAction).String()
}

// BuildRedirectURL generates the 302 redirect URL to send a user to Steam for OpenID authentication.
func BuildRedirectURL(realm, returnTo string) string {
	params := url.Values{}
	params.Set("openid.ns", openIDNS)
	params.Set("openid.mode", "checkid_setup")
	params.Set("openid.return_to", returnTo)
	params.Set("openid.realm", realm)
	params.Set("openid.identity", identifier)
	params.Set("openid.claimed_id", identifier)

	return steamLoginURL + "?" + params.Encode()
}

// VerifyCallback validates the OpenID callback query parameters by sending a check_authentication
// request to Steam.
func VerifyCallback(ctx context.Context, client *aoni.Client, query url.Values) (bool, error) {
	// Clone query so we don't mutate the input map
	reqQuery := make(url.Values, len(query))
	maps.Copy(reqQuery, query)

	reqQuery.Set("openid.mode", "check_authentication")

	// Post directly with aoni Client. We use raw Post because Steam returns text/plain ("ns:...\nis_valid:true\n"),
	// and we avoid typed decoding which expects JSON/XML.
	resp, err := client.Post(ctx, steamLoginURL, nil, mod.WithFormValues(reqQuery), decode.WithRaw())
	if err != nil {
		return false, fmt.Errorf("openid: check_authentication request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return false, fmt.Errorf("openid: unexpected status code from Steam: %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return false, fmt.Errorf("openid: failed to read response body: %w", err)
	}

	strBody := string(body)
	if !strings.Contains(strBody, "is_valid:true") {
		return false, ErrAuthenticationFailed
	}

	return true, nil
}

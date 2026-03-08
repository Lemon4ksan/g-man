// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strings"

	"github.com/lemon4ksan/g-man/pkg/log"
	tr "github.com/lemon4ksan/g-man/pkg/steam/transport"
)

const CommunityBase = "https://steamcommunity.com/"

var (
	ErrNotLoggedIn          = errors.New("steam community: not logged in (session expired)")
	ErrFamilyViewRestricted = errors.New("steam community: family view restricted")
	ErrRateLimit            = errors.New("steam community: rate limit exceeded")

	rxFamilyView = regexp.MustCompile(`<div id="parental_notice_instructions">Enter your PIN below to exit Family View\.<\/div>`)
	rxSorry      = regexp.MustCompile(`<h1>Sorry!<\/h1>[\s\S]*?<h3>(.+?)<\/h3>`)
	rxTradeError = regexp.MustCompile(`<div id="error_msg">\s*([^<]+)\s*<\/div>`)
)

type CommunityClient struct {
	transport   tr.Transport
	sessionFunc func(string) string
	logger      log.Logger
}

func NewCommunityClient(tr tr.Transport, sessionFunc func(string) string, logger log.Logger) *CommunityClient {
	return &CommunityClient{
		transport:   tr,
		sessionFunc: sessionFunc,
		logger:      logger,
	}
}

func (c *CommunityClient) SessionID(targetURI string) string {
	return c.sessionFunc(targetURI)
}

func (c *CommunityClient) Do(req *tr.Request) (*tr.Response, error) {
	target, ok := req.Target().(tr.HTTPTarget)
	if !ok {
		return nil, errors.New("community_client: request doesn't implement HTTPTarget")
	}

	if req.Header().Get("User-Agent") == "" {
		req.WithHeader("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36")
	}
	if target.HTTPMethod() != "GET" && req.Header().Get("Origin") == "" {
		u, _ := url.Parse(target.String())
		req.WithHeader("Origin", u.Scheme+"://"+u.Host)
	}

	c.logger.Debug("Community Request", log.String("method", target.HTTPMethod()), log.String("url", target.String()))

	resp, err := c.transport.Do(req)
	if err != nil {
		return nil, err
	}

	if err := c.checkSteamErrors(resp); err != nil {
		return resp, err
	}

	return resp, nil
}

func (c *CommunityClient) Get(ctx context.Context, path string, mods ...RequestModifier) (*tr.Response, error) {
	req := NewHttpRequest(ctx, "GET", CommunityBase+path, nil)
	for _, mod := range mods {
		mod(req)
	}
	return c.Do(req)
}

func (c *CommunityClient) GetJSON(ctx context.Context, path string, params url.Values, target any, mods ...RequestModifier) error {
	req := NewHttpRequest(ctx, "GET", CommunityBase+path, nil).
		WithHeader("Accept", "application/json, text/javascript; q=0.01").
		WithHeader("X-Requested-With", "XMLHttpRequest")

	if params != nil {
		req.WithParams(params)
	}

	for _, mod := range mods {
		mod(req)
	}

	resp, err := c.Do(req)
	if err != nil {
		return err
	}

	if len(resp.Body) == 0 {
		return fmt.Errorf("empty response received")
	}

	return json.Unmarshal(resp.Body, target)
}

func (c *CommunityClient) PostForm(ctx context.Context, path string, data url.Values, mods ...RequestModifier) (*tr.Response, error) {
	if data == nil {
		data = url.Values{}
	}

	if data.Get("sessionid") == "" {
		if sid := c.SessionID(CommunityBase); sid != "" {
			data.Set("sessionid", sid)
		}
	}

	bodyStr := data.Encode()
	req := NewHttpRequest(ctx, "POST", CommunityBase+path, []byte(bodyStr)).
		WithHeader("Content-Type", "application/x-www-form-urlencoded; charset=UTF-8")

	for _, mod := range mods {
		mod(req)
	}

	return c.Do(req)
}

// PostJSON injects sessionid via Query Parameters since Steam Community
// expects CSRF tokens in the URL for application/json payloads.
func (c *CommunityClient) PostJSON(ctx context.Context, path string, payload any, mods ...RequestModifier) (*tr.Response, error) {
	data, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal payload: %w", err)
	}

	req := NewHttpRequest(ctx, "POST", CommunityBase+path, data).
		WithHeader("Content-Type", "application/json; charset=UTF-8")

	if sid := c.SessionID(CommunityBase); sid != "" {
		req.WithParam("sessionid", sid)
	}

	for _, mod := range mods {
		mod(req)
	}

	return c.Do(req)
}

func (c *CommunityClient) checkSteamErrors(resp *tr.Response) error {
	// (Your exact implementation remains unchanged, it is perfect)
	if resp.StatusCode == 429 {
		return ErrRateLimit
	}
	if resp.StatusCode >= 500 {
		return fmt.Errorf("steam server error: %d", resp.StatusCode)
	}

	if resp.StatusCode >= 300 && resp.StatusCode <= 399 {
		if loc := resp.Header.Get("Location"); strings.Contains(loc, "/login") || strings.Contains(loc, "login.steampowered.com") {
			return ErrNotLoggedIn
		}
	}

	if resp.StatusCode == 403 && rxFamilyView.Match(resp.Body) {
		return ErrFamilyViewRestricted
	}

	bodyStr := string(resp.Body)
	if strings.Contains(bodyStr, "g_steamID = false;") && strings.Contains(bodyStr, "<title>Sign In</title>") {
		return ErrNotLoggedIn
	}

	if strings.Contains(bodyStr, "<h1>Sorry!</h1>") {
		if matches := rxSorry.FindStringSubmatch(bodyStr); len(matches) > 1 {
			return fmt.Errorf("steam community error: %s", strings.TrimSpace(matches[1]))
		}
		return fmt.Errorf("unknown steam community error (Sorry page)")
	}

	if matches := rxTradeError.FindStringSubmatch(bodyStr); len(matches) > 1 {
		return fmt.Errorf("trade error: %s", strings.TrimSpace(matches[1]))
	}

	if resp.StatusCode >= 400 {
		return fmt.Errorf("http error %d", resp.StatusCode)
	}

	return nil
}

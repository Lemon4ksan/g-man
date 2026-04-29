// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package websession provides a high-level interface for managing Steam web sessions.
//
// It automates the process of obtaining and synchronizing authentication cookies
// ('steamLoginSecure' and 'sessionid') across multiple Steam domains such as
// steamcommunity.com and steampowered.com.
//
// - Fast Path: If a valid Access Token is already present (from a mobile or
// desktop client), the library generates cookies instantly without network roundtrips.
//
// - Slow Path: Uses the official OIDC redirection flow (/jwt/finalizelogin) to
// ensure all Steam domains are correctly synchronized with the session.
package websession

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"maps"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"sync"

	"github.com/lemon4ksan/g-man/pkg/log"
	pb "github.com/lemon4ksan/g-man/pkg/protobuf/steam"
	"github.com/lemon4ksan/g-man/pkg/rest"
	"github.com/lemon4ksan/g-man/pkg/steam/id"
	"github.com/lemon4ksan/g-man/pkg/steam/protocol/enums"
)

var defaultDomains = []string{
	"https://steamcommunity.com",
	"https://store.steampowered.com",
	"https://help.steampowered.com",
	"https://login.steampowered.com",
	"https://s.team", // Short Steam domain, used for sharing and redirects
}

const (
	urlFinalize            = "https://login.steampowered.com/jwt/finalizelogin"
	urlVerify              = "https://steamcommunity.com/chat/clientinterfaces"
	cookieSessionID        = "sessionid"
	cookieSteamLoginSecure = "steamLoginSecure"
)

// WebSession handles HTTP-based interactions with Steam Community and Store.
// It manages a shared cookie jar and provides a thread-safe way to authenticate
// and verify the status of a web session.
type WebSession struct {
	mu sync.RWMutex

	steamID    id.ID
	client     *rest.Client
	httpClient *http.Client
	jar        http.CookieJar
	logger     log.Logger
	isAuth     bool
	domains    []string
}

// New creates a new, unauthenticated web session for the provided SteamID.
// It initializes the session with a fresh cookie jar and the default set
// of Steam domains.
func New(steamID id.ID, logger log.Logger) *WebSession {
	ws := &WebSession{
		steamID: steamID,
		logger:  logger,
		domains: append([]string{}, defaultDomains...),
	}
	ws.httpClient = &http.Client{}
	ws.client = rest.NewClient(ws.httpClient)

	ws.Clear()

	return ws
}

// AddDomains appends additional URLs to the session's synchronization list.
// Cookies generated during authentication will be seeded into these domains.
func (s *WebSession) AddDomains(domains ...string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.domains = append(s.domains, domains...)
}

// Authenticate synchronizes the web session with Steam's OIDC providers.
//
// It requires a valid refreshToken. For native platforms (SteamClient or MobileApp),
// it utilizes an accessToken to perform "Fast Path" cookie generation. For other
// platforms, it executes the full OIDC redirection/transfer flow ("Slow Path").
//
// On success, the internal CookieJar is populated with 'steamLoginSecure'
// and 'sessionid' across all configured domains.
func (s *WebSession) Authenticate(
	ctx context.Context,
	platform pb.EAuthTokenPlatformType,
	refreshToken, accessToken string,
) error {
	if refreshToken == "" {
		return errors.New("websession: refresh token is required")
	}

	// Clear any old cookies (to avoid conflicts when re-authorizing)
	s.Clear()

	sessionID, err := generateSessionID()
	if err != nil {
		return err
	}

	// Steam treats Web tokens and App tokens differently. Fast path is for client tokens.
	if platform == pb.EAuthTokenPlatformType_k_EAuthTokenPlatformType_SteamClient ||
		platform == pb.EAuthTokenPlatformType_k_EAuthTokenPlatformType_MobileApp && accessToken != "" {
		s.logger.Debug("Platform allows fast-path cookie generation")
		return s.applyFastPath(accessToken, sessionID)
	}

	return s.authSlowPath(ctx, refreshToken, sessionID)
}

// Verify proactively checks if the current web session is still valid on Steam servers.
//
// It performs a lightweight request to Steam Community. If the request fails,
// returns a non-200 status code, or is redirected to the login page, the session
// is considered dead and is automatically cleared.
func (s *WebSession) Verify(ctx context.Context) (bool, error) {
	if !s.IsAuthenticated() {
		return false, nil
	}

	s.logger.Debug("Verifying web session state...")
	client := s.REST()

	// chat/clientinterfaces endpoint is lightweight and reliably returns an error or redirect if the session is dead.
	resp, err := client.Request(ctx, http.MethodGet, urlVerify, nil, nil)
	if err != nil {
		return false, fmt.Errorf("verify request failed: %w", err)
	}
	defer resp.Body.Close()

	// If Steam resets your session, it often redirects to the login page
	if resp.StatusCode != http.StatusOK || resp.Request.URL.Path == "/login/home/" {
		s.logger.Warn("Web session verification failed (Token expired or revoked by Steam)")
		s.Clear() // Session is dead, reset local state

		return false, nil
	}

	return true, nil
}

// IsAuthenticated returns true if the session has successfully obtained login cookies.
func (s *WebSession) IsAuthenticated() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.isAuth
}

// SessionID retrieves the value of the 'sessionid' cookie for a specific target URL.
// This value is frequently required as a 'sessionid' parameter in POST requests
// to Steam to prevent CSRF. Returns an empty string if the cookie is not found
// or the URL is invalid.
func (s *WebSession) SessionID(targetURL string) string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	u, err := url.Parse(targetURL)
	if err != nil {
		return ""
	}

	for _, cookie := range s.jar.Cookies(u) {
		if cookie.Name == cookieSessionID {
			return cookie.Value
		}
	}

	return ""
}

// REST returns the underlying REST client.
// The client is configured with the session's internal CookieJar.
func (s *WebSession) REST() *rest.Client {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.client
}

// HTTP returns the raw http.Client used by the session.
// The client is configured with the session's internal CookieJar.
func (s *WebSession) HTTP() *http.Client {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.httpClient
}

// Clear completely resets the web session state.
// It wipes all cookies, resets the authentication flag, and generates
// a fresh HTTP client and CookieJar.
func (s *WebSession) Clear() {
	s.mu.Lock()
	defer s.mu.Unlock()

	jar, _ := cookiejar.New(nil)
	s.jar = jar
	s.httpClient.Jar = jar
	s.isAuth = false
}

func (s *WebSession) applyFastPath(accessToken, sessionID string) error {
	secureCookieValue := fmt.Sprintf("%d||%s", s.steamID, accessToken)
	s.seedCookies(sessionID, secureCookieValue)

	s.mu.Lock()
	s.isAuth = true
	s.mu.Unlock()

	s.logger.Info("Web session authenticated via existing token")

	return nil
}

// authSlowPath follows the full OIDC redirection/transfer flow for web-based tokens.
func (s *WebSession) authSlowPath(ctx context.Context, refreshToken, sessionID string) error {
	params := map[string]string{
		"nonce":         refreshToken,
		cookieSessionID: sessionID,
		"redir":         "https://steamcommunity.com/login/home/?goto=",
	}

	type finalizeResponse struct {
		Error        int `json:"error"`
		TransferInfo []struct {
			URL    string            `json:"url"`
			Params map[string]string `json:"params"`
		} `json:"transfer_info"`
	}

	client := s.REST()

	finalRes, err := rest.PostJSON[map[string]string, finalizeResponse](ctx, client, urlFinalize, params, nil)
	if err != nil {
		return fmt.Errorf("websession: finalize login failed: %w", err)
	}

	if finalRes.Error != 0 {
		return fmt.Errorf("websession: finalize login error code: %d", finalRes.Error)
	}

	// Execute transfers to other domains (community, store, etc.)
	for _, transfer := range finalRes.TransferInfo {
		transferParams := map[string]string{"steamID": fmt.Sprintf("%d", s.steamID)}
		maps.Copy(transferParams, transfer.Params)

		if err := s.executeTransferWithRetry(ctx, client, transfer.URL, transferParams); err != nil {
			return fmt.Errorf("transfer failed for %s: %w", transfer.URL, err)
		}
	}

	// Ensure sessionid is seeded across ALL steam domains for CSRF protection.
	s.seedCookies(sessionID, "")

	s.mu.Lock()
	s.isAuth = true
	s.mu.Unlock()

	s.logger.Info("Web session authenticated (Slow Path)")

	return nil
}

func (s *WebSession) seedCookies(sessionID, secureValue string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, domain := range s.domains {
		u, _ := url.Parse(domain)

		cookies := []*http.Cookie{
			{
				Name:     cookieSessionID,
				Value:    sessionID,
				Path:     "/",
				Secure:   true,
				HttpOnly: false, // Must be accessible by Steam's JS (Required for CSRF token extraction)
				SameSite: http.SameSiteLaxMode,
			},
		}
		if secureValue != "" {
			cookies = append(cookies, &http.Cookie{
				Name:     cookieSteamLoginSecure,
				Value:    secureValue,
				Path:     "/",
				Secure:   true,
				HttpOnly: true,                  // Secure token should never be readable by JS
				SameSite: http.SameSiteNoneMode, // Required for proper cross-domain auth (e.g., from store to community)
			})
		}

		s.jar.SetCookies(u, cookies)
	}
}

func (s *WebSession) executeTransferWithRetry(
	ctx context.Context,
	client rest.Requester,
	transferURL string,
	params map[string]string,
) error {
	const maxRetries = 3

	var lastErr error

	type transferResult struct {
		Result enums.EResult `json:"result"`
	}

	for range maxRetries {
		resp, err := rest.PostJSON[map[string]string, transferResult](ctx, client, transferURL, params, nil)
		if err != nil {
			lastErr = err
			continue
		}

		if resp.Result != enums.EResult_OK {
			return fmt.Errorf("steam error: %s", resp.Result.String())
		}

		return nil // Success
	}

	return fmt.Errorf("after %d retries: %w", maxRetries, lastErr)
}

func generateSessionID() (string, error) {
	var b [12]byte
	rand.Read(b[:])
	return hex.EncodeToString(b[:]), nil
}

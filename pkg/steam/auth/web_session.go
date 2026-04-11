// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package auth

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
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

// steamDomains are the standard Steam domains that require synchronized cookies.
var steamDomains = []string{
	"https://steamcommunity.com",
	"https://store.steampowered.com",
	"https://help.steampowered.com",
	"https://login.steampowered.com",
	"https://s.team", // Short Steam domain, used for sharing and redirects
}

// WebSessionManager defines the public interface for the web session.
type WebSessionManager interface {
	Authenticate(ctx context.Context, platform pb.EAuthTokenPlatformType, refreshToken, accessToken string) error
	IsAuthenticated() bool
	Verify(ctx context.Context) (bool, error)
	Client() rest.Requester
	SessionID(targetURL string) string
	Clear()
}

// WebSession handles HTTP-based interactions with Steam Community and Store.
// It uses a rest.Client with a shared cookie jar for state management.
type WebSession struct {
	mu sync.RWMutex

	steamID id.ID
	client  *rest.Client
	jar     http.CookieJar
	logger  log.Logger
	isAuth  bool
}

// NewWebSession creates a new, unauthenticated web session.
func NewWebSession(steamID id.ID, logger log.Logger) *WebSession {
	ws := &WebSession{
		steamID: steamID,
		logger:  logger,
	}
	ws.Clear()

	return ws
}

// Client returns the underlying REST requester.
func (s *WebSession) Client() *rest.Client {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.client
}

// IsAuthenticated returns true if the session has successfully obtained login cookies.
func (s *WebSession) IsAuthenticated() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.isAuth
}

// Clear completely resets the web session, wiping all cookies and generating a fresh HTTP client.
func (s *WebSession) Clear() {
	s.mu.Lock()
	defer s.mu.Unlock()

	jar, _ := cookiejar.New(nil)
	httpClient := &http.Client{Jar: jar}

	s.jar = jar
	s.client = rest.NewClient(httpClient)
	s.isAuth = false
}

// SessionID retrieves the 'sessionid' cookie value for a specific domain.
func (s *WebSession) SessionID(targetURL string) string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	u, err := url.Parse(targetURL)
	if err != nil {
		return ""
	}

	for _, cookie := range s.jar.Cookies(u) {
		if cookie.Name == "sessionid" {
			return cookie.Value
		}
	}

	return ""
}

// Authenticate synchronizes the web session with Steam's OIDC providers.
// It prioritizes the "Fast Path" if the platform matches a native client,
// otherwise it executes the full "Slow Path" (finalize/transfer) flow.
// This method populates the internal CookieJar with 'steamLoginSecure'
// and 'sessionid' across all Steam domains.
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

// Verify proactively checks if the web session is still alive on Steam servers.
func (s *WebSession) Verify(ctx context.Context) (bool, error) {
	if !s.IsAuthenticated() {
		return false, nil
	}

	s.logger.Debug("Verifying web session state...")

	// chat/clientinterfaces endpoint is lightweight and reliably returns an error or redirect if the session is dead.
	resp, err := s.Client().Request(ctx, http.MethodGet, "https://steamcommunity.com/chat/clientinterfaces", nil, nil)
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
		"nonce":     refreshToken,
		"sessionid": sessionID,
		"redir":     "https://steamcommunity.com/login/home/?goto=",
	}

	type finalizeResponse struct {
		Error        int `json:"error"`
		TransferInfo []struct {
			URL    string            `json:"url"`
			Params map[string]string `json:"params"`
		} `json:"transfer_info"`
	}

	finalRes, err := rest.PostJSON[map[string]string, finalizeResponse](
		ctx, s.Client(), "https://login.steampowered.com/jwt/finalizelogin", params, nil,
	)
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

		if err := s.executeTransferWithRetry(ctx, transfer.URL, transferParams); err != nil {
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

	for _, domain := range steamDomains {
		u, _ := url.Parse(domain)

		cookies := []*http.Cookie{
			{
				Name:     "sessionid",
				Value:    sessionID,
				Path:     "/",
				Secure:   true,
				HttpOnly: false, // Must be accessible by Steam's JS (Required for CSRF token extraction)
				SameSite: http.SameSiteLaxMode,
			},
		}
		if secureValue != "" {
			cookies = append(cookies, &http.Cookie{
				Name:     "steamLoginSecure",
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

func (s *WebSession) executeTransferWithRetry(ctx context.Context, transferURL string, params map[string]string) error {
	const maxRetries = 3

	var lastErr error

	type transferResult struct {
		Result enums.EResult `json:"result"`
	}

	for range maxRetries {
		resp, err := rest.PostJSON[map[string]string, transferResult](ctx, s.Client(), transferURL, params, nil)
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
	b := make([]byte, 12)
	if _, err := io.ReadFull(rand.Reader, b); err != nil {
		return "", err
	}

	return hex.EncodeToString(b), nil
}

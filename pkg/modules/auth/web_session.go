// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package auth

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"maps"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"sync"

	"github.com/lemon4ksan/g-man/pkg/log"
	"github.com/lemon4ksan/g-man/pkg/rest"
	pb "github.com/lemon4ksan/g-man/pkg/steam/protobuf"
	"github.com/lemon4ksan/g-man/pkg/steam/protocol"
)

// steamDomains are the standard Steam domains that require synchronized cookies.
var steamDomains = []string{
	"https://steamcommunity.com",
	"https://store.steampowered.com",
	"https://help.steampowered.com",
	"https://login.steampowered.com",
}

// WebSessionManager defines the public interface for the web session.
type WebSessionManager interface {
	Authenticate(ctx context.Context, authService *AuthenticationService, refreshToken string) error
	IsAuthenticated() bool
	Client() rest.Requester
	SessionID(targetURL string) string
}

// WebSession handles HTTP-based interactions with Steam Community and Store.
// It uses a rest.Client with a shared cookie jar for state management.
type WebSession struct {
	mu sync.RWMutex

	steamID uint64
	client  *rest.Client
	jar     http.CookieJar
	logger  log.Logger
	isAuth  bool
}

// NewWebSession creates a new, unauthenticated web session.
func NewWebSession(steamID uint64, logger log.Logger) *WebSession {
	jar, _ := cookiejar.New(nil)
	httpClient := &http.Client{Jar: jar}

	return &WebSession{
		steamID: steamID,
		logger:  logger,
		jar:     jar,
		client:  rest.NewClient(httpClient),
	}
}

// Client returns the underlying REST requester.
func (s *WebSession) Client() *rest.Client { return s.client }

// IsAuthenticated returns true if the session has successfully obtained login cookies.
func (s *WebSession) IsAuthenticated() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.isAuth
}

// SessionID retrieves the 'sessionid' cookie value for a specific domain.
func (s *WebSession) SessionID(targetURL string) string {
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

// Authenticate performs the OIDC login flow to establish web cookies using a refresh token.
func (s *WebSession) Authenticate(ctx context.Context, authService *AuthenticationService, refreshToken string) error {
	if refreshToken == "" {
		return fmt.Errorf("websession: refresh token is required")
	}

	sessionID, err := generateSessionID()
	if err != nil {
		return err
	}

	platform := authService.DeviceConf().PlatformType

	// Steam treats Web tokens and App tokens differently. Fast path is for client tokens.
	if platform == pb.EAuthTokenPlatformType_k_EAuthTokenPlatformType_SteamClient ||
		platform == pb.EAuthTokenPlatformType_k_EAuthTokenPlatformType_MobileApp {
		s.logger.Debug("Platform allows fast-path cookie generation")
		return s.authFastPath(ctx, authService, refreshToken, sessionID)
	}

	return s.authSlowPath(ctx, refreshToken, sessionID)
}

// authFastPath directly constructs the steamLoginSecure cookie using a JWT.
func (s *WebSession) authFastPath(ctx context.Context, authService *AuthenticationService, refreshToken, sessionID string) error {
	tokenResp, err := authService.GenerateAccessTokenForApp(ctx, refreshToken, s.steamID)
	if err != nil {
		return fmt.Errorf("websession: failed to generate token: %w", err)
	}

	// Format: SteamID||AccessToken
	secureCookieValue := fmt.Sprintf("%d||%s", s.steamID, tokenResp.GetAccessToken())

	s.seedCookies(sessionID, secureCookieValue)

	s.mu.Lock()
	s.isAuth = true
	s.mu.Unlock()

	s.logger.Info("Web session authenticated (Fast Path)")
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

	// The rest package uses url.Values, so we adapt our map.
	finalRes, err := rest.PostJSON[map[string]string, finalizeResponse](
		ctx, s.client, "https://login.steampowered.com/jwt/finalizelogin", params, nil,
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
	for _, domain := range steamDomains {
		u, _ := url.Parse(domain)
		cookies := []*http.Cookie{
			{
				Name:     "sessionid",
				Value:    sessionID,
				Path:     "/",
				Secure:   true,
				HttpOnly: false, // Must be accessible by Steam's JS
				SameSite: http.SameSiteLaxMode,
			},
		}
		if secureValue != "" {
			cookies = append(cookies, &http.Cookie{
				Name:     "steamLoginSecure",
				Value:    secureValue,
				Path:     "/",
				Secure:   true,
				HttpOnly: true,
				SameSite: http.SameSiteLaxMode,
			})
		}
		s.jar.SetCookies(u, cookies)
	}
}

func (s *WebSession) executeTransferWithRetry(ctx context.Context, transferURL string, params map[string]string) error {
	const maxRetries = 3
	var lastErr error

	type transferResult struct {
		Result protocol.EResult `json:"result"`
	}

	for range maxRetries {
		resp, err := rest.PostJSON[map[string]string, transferResult](ctx, s.client, transferURL, params, nil)
		if err != nil {
			lastErr = err
			continue
		}

		if resp.Result != protocol.EResult_OK {
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

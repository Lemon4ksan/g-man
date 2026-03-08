// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package auth

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"mime/multipart"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"sync"
	"time"

	"github.com/lemon4ksan/g-man/pkg/log"
	"github.com/lemon4ksan/g-man/pkg/steam/protocol"
	pb "github.com/lemon4ksan/g-man/pkg/steam/protocol/protobuf"
	"github.com/lemon4ksan/g-man/pkg/steam/transport"
)

// Standard Steam domains that require synchronization of cookies.
var steamDomains = []string{
	"https://steamcommunity.com",
	"https://store.steampowered.com",
	"https://help.steampowered.com",
	"https://login.steampowered.com",
}

type WebSessionManager interface {
	Authenticate(ctx context.Context, authService *AuthenticationService, refreshToken string) error
	IsAuthenticated() bool
	Client() *http.Client
	SessionID(targetURL string) string
	SteamID() uint64
}

// WebSession handles HTTP-based interactions with Steam Community and Store.
type WebSession struct {
	mu sync.RWMutex

	steamID uint64
	client  *http.Client
	logger  log.Logger
	isAuth  bool
}

func NewWebSession(steamID uint64, logger log.Logger) *WebSession {
	jar, _ := cookiejar.New(nil)
	return &WebSession{
		steamID: steamID,
		logger:  logger,
		client: &http.Client{
			Timeout: 30 * time.Second,
			Jar:     jar,
		},
	}
}

func (s *WebSession) Client() *http.Client { return s.client }

func (s *WebSession) SteamID() uint64 { return s.steamID }

func (s *WebSession) IsAuthenticated() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.isAuth
}

// Authenticate performs the OIDC login flow to establish web cookies.
func (s *WebSession) Authenticate(ctx context.Context, authService *AuthenticationService, refreshToken string) error {
	if refreshToken == "" {
		return fmt.Errorf("web_session: refresh token is required")
	}

	sessionID, err := generateSessionID()
	if err != nil {
		return err
	}

	platform := authService.DeviceConf().PlatformType

	// Steam treats Web tokens and App tokens differently.
	// Using a SteamClient token via authSlowPath (Web login) often causes 401.
	if platform == pb.EAuthTokenPlatformType_k_EAuthTokenPlatformType_SteamClient ||
		platform == pb.EAuthTokenPlatformType_k_EAuthTokenPlatformType_MobileApp {
		s.logger.Debug("Platform allows fast-path cookie generation")
		return s.authFastPath(ctx, authService, refreshToken, sessionID)
	}

	return s.authSlowPath(ctx, refreshToken, sessionID)
}

func (s *WebSession) SessionID(targetURL string) string {
	u, err := url.Parse(targetURL)
	if err != nil {
		return ""
	}
	for _, cookie := range s.client.Jar.Cookies(u) {
		if cookie.Name == "sessionid" {
			return cookie.Value
		}
	}
	return ""
}

// authFastPath directly constructs the steamLoginSecure cookie using a JWT.
func (s *WebSession) authFastPath(ctx context.Context, authService *AuthenticationService, refreshToken, sessionID string) error {
	tokenResp, err := authService.GenerateAccessTokenForApp(ctx, refreshToken, s.steamID)
	if err != nil {
		return fmt.Errorf("fast_path generate token: %w", err)
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

// authSlowPath follows the full OIDC redirection/transfer flow.
func (s *WebSession) authSlowPath(ctx context.Context, refreshToken, sessionID string) error {
	params := map[string]string{
		"nonce":     refreshToken,
		"sessionid": sessionID,
		"redir":     "https://steamcommunity.com/login/home/?goto=",
	}

	resp, err := s.doMultipartRequest(ctx, "https://login.steampowered.com/jwt/finalizelogin", params)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	var finalRes finalizeResponse
	if err := json.NewDecoder(resp.Body).Decode(&finalRes); err != nil {
		return fmt.Errorf("decode finalizelogin: %w", err)
	}

	if finalRes.Error != 0 {
		return fmt.Errorf("finalizelogin error code: %d", finalRes.Error)
	}

	// FinalizeLogin sets the initial cookies for login.steampowered.com
	s.extractCookiesToJar(resp)

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
		s.client.Jar.SetCookies(u, cookies)
	}
}

func (s *WebSession) executeTransferWithRetry(ctx context.Context, transferURL string, params map[string]string) error {
	const maxRetries = 3
	var lastErr error

	for i := 0; i < maxRetries; i++ {
		resp, err := s.doMultipartRequest(ctx, transferURL, params)
		if err != nil {
			lastErr = err
			continue
		}

		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			lastErr = fmt.Errorf("status %d", resp.StatusCode)
			continue
		}

		// Check for EResult in JSON body
		var tr transferResult
		if err := json.Unmarshal(body, &tr); err == nil {
			if tr.Result != protocol.EResult_OK {
				return fmt.Errorf("steam error: %s", tr.Result.String())
			}
		}

		s.extractCookiesToJar(resp)
		return nil
	}
	return fmt.Errorf("after %d retries: %w", maxRetries, lastErr)
}

func (s *WebSession) doMultipartRequest(ctx context.Context, reqURL string, params map[string]string) (*http.Response, error) {
	var b bytes.Buffer
	w := multipart.NewWriter(&b)

	for k, v := range params {
		_ = w.WriteField(k, v)
	}
	w.Close()

	req, err := http.NewRequestWithContext(ctx, "POST", reqURL, &b)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Content-Type", w.FormDataContentType())
	req.Header.Set("User-Agent", transport.HTTPUserAgent)
	// Important for CORS-like requests in Steam's backend
	req.Header.Set("Referer", "https://steamcommunity.com/")

	return s.client.Do(req)
}

func (s *WebSession) extractCookiesToJar(resp *http.Response) {
	if cookies := resp.Cookies(); len(cookies) > 0 {
		s.client.Jar.SetCookies(resp.Request.URL, cookies)
	}
}

func generateSessionID() (string, error) {
	b := make([]byte, 12)
	if _, err := io.ReadFull(rand.Reader, b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

type finalizeResponse struct {
	Error        int `json:"error"`
	TransferInfo []struct {
		URL    string            `json:"url"`
		Params map[string]string `json:"params"`
	} `json:"transfer_info"`
}

type transferResult struct {
	Result protocol.EResult `json:"result"`
}

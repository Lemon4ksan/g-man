// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package websession provides a high-level interface for managing Steam web sessions.
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
	"strings"
	"sync"
	"time"

	"github.com/lemon4ksan/aoni"
	"github.com/lemon4ksan/miyako/log"

	pb "github.com/lemon4ksan/g-man/pkg/protobuf/steam"
	"github.com/lemon4ksan/g-man/pkg/steam/id"
	"github.com/lemon4ksan/g-man/pkg/steam/protocol/enums"
)

// DefaultDomains is the list of domains that will have authenticated cookies.
var DefaultDomains = []string{
	"https://steamcommunity.com",
	"https://store.steampowered.com",
	"https://help.steampowered.com",
	"https://login.steampowered.com",
	"https://s.team",
}

const (
	urlFinalize            = "https://login.steampowered.com/jwt/finalizelogin"
	urlVerify              = "https://steamcommunity.com/chat/clientinterfaces"
	cookieSessionID        = "sessionid"
	cookieSteamLoginSecure = "steamLoginSecure"
)

// WebSession handles HTTP-based interactions with Steam Community and Store.
// It manages a shared cookie jar and provides a thread-safe way to authenticate.
//
// It implements the [aoni.HTTPDoer] interface, allowing it to be used
// as a transport for REST clients that require session-aware cookies.
//
// By default, cookies are synchronized across standard Steam domains. To add
// additional custom domains, use [WebSession.AddDomains].
// Use [New] to create new instances of WebSession.
type WebSession struct {
	mu sync.RWMutex

	steamID    id.ID
	baseDoer   aoni.HTTPDoer
	httpClient *http.Client
	jar        http.CookieJar
	logger     log.Logger
	isAuth     bool
	domains    []*url.URL

	retryBackoff time.Duration
}

type doerRoundTripper struct {
	doer aoni.HTTPDoer
}

func (d *doerRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	return d.doer.Do(req)
}

// New creates a new, unauthenticated web session for the provided SteamID.
//
// If the baseDoer argument is nil, it automatically initializes a default
// [http.Client] with a 30-second timeout.
func New(steamID id.ID, logger log.Logger, baseDoer aoni.HTTPDoer) *WebSession {
	if baseDoer == nil {
		baseDoer = &http.Client{Timeout: 30 * time.Second}
	}

	ws := &WebSession{
		steamID:      steamID,
		baseDoer:     baseDoer,
		logger:       logger.With(log.Module("websession")),
		retryBackoff: time.Second,
	}

	for _, d := range DefaultDomains {
		if u, err := url.Parse(d); err == nil {
			ws.domains = append(ws.domains, u)
		}
	}

	ws.Clear()

	return ws
}

// Do implements [aoni.HTTPDoer]. It executes the request using the session's
// internal cookie-aware HTTP client.
func (s *WebSession) Do(req *http.Request) (*http.Response, error) {
	s.mu.RLock()
	client := s.httpClient
	s.mu.RUnlock()

	return client.Do(req) //nolint:gosec
}

// REST returns a new [aoni.Client] instance configured to use this session.
func (s *WebSession) REST() *aoni.Client {
	s.mu.RLock()
	backoff := s.retryBackoff
	s.mu.RUnlock()

	retrier := aoni.RetryMiddleware(aoni.RetryOptions{
		MaxRetries: 3,
		Backoff:    backoff,
	}, aoni.RetryOnErr())

	s.mu.RLock()
	defer s.mu.RUnlock()

	return aoni.NewClient(aoni.Chain(s, retrier))
}

// HTTP returns the raw cookie-aware [http.Client].
func (s *WebSession) HTTP() *http.Client {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.httpClient
}

// AddDomains appends additional URLs to the session's synchronization list.
func (s *WebSession) AddDomains(domains ...string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, d := range domains {
		if u, err := url.Parse(d); err == nil {
			s.domains = append(s.domains, u)
		}
	}
}

// Authenticate synchronizes the web session with Steam's OIDC providers.
//
// If the platform type is a client or mobile app and a non-empty accessToken is provided,
// Authenticate performs a fast-path direct cookie injection. Otherwise, it executes
// the slow-path OIDC finalization flow, resolving TransferInfo redirects.
//
// It returns an error if the refreshToken is empty, if the network request fails,
// or if Steam returns an EResult failure.
func (s *WebSession) Authenticate(
	ctx context.Context,
	platform pb.EAuthTokenPlatformType,
	refreshToken, accessToken string,
) error {
	if refreshToken == "" {
		return errors.New("websession: refresh token is required")
	}

	s.Clear()

	sessionID := generateSessionID()

	if platform == pb.EAuthTokenPlatformType_k_EAuthTokenPlatformType_SteamClient ||
		platform == pb.EAuthTokenPlatformType_k_EAuthTokenPlatformType_MobileApp && accessToken != "" {
		return s.applyFastPath(accessToken, sessionID)
	}

	return s.authSlowPath(ctx, refreshToken, sessionID)
}

// Verify proactively checks if the current web session is still valid.
//
// It performs a GET request to the Steam chat client interfaces endpoint.
// If the session is invalid, expired, or redirected to the login page, Verify
// automatically clears the internal cookie jar and returns false.
func (s *WebSession) Verify(ctx context.Context) (bool, error) {
	if !s.IsAuthenticated() {
		return false, nil
	}

	_, err := aoni.GetTo[aoni.NoResponse](ctx, s.REST(), urlVerify)
	if err != nil {
		s.Clear()
		return false, nil //nolint:nilerr
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
//
// It returns an empty string if the targetURL is empty or cannot be parsed as a valid URL.
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

// Clear completely resets the web session state by instantiating a fresh cookie jar.
func (s *WebSession) Clear() {
	s.mu.Lock()
	defer s.mu.Unlock()

	jar, _ := cookiejar.New(nil)
	s.jar = jar

	s.httpClient = &http.Client{
		Transport: &doerRoundTripper{doer: s.baseDoer},
		Jar:       jar,
		Timeout:   30 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 10 {
				return errors.New("steam: stopped after 10 redirects (redirect loop)")
			}

			if strings.Contains(req.URL.Path, "/login/home") {
				return errors.New("websession: session expired (redirected to login)")
			}

			return nil
		},
	}
	s.isAuth = false
}

func (s *WebSession) applyFastPath(accessToken, sessionID string) error {
	secureCookieValue := fmt.Sprintf("%d||%s", s.steamID, accessToken)
	s.seedCookies(sessionID, secureCookieValue)

	s.mu.Lock()
	s.isAuth = true
	s.mu.Unlock()

	return nil
}

func (s *WebSession) authSlowPath(ctx context.Context, refreshToken, sessionID string) error {
	payload := map[string]string{
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

	res, err := aoni.PostTo[finalizeResponse](ctx, s.REST(), urlFinalize, payload)
	if err != nil {
		return fmt.Errorf("websession: finalize login failed: %w", err)
	}

	if res.Error != 0 {
		return fmt.Errorf("websession: finalize login error code: %d", res.Error)
	}

	for _, transfer := range res.TransferInfo {
		transferParams := map[string]string{"steamID": fmt.Sprintf("%d", s.steamID)}
		maps.Copy(transferParams, transfer.Params)

		if err := s.executeTransfer(ctx, transfer.URL, transferParams); err != nil {
			return err
		}
	}

	s.seedCookies(sessionID, "")

	s.mu.Lock()
	s.isAuth = true
	s.mu.Unlock()

	return nil
}

func (s *WebSession) executeTransfer(ctx context.Context, transferURL string, params map[string]string) error {
	type transferResp struct {
		Result enums.EResult `json:"result"`
	}

	resp, err := aoni.PostTo[transferResp](ctx, s.REST(), transferURL, params)
	if err != nil {
		return err
	}

	if resp.Result != enums.EResult_OK {
		return fmt.Errorf("steam error: %s", resp.Result.String())
	}

	return nil
}

func (s *WebSession) seedCookies(sessionID, secureValue string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, u := range s.domains {
		cookies := []*http.Cookie{
			{
				Name:     cookieSessionID,
				Value:    sessionID,
				Path:     "/",
				Secure:   true,
				HttpOnly: true,
				SameSite: http.SameSiteLaxMode,
			},
		}
		if secureValue != "" {
			cookies = append(cookies, &http.Cookie{
				Name:     cookieSteamLoginSecure,
				Value:    secureValue,
				Path:     "/",
				Secure:   true,
				HttpOnly: true,
				SameSite: http.SameSiteNoneMode,
			})
		}

		s.jar.SetCookies(u, cookies)
	}
}

func generateSessionID() string {
	var b [12]byte

	_, _ = rand.Read(b[:])

	return hex.EncodeToString(b[:])
}

// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package websession manages cookie jars and OIDC authentication routines across Steam web domains.
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
	"github.com/lemon4ksan/aoni/fast"
	"github.com/lemon4ksan/aoni/middleware"
	"github.com/lemon4ksan/aoni/mod"
	"github.com/lemon4ksan/aoni/request"
	"github.com/lemon4ksan/miyako/log"

	pb "github.com/lemon4ksan/g-man/pkg/protobuf/steam"
	"github.com/lemon4ksan/g-man/pkg/steam/id"
	"github.com/lemon4ksan/g-man/pkg/steam/protocol/enums"
)

var (
	// ErrRefreshTokenRequired indicates authentication failed due to an empty refresh token.
	ErrRefreshTokenRequired = errors.New("websession: refresh token is required")
	// ErrTooManyRedirects indicates HTTP redirect iteration exceeded limits.
	ErrTooManyRedirects = errors.New("websession: stopped after 10 redirects (redirect loop)")
	// ErrSessionExpiredRedirect indicates the request was redirected to the login page.
	ErrSessionExpiredRedirect = errors.New("websession: session expired (redirected to login)")
)

// DefaultDomains contains standard Steam web domain URLs synchronized by WebSession.
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

// WebSession maintains cookie jars across Steam web domains and provides authenticated HTTP Doer capabilities.
//
// Thread Safety:
//   - Safe for concurrent use across all methods.
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

// New constructs an unauthenticated WebSession.
func New(steamID id.ID, logger log.Logger, doer any) *WebSession {
	var httpDoer aoni.HTTPDoer

	if doer == nil {
		fastEngine := fast.NewClient()
		httpDoer = fast.NewStdClient(fastEngine)
	} else if fc, ok := doer.(*fast.Client); ok {
		httpDoer = fast.NewStdClient(fc)
	} else if ac, ok := doer.(*aoni.Client); ok {
		httpDoer = ac.HTTP()
	} else if hd, ok := doer.(aoni.HTTPDoer); ok {
		httpDoer = hd
	} else if rd, ok := doer.(aoni.RequestDoer); ok {
		httpDoer = aoni.NewRequestDoerAdapter(rd)
	} else {
		httpDoer = aoni.NewClient(nil).HTTP()
	}

	ws := &WebSession{
		steamID:      steamID,
		baseDoer:     httpDoer,
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

// Do executes an HTTP request using the active cookie jar.
func (s *WebSession) Do(req *http.Request) (*http.Response, error) {
	s.mu.RLock()
	client := s.httpClient
	s.mu.RUnlock()

	return client.Do(req) //nolint:gosec
}

// REST returns an aoni.Client wrapping the web session with exponential backoff retries.
func (s *WebSession) REST() *aoni.Client {
	s.mu.RLock()
	backoff := s.retryBackoff
	s.mu.RUnlock()

	retrier := middleware.Retry(middleware.RetryOptions{
		MaxRetries: 3,
		Backoff:    backoff,
	}, middleware.RetryOnErr())

	s.mu.RLock()
	defer s.mu.RUnlock()

	return aoni.NewClient(middleware.Chain(s, retrier))
}

// HTTP returns the underlying http.Client instance.
func (s *WebSession) HTTP() *http.Client {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.httpClient
}

// AddDomains registers additional web URLs for cookie synchronization.
func (s *WebSession) AddDomains(domains ...string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, d := range domains {
		if u, err := url.Parse(d); err == nil {
			s.domains = append(s.domains, u)
		}
	}
}

// Authenticate performs OIDC web finalization or fast-path cookie injection.
func (s *WebSession) Authenticate(
	ctx context.Context,
	platform pb.EAuthTokenPlatformType,
	refreshToken, accessToken string,
) error {
	if refreshToken == "" {
		return ErrRefreshTokenRequired
	}

	s.Clear()

	sessionID := generateSessionID()

	if platform == pb.EAuthTokenPlatformType_k_EAuthTokenPlatformType_SteamClient ||
		platform == pb.EAuthTokenPlatformType_k_EAuthTokenPlatformType_MobileApp && accessToken != "" {
		return s.applyFastPath(accessToken, sessionID)
	}

	return s.authSlowPath(ctx, refreshToken, sessionID)
}

// Verify checks session validity by requesting Steam chat interface endpoints.
func (s *WebSession) Verify(ctx context.Context) (bool, error) {
	if !s.IsAuthenticated() {
		return false, nil
	}

	_, err := request.GetTo[request.NoResponse](ctx, s.REST(), urlVerify)
	if err != nil {
		s.Clear()
		return false, nil //nolint:nilerr
	}

	return true, nil
}

// IsAuthenticated reports whether authenticated cookies are active.
func (s *WebSession) IsAuthenticated() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.isAuth
}

// SessionID retrieves the 'sessionid' cookie value for a target URL string.
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

// Clear resets the internal cookie jar.
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
				return ErrTooManyRedirects
			}

			if strings.Contains(req.URL.Path, "/login/home") {
				return ErrSessionExpiredRedirect
			}

			return nil
		},
	}
	s.isAuth = false
}

func (s *WebSession) applyFastPath(accessToken, sessionID string) error {
	secureCookieValue := fmt.Sprintf("%d%%7C%%7C%s", s.steamID, accessToken)
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

	res, err := request.PostTo[finalizeResponse](ctx, s.REST(), urlFinalize, payload)
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

	resp, err := request.PostTo[transferResp](ctx, s.REST(), transferURL, nil, mod.WithFormBody(params))
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

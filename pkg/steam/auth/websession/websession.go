// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package websession manages cookie jars and OIDC authentication routines across Steam web domains.
package websession

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"maps"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/lemon4ksan/aoni"
	"github.com/lemon4ksan/aoni/middleware"
	"github.com/lemon4ksan/aoni/mod"
	log "github.com/lemon4ksan/foundation/async/logkit"
	"github.com/lemon4ksan/foundation/generic"

	"github.com/lemon4ksan/g-man/internal/network"
	"github.com/lemon4ksan/g-man/pkg/steam/id"
	"github.com/lemon4ksan/g-man/pkg/steam/protocol/enums"
	"github.com/lemon4ksan/g-man/pkg/steam/service"
	pb "github.com/lemon4ksan/g-man/protobuf/steam"
)

var (
	// ErrRefreshTokenRequired indicates authentication failed due to an empty refresh token.
	ErrRefreshTokenRequired = errors.New("websession: refresh token is required")
	// ErrTooManyRedirects indicates HTTP redirect iteration exceeded limits.
	ErrTooManyRedirects = errors.New("websession: stopped after 10 redirects (redirect loop)")
	// ErrSessionExpiredRedirect indicates the request was redirected to the login page.
	ErrSessionExpiredRedirect = errors.New("websession: session expired (redirected to login)")
	// ErrMissingTokenRefresher indicates that tokenRefresher is nil for platforms requiring CM socket token generation.
	ErrMissingTokenRefresher = errors.New("websession: missing TokenRefresher")
)

// MissingTokenRefresherError reports that a TokenRefresher callback is required for the platform.
type MissingTokenRefresherError struct {
	Platform pb.EAuthTokenPlatformType
}

func (e *MissingTokenRefresherError) Error() string {
	return fmt.Sprintf(
		"websession: missing TokenRefresher for platform %s (cannot use /jwt/finalizelogin)",
		e.Platform.String(),
	)
}

func (e *MissingTokenRefresherError) Is(target error) bool {
	return target == ErrMissingTokenRefresher
}

var (
	patternSteamIDFalse            = []byte("g_steamID = false;")
	patternSteamIDZero             = []byte(`g_steamID = "0";`)
	patternSignInTitle             = []byte("<title>Sign In</title>")
	patternLoggedInFalse           = []byte(`"Logged In":false`)
	patternLoggedInFalseSpace      = []byte(`"Logged In": false`)
	patternLowerLoggedInFalse      = []byte(`"logged in":false`)
	patternLowerLoggedInFalseSpace = []byte(`"logged in": false`)
)

func isSoftLogoutBody(body []byte) bool {
	if len(body) == 0 {
		return false
	}

	return bytes.Contains(body, patternSignInTitle) ||
		bytes.Contains(body, patternSteamIDFalse) ||
		bytes.Contains(body, patternSteamIDZero) ||
		bytes.Contains(body, patternLoggedInFalse) ||
		bytes.Contains(body, patternLoggedInFalseSpace) ||
		bytes.Contains(body, patternLowerLoggedInFalse) ||
		bytes.Contains(body, patternLowerLoggedInFalseSpace)
}

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
	cookieClientSessionID  = "clientsessionid"
	cookieSteamLoginSecure = "steamLoginSecure"
)

type refreshCtxKey struct{}

var inRefreshKey = &refreshCtxKey{}

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

	lastRefreshToken string
	lastAccessToken  string
	lastPlatform     pb.EAuthTokenPlatformType
	refreshCancel    context.CancelFunc

	refreshSF      *generic.SingleFlight[struct{}]
	tokenRefresher func(ctx context.Context, refreshToken string) (string, error)
}

// WithTokenRefresher sets a callback used to acquire fresh access tokens during background refresh for Client/Mobile tokens.
func (s *WebSession) WithTokenRefresher(fn func(ctx context.Context, refreshToken string) (string, error)) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.tokenRefresher = fn
}

type doerRoundTripper struct {
	doer aoni.HTTPDoer
}

func (d *doerRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	return d.doer.Do(req)
}

// New constructs an unauthenticated WebSession.
func New(steamID id.ID, logger log.Logger, doer any) *WebSession {
	ws := &WebSession{
		steamID:      steamID,
		baseDoer:     aoni.NewRequestDoerAdapter(aoni.Configure(doer, network.DefaultClientOptions()...)),
		logger:       logger.With(log.Module("websession")),
		retryBackoff: time.Second,
		refreshSF:    generic.NewSingleFlight[struct{}](),
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

// REST returns an aoni.Client wrapping the WebSession with retries and singleflight re-authentication.
func (s *WebSession) REST() *aoni.Client {
	s.mu.RLock()
	backoff := s.retryBackoff
	s.mu.RUnlock()

	retrier := middleware.Retry(middleware.RetryOptions{
		MaxRetries:     3,
		Backoff:        backoff,
		AllowedMethods: []string{"GET", "POST", "HEAD", "PUT", "DELETE"},
	}, middleware.RetryOnErr())

	reauth := middleware.ReAuth(middleware.ReAuthConfig{
		Trigger: func(resp aoni.Response, err error) bool {
			if err != nil {
				if errors.Is(err, ErrSessionExpiredRedirect) ||
					errors.Is(err, service.ErrSessionExpired) ||
					errors.Is(err, aoni.ErrRedirectBlocked) {
					return true
				}

				msg := strings.ToLower(err.Error())

				return strings.Contains(msg, "session expired") ||
					strings.Contains(msg, "redirected to login") ||
					strings.Contains(msg, "redirect blocked")
			}

			if resp != nil {
				status := resp.StatusCode()
				if status == http.StatusUnauthorized || status == http.StatusForbidden {
					return true
				}

				if status == http.StatusFound || status == http.StatusSeeOther {
					loc := resp.Header("Location")
					if strings.Contains(loc, "/login/home") || strings.Contains(loc, "/login") {
						return true
					}
				}

				if isSoftLogoutBody(resp.BodyBytes()) {
					return true
				}
			}

			return false
		},
		Refresh: func(ctx context.Context) error {
			return s.Refresh(ctx)
		},
	})

	s.mu.RLock()
	defer s.mu.RUnlock()

	return aoni.NewClient(middleware.Chain(s, reauth, retrier))
}

// rawREST returns an aoni.Client wrapping the WebSession with retries but WITHOUT ReAuth middleware.
// This is used internally during authentication, transfers, and verification to prevent mutual recursion.
func (s *WebSession) rawREST() *aoni.Client {
	s.mu.RLock()
	backoff := s.retryBackoff
	s.mu.RUnlock()

	retrier := middleware.Retry(middleware.RetryOptions{
		MaxRetries:     3,
		Backoff:        backoff,
		AllowedMethods: []string{"GET", "POST", "HEAD", "PUT", "DELETE"},
	}, middleware.RetryOnErr())

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

// DefaultRefreshInterval is the default period for updating WebSession cookies (every 6 hours).
const DefaultRefreshInterval = 6 * time.Hour

// Authenticate performs OIDC web finalization or fast-path cookie injection.
//
// Invariant: For SteamClient and MobileApp platforms, calling /jwt/finalizelogin fails with
// AccessDenied on Steam's backend. Authentication must obtain an access token via TokenRefresher
// (using the Steam CM socket connection) and inject it directly via applyFastPath.
//
// Parity: matches node steam-session and steam-user.
func (s *WebSession) Authenticate(
	ctx context.Context,
	platform pb.EAuthTokenPlatformType,
	refreshToken, accessToken string,
) error {
	if refreshToken == "" {
		return ErrRefreshTokenRequired
	}

	s.mu.Lock()
	s.lastRefreshToken = refreshToken
	s.lastAccessToken = accessToken
	s.lastPlatform = platform
	s.mu.Unlock()

	s.Clear()

	sessionID := generateSessionID()

	if platform == pb.EAuthTokenPlatformType_k_EAuthTokenPlatformType_SteamClient ||
		platform == pb.EAuthTokenPlatformType_k_EAuthTokenPlatformType_MobileApp {
		if accessToken == "" {
			s.mu.RLock()
			refresher := s.tokenRefresher
			s.mu.RUnlock()

			if refresher == nil {
				return &MissingTokenRefresherError{Platform: platform}
			}

			var err error

			accessToken, err = refresher(ctx, refreshToken)
			if err != nil {
				s.logger.Error("Failed to acquire access token via refresher", log.Err(err))
				return fmt.Errorf("websession: failed to acquire access token via refresher: %w", err)
			}

			if accessToken == "" {
				return errors.New("websession: token refresher returned empty access token")
			}

			s.mu.Lock()
			s.lastAccessToken = accessToken
			s.mu.Unlock()
		}

		return s.applyFastPath(accessToken, sessionID)
	}

	return s.authSlowPath(ctx, refreshToken, sessionID)
}

// Refresh re-authenticates the WebSession using stored credentials with singleflight deduplication.
func (s *WebSession) Refresh(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	if ctx.Value(inRefreshKey) != nil {
		return errors.New("websession: refresh recursion detected")
	}

	_, err := s.refreshSF.Do("refresh", func() (struct{}, error) {
		return struct{}{}, s.doRefresh(ctx)
	})

	return err
}

func (s *WebSession) doRefresh(ctx context.Context) error {
	ctx = context.WithValue(ctx, inRefreshKey, struct{}{})

	s.mu.RLock()
	refreshToken := s.lastRefreshToken
	platform := s.lastPlatform
	refresher := s.tokenRefresher
	s.mu.RUnlock()

	if refreshToken == "" {
		return ErrRefreshTokenRequired
	}

	s.logger.Info("Refreshing WebSession cookies...")

	accessToken := ""
	if platform == pb.EAuthTokenPlatformType_k_EAuthTokenPlatformType_SteamClient ||
		platform == pb.EAuthTokenPlatformType_k_EAuthTokenPlatformType_MobileApp {
		if refresher == nil {
			return &MissingTokenRefresherError{Platform: platform}
		}

		var err error

		accessToken, err = refresher(ctx, refreshToken)
		if err != nil {
			s.logger.Error("Failed to acquire fresh access token", log.Err(err))
			return err
		}
	}

	if err := s.Authenticate(ctx, platform, refreshToken, accessToken); err != nil {
		s.logger.Error("Failed to refresh WebSession cookies", log.Err(err))
		return err
	}

	s.logger.Info("Successfully refreshed WebSession cookies")

	return nil
}

// StartAutoRefresh begins a background loop that periodically re-authenticates the WebSession before cookies expire.
func (s *WebSession) StartAutoRefresh(ctx context.Context, interval time.Duration) {
	s.StopAutoRefresh()

	if interval <= 0 {
		interval = DefaultRefreshInterval
	}

	refreshCtx, cancel := context.WithCancel(ctx)

	s.mu.Lock()
	s.refreshCancel = cancel
	s.mu.Unlock()

	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		s.logger.Info("Started automatic WebSession refresh loop", log.Duration("interval", interval))

		for {
			select {
			case <-refreshCtx.Done():
				s.logger.Debug("Auto WebSession refresh loop stopped")
				return
			case <-ticker.C:
				if err := s.Refresh(refreshCtx); err != nil {
					s.logger.Warn("Periodic WebSession refresh attempt failed", log.Err(err))
				}
			}
		}
	}()
}

// StopAutoRefresh halts any active automatic refresh loop.
func (s *WebSession) StopAutoRefresh() {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.refreshCancel != nil {
		s.refreshCancel()
		s.refreshCancel = nil
	}
}

// Verify checks session validity by requesting Steam chat interface endpoints.
func (s *WebSession) Verify(ctx context.Context) (bool, error) {
	if !s.IsAuthenticated() {
		return false, nil
	}

	resp, err := s.rawREST().Request(ctx, http.MethodGet, urlVerify)
	if err != nil {
		if errors.Is(err, ErrSessionExpiredRedirect) ||
			errors.Is(err, aoni.ErrRedirectBlocked) ||
			errors.Is(err, service.ErrSessionExpired) ||
			aoni.IsUnauthorized(err) ||
			aoni.IsForbidden(err) {
			s.logger.Warn("WebSession verification failed: session expired or unauthorized", log.Err(err))
			s.Clear()
			return false, nil
		}

		s.logger.Error("WebSession verification encountered network error (cookies preserved)", log.Err(err))

		return false, fmt.Errorf("websession: verify failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		s.logger.Warn(
			"WebSession verification failed: status unauthorized or forbidden",
			log.Int("status", resp.StatusCode),
		)
		s.Clear()

		return false, nil
	}

	if resp.StatusCode == http.StatusFound || resp.StatusCode == http.StatusSeeOther {
		loc := resp.Header.Get("Location")
		if strings.Contains(loc, "/login/home") || strings.Contains(loc, "/login") {
			s.logger.Warn("WebSession verification failed: redirected to login", log.String("location", loc))
			s.Clear()

			return false, nil
		}
	}

	body, _ := io.ReadAll(resp.Body)
	if isSoftLogoutBody(body) {
		s.logger.Warn("WebSession verification failed: soft logout HTML/JSON detected")
		s.Clear()

		return false, nil
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

// ClientSessionID retrieves the 'clientsessionid' cookie value for a target URL string.
func (s *WebSession) ClientSessionID(targetURL string) string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	u, err := url.Parse(targetURL)
	if err != nil {
		return ""
	}

	for _, cookie := range s.jar.Cookies(u) {
		if cookie.Name == cookieClientSessionID {
			return cookie.Value
		}
	}

	return ""
}

// Cookies returns all cookies stored in the jar for the given URL.
func (s *WebSession) Cookies(u *url.URL) []*http.Cookie {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.jar == nil || u == nil {
		return nil
	}

	return s.jar.Cookies(u)
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

			if strings.Contains(req.URL.Path, "/login/home") || strings.Contains(req.URL.Path, "/login") {
				return ErrSessionExpiredRedirect
			}

			return nil
		},
	}
	s.isAuth = false
}

func (s *WebSession) applyFastPath(accessToken, sessionID string) error {
	secureCookieValue := FormatSteamLoginSecure(s.steamID, accessToken)
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

	res, err := s.rawREST().PostTo[finalizeResponse](ctx, urlFinalize, payload)
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

	resp, err := s.rawREST().PostTo[transferResp](ctx, transferURL, nil, mod.WithFormBody(params))
	if err != nil {
		return err
	}

	if resp.Result != enums.EResult_OK {
		return fmt.Errorf("steam error: %s", resp.Result.String())
	}

	return nil
}

// FormatSteamLoginSecure formats the steamLoginSecure cookie value per Steam / steam-session specification:
// encodeURIComponent([steamid64, accessToken].join('||'))
func FormatSteamLoginSecure(steamID id.ID, accessToken string) string {
	raw := fmt.Sprintf("%d||%s", steamID, accessToken)
	return encodeURIComponent(raw)
}

// encodeURIComponent escapes all characters except: A-Z a-z 0-9 - _ . ! ~ * ' ( )
// strictly matching JavaScript's global encodeURIComponent function per ECMA-262 §19.2.6.2.
func encodeURIComponent(s string) string {
	var buf strings.Builder
	buf.Grow(len(s))

	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c >= 'a' && c <= 'z') ||
			(c >= 'A' && c <= 'Z') ||
			(c >= '0' && c <= '9') ||
			c == '-' || c == '_' || c == '.' || c == '!' ||
			c == '~' || c == '*' || c == '\'' || c == '(' || c == ')' {
			buf.WriteByte(c)
		} else {
			fmt.Fprintf(&buf, "%%%02X", c)
		}
	}

	return buf.String()
}

// cookieDomainForHost determines the cookie Domain attribute for a given hostname
// so that subdomains within the same domain receive the cookies per RFC 6265 §5.3.
func cookieDomainForHost(host string) string {
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}

	lower := strings.ToLower(host)
	if strings.HasSuffix(lower, ".steampowered.com") || lower == "steampowered.com" {
		return "steampowered.com"
	}

	if strings.HasSuffix(lower, ".steamcommunity.com") || lower == "steamcommunity.com" {
		return "steamcommunity.com"
	}

	if strings.HasSuffix(lower, ".s.team") || lower == "s.team" {
		return "s.team"
	}

	return host
}

func buildCookiesForDomain(u *url.URL, sessionID, clientSessionID, secureValue string) []*http.Cookie {
	domain := cookieDomainForHost(u.Hostname())

	// #nosec G124 -- sessionid and clientsessionid intentionally HttpOnly: false for client-side JavaScript/CSRF parity
	cookies := []*http.Cookie{
		{
			Name:     cookieSessionID,
			Value:    sessionID,
			Path:     "/",
			Domain:   domain,
			Secure:   true,
			HttpOnly: false,
			SameSite: http.SameSiteLaxMode,
		},
		{
			Name:     cookieClientSessionID,
			Value:    clientSessionID,
			Path:     "/",
			Domain:   domain,
			Secure:   true,
			HttpOnly: false,
			SameSite: http.SameSiteLaxMode,
		},
	}
	if secureValue != "" {
		// #nosec G124: steamLoginSecure requires SameSite=None for Steam cross-site auth
		cookies = append(cookies, &http.Cookie{
			Name:     cookieSteamLoginSecure,
			Value:    secureValue,
			Path:     "/",
			Domain:   domain,
			Secure:   true,
			HttpOnly: true,
			SameSite: http.SameSiteNoneMode,
		})
	}

	return cookies
}

func (s *WebSession) seedCookies(sessionID, secureValue string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	clientSessionID := generateClientSessionID()

	for _, u := range s.domains {
		cookies := buildCookiesForDomain(u, sessionID, clientSessionID, secureValue)
		s.jar.SetCookies(u, cookies)
	}
}

// generateSessionID generates a random 12-byte hex string (24 characters).
//
// Invariant: Steam Web services require a 24-character hex string as the 'sessionid' cookie
// and form parameter for CSRF mitigation across community operations.
//
// Parity: matches node steamcommunity (index.js) and steam-tradeoffer-manager.
func generateSessionID() string {
	var b [12]byte

	if _, err := io.ReadFull(rand.Reader, b[:]); err != nil {
		_, _ = rand.Read(b[:])
	}

	return hex.EncodeToString(b[:])
}

// generateClientSessionID generates a random 8-byte hex string (16 characters).
//
// Invariant: Steam Community and chat interfaces expect a 'clientsessionid' cookie alongside
// 'sessionid' for client-side web sessions.
//
// Parity: matches node-steam-user (components/web.js).
func generateClientSessionID() string {
	var b [8]byte

	if _, err := io.ReadFull(rand.Reader, b[:]); err != nil {
		_, _ = rand.Read(b[:])
	}

	return hex.EncodeToString(b[:])
}

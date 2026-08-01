// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

var (
	ErrNotPlaying       = errors.New("spotify: no track is currently playing")
	ErrMissingToken     = errors.New("spotify: refresh token is required")
	ErrTokenFetchFailed = errors.New("spotify: failed to refresh access token")
	ErrUnexpectedStatus = errors.New("spotify: unexpected API response status")
)

const (
	tokenEndpoint       = "https://accounts.spotify.com/api/token" //nolint:gosec
	currentlyPlayingURL = "https://api.spotify.com/v1/me/player/currently-playing"
	defaultHTTPTimeout  = 10 * time.Second
)

// Config encapsulates Spotify OAuth2 client credentials and tokens.
type Config struct {
	ClientID     string
	ClientSecret string
	RefreshToken string
}

// Track represents metadata for the currently playing Spotify song.
type Track struct {
	Artist     string
	Title      string
	ProgressMS int64
	DurationMS int64
	IsPlaying  bool
}

// StatusString formats the track metadata into a Steam status string with audio icon and progress timing.
func (t *Track) StatusString() string {
	if t == nil || !t.IsPlaying || t.Title == "" {
		return ""
	}

	progSec := t.ProgressMS / 1000
	durSec := t.DurationMS / 1000

	var sb strings.Builder
	sb.Grow(64)

	fmt.Fprintf(&sb, "🎧 %s - %s [%02d:%02d/%02d:%02d]",
		t.Artist,
		t.Title,
		progSec/60, progSec%60,
		durSec/60, durSec%60,
	)

	return sb.String()
}

// Provider manages Spotify Web API interactions and token rotation.
//
// Thread Safety:
//   - Safe for concurrent access across goroutines.
type Provider struct {
	config Config

	tokenMu     sync.RWMutex
	accessToken string
	tokenExpiry time.Time
	httpClient  *http.Client
}

// NewProvider constructs a Spotify Provider using the given OAuth2 configurations.
func NewProvider(cfg Config) *Provider {
	return &Provider{
		config: cfg,
		httpClient: &http.Client{
			Timeout: defaultHTTPTimeout,
		},
	}
}

// FetchCurrentTrack queries the active Spotify playback state.
//
// Side Effects:
//   - Automatically refreshes the OAuth2 access token if expired.
func (p *Provider) FetchCurrentTrack(ctx context.Context) (*Track, error) {
	if err := p.ensureValidAccessToken(ctx); err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, currentlyPlayingURL, nil)
	if err != nil {
		return nil, err
	}

	p.tokenMu.RLock()
	token := p.accessToken
	p.tokenMu.RUnlock()

	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNoContent {
		return nil, ErrNotPlaying
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%w: %d", ErrUnexpectedStatus, resp.StatusCode)
	}

	var payload spotifyPlayerPayload
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, err
	}

	if !payload.IsPlaying || len(payload.Item.Artists) == 0 {
		return nil, ErrNotPlaying
	}

	artists := make([]string, 0, len(payload.Item.Artists))
	for _, a := range payload.Item.Artists {
		artists = append(artists, a.Name)
	}

	return &Track{
		Artist:     strings.Join(artists, ", "),
		Title:      payload.Item.Name,
		ProgressMS: payload.ProgressMS,
		DurationMS: payload.Item.DurationMS,
		IsPlaying:  payload.IsPlaying,
	}, nil
}

func (p *Provider) ensureValidAccessToken(ctx context.Context) error {
	p.tokenMu.RLock()

	if p.accessToken != "" && time.Now().Before(p.tokenExpiry) {
		p.tokenMu.RUnlock()
		return nil
	}

	p.tokenMu.RUnlock()

	p.tokenMu.Lock()
	defer p.tokenMu.Unlock()

	if p.accessToken != "" && time.Now().Before(p.tokenExpiry) {
		return nil
	}

	return p.refreshToken(ctx)
}

func (p *Provider) refreshToken(ctx context.Context) error {
	if p.config.RefreshToken == "" {
		return ErrMissingToken
	}

	form := url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {p.config.RefreshToken},
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tokenEndpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}

	authHeader := base64.StdEncoding.EncodeToString([]byte(p.config.ClientID + ":" + p.config.ClientSecret))
	req.Header.Set("Authorization", "Basic "+authHeader)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%w: status %d", ErrTokenFetchFailed, resp.StatusCode)
	}

	var tokenResp tokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
		return err
	}

	p.accessToken = tokenResp.AccessToken
	p.tokenExpiry = time.Now().Add(time.Duration(tokenResp.ExpiresIn-30) * time.Second)

	return nil
}

type spotifyPlayerPayload struct {
	IsPlaying  bool  `json:"is_playing"`
	ProgressMS int64 `json:"progress_ms"`
	Item       struct {
		Name       string `json:"name"`
		DurationMS int64  `json:"duration_ms"`
		Artists    []struct {
			Name string `json:"name"`
		} `json:"artists"`
	} `json:"item"`
}

type tokenResponse struct {
	AccessToken string `json:"access_token"`
	ExpiresIn   int    `json:"expires_in"`
}

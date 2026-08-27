// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package auth

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/lemon4ksan/foundation/async/logkit"

	"github.com/lemon4ksan/g-man/internal/crypto"
	"github.com/lemon4ksan/g-man/pkg/steam/socket"
	"github.com/lemon4ksan/g-man/pkg/storage"
)

var (
	// ErrNoCredentials indicates an auth flow was executed without credentials or tokens.
	ErrNoCredentials = errors.New("auth: no credentials, refresh token, or QR callback configured")
	// ErrServerFinderRequired indicates no Connection Manager server resolver was available.
	ErrServerFinderRequired = errors.New("auth: cm server resolver is required")
)

// FlowOption configures a declarative authentication flow.
type FlowOption func(*Flow)

// WithCredentials sets account login credentials.
func WithCredentials(accountName, password string) FlowOption {
	return func(f *Flow) {
		f.accountName = accountName
		f.password = password
	}
}

// WithSharedSecret sets the Base64/Hex shared secret for automatic Steam Guard 2FA TOTP code generation.
func WithSharedSecret(secret string) FlowOption {
	return func(f *Flow) {
		if dec, err := crypto.DecodeSecret(secret); err == nil {
			f.sharedSecret = dec
		}
	}
}

// WithSteamGuardCode sets an explicit 5-character Steam Guard 2FA auth code.
func WithSteamGuardCode(code string) FlowOption {
	return func(f *Flow) {
		f.guardCode = code
	}
}

// WithRefreshToken sets a previously obtained Steam JWT refresh token for fast session resume.
func WithRefreshToken(token string) FlowOption {
	return func(f *Flow) {
		f.refreshToken = token
	}
}

// WithAccessToken sets a pre-generated access token.
func WithAccessToken(token string) FlowOption {
	return func(f *Flow) {
		f.accessToken = token
	}
}

// WithFlowStorage configures persistent storage to automatically load and save refresh tokens.
func WithFlowStorage(s storage.KV) FlowOption {
	return func(f *Flow) {
		if s != nil {
			f.store = NewKVStore(s)
		}
	}
}

// WithQR configures a callback to handle Steam Mobile QR code login challenges.
func WithQR(onQR func(challengeURL string)) FlowOption {
	return func(f *Flow) {
		f.onQR = onQR
	}
}

// OnStateChange registers a callback invoked when the authentication state machine transitions.
func OnStateChange(callback func(oldState, newState State)) FlowOption {
	return func(f *Flow) {
		f.onStateChange = callback
	}
}

// WithServerFinder configures a custom CM server resolver.
func WithServerFinder(finder func(ctx context.Context) (socket.CMServer, error)) FlowOption {
	return func(f *Flow) {
		f.serverFinder = finder
	}
}

// WithTimeOffset configures clock drift compensation duration.
func WithTimeOffset(offset time.Duration) FlowOption {
	return func(f *Flow) {
		f.timeOffset = offset
	}
}

// LogOnRunner executes low-level network authentication against Steam Connection Managers.
type LogOnRunner interface {
	LogOn(ctx context.Context, details *LogOnDetails, server socket.CMServer) error
}

// Flow coordinates the multi-stage Steam authentication lifecycle.
type Flow struct {
	runner        LogOnRunner
	store         Store
	logger        logkit.Logger
	accountName   string
	password      string
	sharedSecret  []byte
	guardCode     string
	refreshToken  string
	accessToken   string
	timeOffset    time.Duration
	serverFinder  func(ctx context.Context) (socket.CMServer, error)
	onQR          func(challengeURL string)
	onStateChange func(oldState, newState State)
}

// NewFlow constructs a declarative authentication flow.
func NewFlow(runner LogOnRunner, opts ...FlowOption) *Flow {
	f := &Flow{
		runner: runner,
		logger: logkit.Discard,
	}

	for _, opt := range opts {
		opt(f)
	}

	return f
}

// Execute performs authentication according to configured credentials or tokens.
func (f *Flow) Execute(ctx context.Context) (*LogOnDetails, error) {
	// 1. Try saved refresh token if storage is available and token is not explicitly set
	if f.refreshToken == "" && f.store != nil && f.accountName != "" {
		if savedToken, err := f.store.GetRefreshToken(ctx, f.accountName); err == nil && savedToken != "" {
			f.refreshToken = savedToken
		}
	}

	// 2. Generate 2FA code if shared secret is provided and guardCode is not yet set
	if len(f.sharedSecret) > 0 && f.guardCode == "" {
		codeBytes := crypto.GenerateAuthCode(f.sharedSecret, time.Now().Add(f.timeOffset).Unix())
		f.guardCode = string(codeBytes[:])
	}

	// 3. Build LogOnDetails
	details := &LogOnDetails{
		AccountName:   f.accountName,
		Password:      f.password,
		AuthCode:      f.guardCode,
		TwoFactorCode: f.guardCode,
		AccessToken:   f.accessToken,
		RefreshToken:  f.refreshToken,
	}

	// 4. Resolve CM Server
	if f.serverFinder == nil {
		return nil, ErrServerFinderRequired
	}

	srv, err := f.serverFinder(ctx)
	if err != nil {
		return nil, fmt.Errorf("resolve cm server: %w", err)
	}

	// 5. Perform LogOn
	if err := f.runner.LogOn(ctx, details, srv); err != nil {
		return nil, err
	}

	// 6. Automatically save refresh token if storage is available
	if f.store != nil && details.AccountName != "" && details.RefreshToken != "" {
		_ = f.store.SaveRefreshToken(ctx, details.AccountName, details.RefreshToken)
	}

	return details, nil
}

// Resume attempts fast session re-establishment using a refresh token.
func (f *Flow) Resume(ctx context.Context, refreshToken string) (*LogOnDetails, error) {
	f.refreshToken = refreshToken
	return f.Execute(ctx)
}

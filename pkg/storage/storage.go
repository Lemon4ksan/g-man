// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package storage provides interfaces and implementations for persisting bot state.
package storage

import (
	"context"
	"errors"
)

var (
	// ErrNotFound is returned when a requested key does not exist in the storage.
	ErrNotFound = errors.New("storage: key not found")
)

// Provider is the master interface that a storage backend must implement.
// It acts as a factory for specific domain stores.
type Provider interface {
	// AuthStore returns a store dedicated to authentication data (tokens, cookies).
	AuthStore() AuthStore

	// KVStore returns a generic key-value store for arbitrary data.
	KVStore(namespace string) KVStore

	// Close cleanly shuts down the storage connection.
	Close() error
}

// AuthStore specifically handles persisting the Steam authentication state.
type AuthStore interface {
	// SaveRefreshToken persists the long-lived OIDC refresh token.
	SaveRefreshToken(ctx context.Context, accountName, token string) error

	// GetRefreshToken retrieves the refresh token for a specific account.
	GetRefreshToken(ctx context.Context, accountName string) (string, error)

	SaveMachineID(ctx context.Context, accountName string, machineID []byte) error
	GetMachineID(ctx context.Context, accountName string) ([]byte, error)

	// Clear removes all authentication data for the specified account.
	Clear(ctx context.Context, accountName string) error
}

// KVStore is a generic, string-to-bytes key-value store.
// The "namespace" concept allows separating data (e.g., "trading_known_offers" vs "tf2_schema_version").
type KVStore interface {
	// Set stores a value associated with a key.
	Set(ctx context.Context, key string, value []byte) error

	// Get retrieves a value by its key. Returns ErrNotFound if it doesn't exist.
	Get(ctx context.Context, key string) ([]byte, error)

	// Delete removes a key from the store.
	Delete(ctx context.Context, key string) error

	// Has returns true if the key exists.
	Has(ctx context.Context, key string) (bool, error)
}

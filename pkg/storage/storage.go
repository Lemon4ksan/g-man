// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package storage provides persistent, namespace-isolated key-value store interfaces.
package storage

import (
	"context"
	"errors"
)

// ErrNotFound indicates a requested storage key does not exist.
var ErrNotFound = errors.New("storage: key not found")

// Provider defines storage engine contracts.
type Provider interface {
	KV(namespace string) KV
	Close() error
}

// KV defines string-to-bytes key-value store contracts.
type KV interface {
	Set(ctx context.Context, key string, value []byte) error
	Get(ctx context.Context, key string) ([]byte, error)
	Delete(ctx context.Context, key string) error
	Has(ctx context.Context, key string) (bool, error)
	Keys(ctx context.Context, prefix string) ([]string, error)
}

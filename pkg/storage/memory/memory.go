// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package memory provides a high-performance in-memory storage provider implementation using lock-free concurrent maps.
package memory

import (
	"bytes"
	"context"
	"slices"
	"strings"

	"github.com/lemon4ksan/foundation/generic"

	"github.com/lemon4ksan/g-man/pkg/storage"
)

// Provider implements storage.Provider in memory using lock-free concurrent maps.
type Provider struct {
	kvStores generic.ConcurrentMap[string, *kvStore]
}

// New constructs an in-memory Provider.
func New() *Provider {
	return &Provider{}
}

func (p *Provider) KV(namespace string) storage.KV {
	if store, ok := p.kvStores.Load(namespace); ok {
		return store
	}

	newStore := &kvStore{}
	actual, _ := p.kvStores.LoadOrStore(namespace, newStore)

	return actual
}

func (p *Provider) Close() error {
	return nil
}

type kvStore struct {
	data generic.ConcurrentMap[string, []byte]
}

func (s *kvStore) Set(ctx context.Context, key string, value []byte) error {
	s.data.Store(key, bytes.Clone(value))
	return nil
}

func (s *kvStore) Get(ctx context.Context, key string) ([]byte, error) {
	if val, ok := s.data.Load(key); ok {
		return bytes.Clone(val), nil
	}

	return nil, storage.ErrNotFound
}

func (s *kvStore) Delete(ctx context.Context, key string) error {
	s.data.Delete(key)
	return nil
}

func (s *kvStore) Has(ctx context.Context, key string) (bool, error) {
	_, ok := s.data.Load(key)
	return ok, nil
}

func (s *kvStore) Keys(ctx context.Context, prefix string) ([]string, error) {
	var keys []string
	s.data.Range(func(k string, _ []byte) bool {
		if strings.HasPrefix(k, prefix) {
			keys = append(keys, k)
		}

		return true
	})

	slices.Sort(keys)

	return keys, nil
}

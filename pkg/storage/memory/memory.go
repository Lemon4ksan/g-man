// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package memory provides an in-memory storage provider.
package memory

import (
	"context"
	"sort"
	"strings"
	"sync"

	"github.com/lemon4ksan/g-man/pkg/storage"
)

// Provider implements [storage.Provider] using fast in-memory maps.
//
// All stored data is transient and exists only in memory. All state is lost
// permanently when the application shuts down.
// Create new instances of Provider using the [New] constructor.
type Provider struct {
	kvStores map[string]*kvStore
	mu       sync.Mutex
}

// New creates a new in-memory storage provider.
func New() *Provider {
	return &Provider{
		kvStores: make(map[string]*kvStore),
	}
}

// KV returns the key-value store for the given namespace.
func (p *Provider) KV(namespace string) storage.KV {
	p.mu.Lock()
	defer p.mu.Unlock()

	if store, ok := p.kvStores[namespace]; ok {
		return store
	}

	store := &kvStore{data: make(map[string][]byte)}
	p.kvStores[namespace] = store

	return store
}

// Close closes the provider.
func (p *Provider) Close() error {
	return nil
}

// --- KV Store Implementation ---

type kvStore struct {
	mu   sync.RWMutex
	data map[string][]byte
}

// Set adds a key-value pair to the store.
func (s *kvStore) Set(ctx context.Context, key string, value []byte) error {
	s.mu.Lock()

	s.data[key] = append([]byte(nil), value...) // Copy slice to prevent mutation
	s.mu.Unlock()

	return nil
}

// Get retrieves a value from the store by key.
func (s *kvStore) Get(ctx context.Context, key string) ([]byte, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if val, ok := s.data[key]; ok {
		return append([]byte(nil), val...), nil
	}

	return nil, storage.ErrNotFound
}

// Delete removes a key-value pair from the store.
func (s *kvStore) Delete(ctx context.Context, key string) error {
	s.mu.Lock()
	delete(s.data, key)
	s.mu.Unlock()

	return nil
}

// Has checks if a key exists in the store.
func (s *kvStore) Has(ctx context.Context, key string) (bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	_, ok := s.data[key]

	return ok, nil
}

// Keys returns all keys starting with the given prefix.
func (s *kvStore) Keys(ctx context.Context, prefix string) ([]string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var keys []string
	for k := range s.data {
		if strings.HasPrefix(k, prefix) {
			keys = append(keys, k)
		}
	}

	sort.Strings(keys)

	return keys, nil
}

// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package memory

import (
	"context"
	"sync"
	"time"

	"github.com/lemon4ksan/g-man/pkg/steam/auth"
	"github.com/lemon4ksan/g-man/pkg/storage"
)

// Provider implements [storage.Provider] using in-memory maps.
type Provider struct {
	authStore *authStore
	kvStores  map[string]*kvStore
	ttl       *TTLCache
	mu        sync.Mutex
}

func New() *Provider {
	return &Provider{
		authStore: &authStore{
			tokens:   make(map[string]string),
			machines: make(map[string][]byte),
		},
		kvStores: make(map[string]*kvStore),
		ttl:      NewTTLCache(),
	}
}

func (p *Provider) Auth() auth.Store {
	return p.authStore
}

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

func (p *Provider) TTLCache() *TTLCache {
	return p.ttl
}

func (p *Provider) Close() error {
	return nil
}

// --- Auth Store Implementation ---

type authStore struct {
	mu       sync.RWMutex
	tokens   map[string]string
	machines map[string][]byte
}

func (s *authStore) SaveRefreshToken(ctx context.Context, accountName, token string) error {
	s.mu.Lock()
	s.tokens[accountName] = token
	s.mu.Unlock()

	return nil
}

func (s *authStore) GetRefreshToken(ctx context.Context, accountName string) (string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if token, ok := s.tokens[accountName]; ok {
		return token, nil
	}

	return "", storage.ErrNotFound
}

func (s *authStore) SaveMachineID(ctx context.Context, accountName string, machineID []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.machines[accountName] = append([]byte(nil), machineID...)

	return nil
}

func (s *authStore) GetMachineID(ctx context.Context, accountName string) ([]byte, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if machineID, ok := s.machines[accountName]; ok {
		return machineID, nil
	}

	return nil, storage.ErrNotFound
}

func (s *authStore) Clear(ctx context.Context, accountName string) error {
	s.mu.Lock()
	delete(s.tokens, accountName)
	s.mu.Unlock()

	return nil
}

// --- KV Store Implementation ---

type kvStore struct {
	mu   sync.RWMutex
	data map[string][]byte
}

func (s *kvStore) Set(ctx context.Context, key string, value []byte) error {
	s.mu.Lock()

	s.data[key] = append([]byte(nil), value...) // Copy slice to prevent mutation
	s.mu.Unlock()

	return nil
}

func (s *kvStore) Get(ctx context.Context, key string) ([]byte, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if val, ok := s.data[key]; ok {
		return append([]byte(nil), val...), nil
	}

	return nil, storage.ErrNotFound
}

func (s *kvStore) Delete(ctx context.Context, key string) error {
	s.mu.Lock()
	delete(s.data, key)
	s.mu.Unlock()

	return nil
}

func (s *kvStore) Has(ctx context.Context, key string) (bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	_, ok := s.data[key]

	return ok, nil
}

// --- Time-to-Live Cache implementation ---

type entry struct {
	value      any
	expiration int64
}

type TTLCache struct {
	mu      sync.RWMutex
	entries map[string]entry
}

func NewTTLCache() *TTLCache {
	return &TTLCache{entries: make(map[string]entry)}
}

func (c *TTLCache) Set(key string, value any, ttl time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.entries[key] = entry{
		value:      value,
		expiration: time.Now().Add(ttl).UnixNano(),
	}
}

func (c *TTLCache) Get(key string) (any, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	e, ok := c.entries[key]
	if !ok || time.Now().UnixNano() > e.expiration {
		return nil, false
	}

	return e.value, true
}

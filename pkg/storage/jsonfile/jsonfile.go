// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package jsonfile provides a thread-safe, JSON-backed persistent key-value storage provider.
package jsonfile

import (
	"context"
	"os"
	"sort"
	"strings"
	"sync"

	json "github.com/goccy/go-json"

	"github.com/lemon4ksan/g-man/pkg/storage"
)

type dataLayout struct {
	KV map[string]map[string][]byte `json:"kv"`
}

// Provider implements storage.Provider backed by atomic file writes.
type Provider struct {
	path string
	mu   sync.RWMutex
	data dataLayout
}

// New constructs a Provider reading from path.
func New(path string) (*Provider, error) {
	p := &Provider{
		path: path,
		data: dataLayout{
			KV: make(map[string]map[string][]byte),
		},
	}

	if err := p.load(); err != nil {
		return nil, err
	}

	return p, nil
}

// KV returns a namespace-isolated key-value store.
func (p *Provider) KV(namespace string) storage.KV {
	return &kvStore{p, namespace}
}

// Close flushes in-memory storage data to disk.
func (p *Provider) Close() error {
	return p.save()
}

func (p *Provider) load() error {
	file, err := os.ReadFile(p.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}

		return err
	}

	if len(file) == 0 {
		return nil
	}

	return json.Unmarshal(file, &p.data)
}

func (p *Provider) save() error {
	bytes, err := json.MarshalIndent(p.data, "", "  ")
	if err != nil {
		return err
	}

	tmpPath := p.path + ".tmp"
	if err := os.WriteFile(tmpPath, bytes, 0o644); err != nil {
		return err
	}

	return os.Rename(tmpPath, p.path)
}

type kvStore struct {
	p         *Provider
	namespace string
}

func (s *kvStore) Set(ctx context.Context, key string, value []byte) error {
	s.p.mu.Lock()
	defer s.p.mu.Unlock()

	if s.p.data.KV[s.namespace] == nil {
		s.p.data.KV[s.namespace] = make(map[string][]byte)
	}

	s.p.data.KV[s.namespace][key] = value

	return s.p.save()
}

func (s *kvStore) Get(ctx context.Context, key string) ([]byte, error) {
	s.p.mu.RLock()
	defer s.p.mu.RUnlock()

	ns, ok := s.p.data.KV[s.namespace]
	if !ok {
		return nil, storage.ErrNotFound
	}

	val, ok := ns[key]
	if !ok {
		return nil, storage.ErrNotFound
	}

	return val, nil
}

func (s *kvStore) Delete(ctx context.Context, key string) error {
	s.p.mu.Lock()
	defer s.p.mu.Unlock()

	if ns, ok := s.p.data.KV[s.namespace]; ok {
		delete(ns, key)

		return s.p.save()
	}

	return nil
}

func (s *kvStore) Has(ctx context.Context, key string) (bool, error) {
	s.p.mu.RLock()
	defer s.p.mu.RUnlock()

	if ns, ok := s.p.data.KV[s.namespace]; ok {
		_, exists := ns[key]

		return exists, nil
	}

	return false, nil
}

func (s *kvStore) Keys(ctx context.Context, prefix string) ([]string, error) {
	s.p.mu.RLock()
	defer s.p.mu.RUnlock()

	ns, ok := s.p.data.KV[s.namespace]
	if !ok {
		return []string{}, nil
	}

	var keys []string
	for k := range ns {
		if strings.HasPrefix(k, prefix) {
			keys = append(keys, k)
		}
	}

	sort.Strings(keys)

	return keys, nil
}

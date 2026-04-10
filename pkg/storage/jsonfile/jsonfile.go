// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package jsonfile

import (
	"context"
	"encoding/json"
	"os"
	"sync"

	"github.com/lemon4ksan/g-man/pkg/steam/auth"
	"github.com/lemon4ksan/g-man/pkg/storage"
)

type dataLayout struct {
	Auth     map[string]string            `json:"auth"`
	Machines map[string][]byte            `json:"machines"`
	KV       map[string]map[string][]byte `json:"kv"`
}

// Provider implements storage.Provider, storing data in a single JSON file.
type Provider struct {
	path string
	mu   sync.RWMutex
	data dataLayout
}

// New creates a new JSON provider. If the file exists, it will be loaded.
func New(path string) (*Provider, error) {
	p := &Provider{
		path: path,
		data: dataLayout{
			Auth: make(map[string]string),
			KV:   make(map[string]map[string][]byte),
		},
	}

	if err := p.load(); err != nil {
		return nil, err
	}

	return p, nil
}

func (p *Provider) Auth() auth.Store {
	return &authStore{p}
}

func (p *Provider) KV(namespace string) storage.KV {
	return &kvStore{p, namespace}
}

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
	if err := os.WriteFile(tmpPath, bytes, 0644); err != nil {
		return err
	}

	return os.Rename(tmpPath, p.path)
}

type authStore struct{ p *Provider }

func (s *authStore) SaveRefreshToken(ctx context.Context, accountName, token string) error {
	s.p.mu.Lock()
	defer s.p.mu.Unlock()
	s.p.data.Auth[accountName] = token
	return s.p.save()
}

func (s *authStore) GetRefreshToken(ctx context.Context, accountName string) (string, error) {
	s.p.mu.RLock()
	defer s.p.mu.RUnlock()
	token, ok := s.p.data.Auth[accountName]
	if !ok {
		return "", storage.ErrNotFound
	}
	return token, nil
}

func (s *authStore) SaveMachineID(ctx context.Context, accountName string, machineID []byte) error {
	s.p.mu.Lock()
	defer s.p.mu.Unlock()
	if s.p.data.Machines == nil {
		s.p.data.Machines = make(map[string][]byte)
	}
	s.p.data.Machines[accountName] = append([]byte(nil), machineID...)
	return s.p.save()
}

func (s *authStore) GetMachineID(ctx context.Context, accountName string) ([]byte, error) {
	s.p.mu.RLock()
	defer s.p.mu.RUnlock()
	if s.p.data.Machines == nil {
		return nil, storage.ErrNotFound
	}
	machineID, ok := s.p.data.Machines[accountName]
	if !ok {
		return nil, storage.ErrNotFound
	}
	return append([]byte(nil), machineID...), nil
}

func (s *authStore) Clear(ctx context.Context, accountName string) error {
	s.p.mu.Lock()
	defer s.p.mu.Unlock()
	delete(s.p.data.Auth, accountName)
	return s.p.save()
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

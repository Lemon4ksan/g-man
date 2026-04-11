// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package memory_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/lemon4ksan/g-man/pkg/storage"
	"github.com/lemon4ksan/g-man/pkg/storage/memory"
)

func TestAuthStore(t *testing.T) {
	ctx := context.Background()
	p := memory.New()
	store := p.Auth()

	t.Run("Token Operations", func(t *testing.T) {
		account := "user1"
		token := "abc-123"

		_ = store.SaveRefreshToken(ctx, account, token)

		got, err := store.GetRefreshToken(ctx, account)
		if err != nil || got != token {
			t.Errorf("expected %s, got %s (err: %v)", token, got, err)
		}

		_ = store.Clear(ctx, account)

		_, err = store.GetRefreshToken(ctx, account)
		if !errors.Is(err, storage.ErrNotFound) {
			t.Error("expected ErrNotFound after clear")
		}
	})

	t.Run("MachineID Immutability", func(t *testing.T) {
		account := "user2"
		original := []byte{1, 2, 3}

		_ = store.SaveMachineID(ctx, account, original)

		original[0] = 99

		got, _ := store.GetMachineID(ctx, account)
		if got[0] == 99 {
			t.Error("store should return a copy, not a reference to the original slice")
		}
	})
}

func TestKVStore_Isolation(t *testing.T) {
	ctx := context.Background()
	p := memory.New()

	kv1 := p.KV("ns1")
	kv2 := p.KV("ns2")

	_ = kv1.Set(ctx, "key", []byte("val1"))
	_ = kv2.Set(ctx, "key", []byte("val2"))

	v1, _ := kv1.Get(ctx, "key")
	v2, _ := kv2.Get(ctx, "key")

	if string(v1) == string(v2) {
		t.Error("values in different namespaces should be isolated")
	}
}

func TestKVStore_Operations(t *testing.T) {
	ctx := context.Background()
	kv := memory.New().KV("test")

	t.Run("Set and Get", func(t *testing.T) {
		_ = kv.Set(ctx, "k", []byte("v"))

		val, _ := kv.Get(ctx, "k")
		if string(val) != "v" {
			t.Errorf("got %s", string(val))
		}
	})

	t.Run("Has", func(t *testing.T) {
		exists, _ := kv.Has(ctx, "k")
		if !exists {
			t.Error("key should exist")
		}

		exists, _ = kv.Has(ctx, "nonexistent")
		if exists {
			t.Error("key should not exist")
		}
	})

	t.Run("Delete", func(t *testing.T) {
		_ = kv.Delete(ctx, "k")

		_, err := kv.Get(ctx, "k")
		if !errors.Is(err, storage.ErrNotFound) {
			t.Error("expected ErrNotFound after delete")
		}
	})
}

func TestTTLCache(t *testing.T) {
	p := memory.New()
	cache := p.TTLCache()

	t.Run("Immediate Get", func(t *testing.T) {
		cache.Set("key1", "val1", time.Minute)

		val, ok := cache.Get("key1")
		if !ok || val != "val1" {
			t.Errorf("expected val1, ok=true; got %v, %v", val, ok)
		}
	})

	t.Run("Expiration", func(t *testing.T) {
		cache.Set("key2", "val2", 10*time.Millisecond)

		time.Sleep(20 * time.Millisecond)

		_, ok := cache.Get("key2")
		if ok {
			t.Error("expected key to be expired")
		}
	})

	t.Run("Overwrite TTL", func(t *testing.T) {
		cache.Set("key3", "old", time.Millisecond)
		cache.Set("key3", "new", time.Hour)

		time.Sleep(5 * time.Millisecond)

		val, ok := cache.Get("key3")
		if !ok || val != "new" {
			t.Error("new TTL should overwrite the expired one")
		}
	})
}

func TestMemory_Concurrency(t *testing.T) {
	p := memory.New()
	kv := p.KV("race")
	ctx := context.Background()

	const iterations = 100

	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()

		for i := 0; i < iterations; i++ {
			_ = kv.Set(ctx, "key", []byte("val"))
			_, _ = kv.Has(ctx, "key")
		}
	}()

	go func() {
		defer wg.Done()

		for i := 0; i < iterations; i++ {
			_, _ = kv.Get(ctx, "key")
			_ = kv.Delete(ctx, "key")
		}
	}()

	wg.Wait()
}

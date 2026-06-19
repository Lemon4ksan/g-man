// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package memory_test

import (
	"context"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lemon4ksan/g-man/pkg/steam/auth"
	"github.com/lemon4ksan/g-man/pkg/storage"
	"github.com/lemon4ksan/g-man/pkg/storage/memory"
)

func TestAuthStore(t *testing.T) {
	ctx := context.Background()
	p := memory.New()
	store := auth.NewKVStore(p.KV("auth"))

	t.Run("Token Operations", func(t *testing.T) {
		account := "user1"
		token := "abc-123"

		err := store.SaveRefreshToken(ctx, account, token)
		assert.NoError(t, err)

		got, err := store.GetRefreshToken(ctx, account)
		assert.NoError(t, err)
		assert.Equal(t, token, got)

		err = store.Clear(ctx, account)
		assert.NoError(t, err)

		_, err = store.GetRefreshToken(ctx, account)
		assert.ErrorIs(t, err, storage.ErrNotFound)
	})

	t.Run("MachineID Immutability", func(t *testing.T) {
		account := "user2"
		original := []byte{1, 2, 3}

		err := store.SaveMachineID(ctx, account, original)
		require.NoError(t, err)

		// Mutate the original slice
		original[0] = 99

		got, err := store.GetMachineID(ctx, account)
		assert.NoError(t, err)
		assert.NotEqual(t, uint8(99), got[0], "store should return a copy, not a reference")
		assert.Equal(t, uint8(1), got[0])
	})
}

func TestKVStore_Isolation(t *testing.T) {
	ctx := context.Background()
	p := memory.New()

	kv1 := p.KV("ns1")
	kv2 := p.KV("ns2")

	require.NoError(t, kv1.Set(ctx, "key", []byte("val1")))
	require.NoError(t, kv2.Set(ctx, "key", []byte("val2")))

	v1, err1 := kv1.Get(ctx, "key")
	v2, err2 := kv2.Get(ctx, "key")

	assert.NoError(t, err1)
	assert.NoError(t, err2)
	assert.NotEqual(t, string(v1), string(v2), "values in different namespaces should be isolated")
}

func TestKVStore_Operations(t *testing.T) {
	ctx := context.Background()
	kv := memory.New().KV("test")

	t.Run("Set and Get", func(t *testing.T) {
		err := kv.Set(ctx, "k", []byte("v"))
		assert.NoError(t, err)

		val, err := kv.Get(ctx, "k")
		assert.NoError(t, err)
		assert.Equal(t, "v", string(val))
	})

	t.Run("Has", func(t *testing.T) {
		exists, err := kv.Has(ctx, "k")
		assert.NoError(t, err)
		assert.True(t, exists)

		exists, err = kv.Has(ctx, "nonexistent")
		assert.NoError(t, err)
		assert.False(t, exists)
	})

	t.Run("Delete", func(t *testing.T) {
		err := kv.Delete(ctx, "k")
		assert.NoError(t, err)

		_, err = kv.Get(ctx, "k")
		assert.ErrorIs(t, err, storage.ErrNotFound)
	})

	t.Run("Keys", func(t *testing.T) {
		require.NoError(t, kv.Set(ctx, "prefix:item1", []byte("1")))
		require.NoError(t, kv.Set(ctx, "prefix:item2", []byte("2")))
		require.NoError(t, kv.Set(ctx, "other:item3", []byte("3")))

		keys, err := kv.Keys(ctx, "prefix:")
		assert.NoError(t, err)
		assert.Equal(t, []string{"prefix:item1", "prefix:item2"}, keys)

		keys, err = kv.Keys(ctx, "nonexistent:")
		assert.NoError(t, err)
		assert.Empty(t, keys)
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

		for range iterations {
			_ = kv.Set(ctx, "key", []byte("val"))
			_, _ = kv.Has(ctx, "key")
		}
	}()

	go func() {
		defer wg.Done()

		for range iterations {
			_, _ = kv.Get(ctx, "key")
			_ = kv.Delete(ctx, "key")
		}
	}()

	wg.Wait()
	// Test finishes successfully if no race condition is detected
}

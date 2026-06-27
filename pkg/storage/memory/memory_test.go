// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package memory_test

import (
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lemon4ksan/g-man/pkg/steam/auth"
	"github.com/lemon4ksan/g-man/pkg/storage"
	"github.com/lemon4ksan/g-man/pkg/storage/memory"
)

func TestProvider_Lifecycle(t *testing.T) {
	t.Parallel()

	t.Run("provider_close", func(t *testing.T) {
		t.Parallel()

		p := memory.New()
		err := p.Close()
		assert.NoError(t, err)
	})

	t.Run("cached_namespace_retrieval", func(t *testing.T) {
		t.Parallel()

		p := memory.New()
		kv1 := p.KV("cached")
		kv2 := p.KV("cached")
		assert.Equal(t, kv1, kv2)
	})
}

func TestAuthStore(t *testing.T) {
	t.Parallel()

	p := memory.New()
	store := auth.NewKVStore(p.KV("auth"))

	t.Run("token_operations", func(t *testing.T) {
		t.Parallel()

		account := "user1"
		token := "abc-123"

		err := store.SaveRefreshToken(t.Context(), account, token)
		assert.NoError(t, err)

		got, err := store.GetRefreshToken(t.Context(), account)
		assert.NoError(t, err)
		assert.Equal(t, token, got)

		err = store.Clear(t.Context(), account)
		assert.NoError(t, err)

		_, err = store.GetRefreshToken(t.Context(), account)
		assert.ErrorIs(t, err, storage.ErrNotFound)
	})

	t.Run("machine_id_immutability", func(t *testing.T) {
		t.Parallel()

		account := "user2"
		original := []byte{1, 2, 3}

		err := store.SaveMachineID(t.Context(), account, original)
		require.NoError(t, err)

		// Mutate the original slice
		original[0] = 99

		got, err := store.GetMachineID(t.Context(), account)
		assert.NoError(t, err)
		assert.NotEqual(t, uint8(99), got[0], "store should return a copy, not a reference")
		assert.Equal(t, uint8(1), got[0])
	})
}

func TestKVStore_Isolation(t *testing.T) {
	t.Parallel()

	p := memory.New()

	kv1 := p.KV("ns1")
	kv2 := p.KV("ns2")

	require.NoError(t, kv1.Set(t.Context(), "key", []byte("val1")))
	require.NoError(t, kv2.Set(t.Context(), "key", []byte("val2")))

	v1, err1 := kv1.Get(t.Context(), "key")
	v2, err2 := kv2.Get(t.Context(), "key")

	assert.NoError(t, err1)
	assert.NoError(t, err2)
	assert.NotEqual(t, string(v1), string(v2), "values in different namespaces should be isolated")
}

func TestKVStore_Operations(t *testing.T) {
	t.Parallel()

	kv := memory.New().KV("test")

	t.Run("set_and_get", func(t *testing.T) {
		err := kv.Set(t.Context(), "k", []byte("v"))
		assert.NoError(t, err)

		val, err := kv.Get(t.Context(), "k")
		assert.NoError(t, err)
		assert.Equal(t, "v", string(val))
	})

	t.Run("has", func(t *testing.T) {
		exists, err := kv.Has(t.Context(), "k")
		assert.NoError(t, err)
		assert.True(t, exists)

		exists, err = kv.Has(t.Context(), "nonexistent")
		assert.NoError(t, err)
		assert.False(t, exists)
	})

	t.Run("delete", func(t *testing.T) {
		err := kv.Delete(t.Context(), "k")
		assert.NoError(t, err)

		_, err = kv.Get(t.Context(), "k")
		assert.ErrorIs(t, err, storage.ErrNotFound)
	})

	t.Run("keys", func(t *testing.T) {
		require.NoError(t, kv.Set(t.Context(), "prefix:item1", []byte("1")))
		require.NoError(t, kv.Set(t.Context(), "prefix:item2", []byte("2")))
		require.NoError(t, kv.Set(t.Context(), "other:item3", []byte("3")))

		keys, err := kv.Keys(t.Context(), "prefix:")
		assert.NoError(t, err)
		assert.Equal(t, []string{"prefix:item1", "prefix:item2"}, keys)

		keys, err = kv.Keys(t.Context(), "nonexistent:")
		assert.NoError(t, err)
		assert.Empty(t, keys)
	})
}

func TestMemory_Concurrency(t *testing.T) {
	t.Parallel()

	p := memory.New()
	kv := p.KV("race")
	ctx := t.Context()

	const iterations = 100

	var wg sync.WaitGroup

	wg.Go(func() {
		for range iterations {
			_ = kv.Set(ctx, "key", []byte("val"))
			_, _ = kv.Has(ctx, "key")
		}
	})

	wg.Go(func() {
		for range iterations {
			_, _ = kv.Get(ctx, "key")
			_ = kv.Delete(ctx, "key")
		}
	})

	wg.Wait()
}

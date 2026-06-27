// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package jsonfile_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lemon4ksan/g-man/pkg/steam/auth"
	"github.com/lemon4ksan/g-man/pkg/storage"
	"github.com/lemon4ksan/g-man/pkg/storage/jsonfile"
)

func TestProvider_Persistence(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "storage.json")
	ctx := t.Context()

	p1, err := jsonfile.New(dbPath)
	require.NoError(t, err, "failed to create provider")

	account := "test_user"
	token := "v1_refresh_token_xyz"

	err = auth.NewKVStore(p1.KV("auth")).SaveRefreshToken(ctx, account, token)
	require.NoError(t, err, "failed to save token")

	err = p1.Close()
	require.NoError(t, err, "failed to close p1")

	// Reload from same path
	p2, err := jsonfile.New(dbPath)
	require.NoError(t, err, "failed to reload provider")

	got, err := auth.NewKVStore(p2.KV("auth")).GetRefreshToken(ctx, account)
	assert.NoError(t, err, "failed to get token after reload")
	assert.Equal(t, token, got)
}

func TestAuthStore_MachineID(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "auth.json")
	ctx := t.Context()

	p, err := jsonfile.New(dbPath)
	require.NoError(t, err)

	store := auth.NewKVStore(p.KV("auth"))

	account := "bot_01"
	machineID := []byte{0xDE, 0xAD, 0xBE, 0xEF}

	t.Run("save_and_get", func(t *testing.T) {
		t.Parallel()

		err := store.SaveMachineID(ctx, account, machineID)
		assert.NoError(t, err)

		got, err := store.GetMachineID(ctx, account)
		assert.NoError(t, err)
		assert.Equal(t, machineID, got, "machine ID mismatch")
	})

	t.Run("not_found", func(t *testing.T) {
		t.Parallel()

		_, err := store.GetMachineID(ctx, "non_existent")
		assert.ErrorIs(t, err, storage.ErrNotFound)
	})

	t.Run("clear", func(t *testing.T) {
		t.Parallel()

		err := store.Clear(ctx, account)
		assert.NoError(t, err)

		_, err = store.GetRefreshToken(ctx, account)
		assert.ErrorIs(t, err, storage.ErrNotFound, "expected token to be cleared")
	})
}

func TestKVStore(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "kv.json")
	ctx := t.Context()

	p, err := jsonfile.New(dbPath)
	require.NoError(t, err)

	kv1 := p.KV("settings")
	kv2 := p.KV("cache")

	t.Run("namespace_isolation", func(t *testing.T) {
		t.Parallel()
		require.NoError(t, kv1.Set(ctx, "theme", []byte("dark")))
		require.NoError(t, kv2.Set(ctx, "theme", []byte("light")))

		v1, _ := kv1.Get(ctx, "theme")
		v2, _ := kv2.Get(ctx, "theme")

		assert.NotEqual(t, string(v1), string(v2), "namespaces should be isolated")
	})

	t.Run("crud_operations", func(t *testing.T) {
		t.Parallel()

		key := "my_key"
		val := []byte("my_value")

		// Has (false)
		exists, err := kv1.Has(ctx, key)
		assert.NoError(t, err)
		assert.False(t, exists)

		// Set & Get
		err = kv1.Set(ctx, key, val)
		assert.NoError(t, err)

		got, err := kv1.Get(ctx, key)
		assert.NoError(t, err)
		assert.Equal(t, val, got)

		// Has (true)
		exists, err = kv1.Has(ctx, key)
		assert.NoError(t, err)
		assert.True(t, exists)

		// Delete
		err = kv1.Delete(ctx, key)
		assert.NoError(t, err)

		_, err = kv1.Get(ctx, key)
		assert.ErrorIs(t, err, storage.ErrNotFound)
	})

	t.Run("keys", func(t *testing.T) {
		t.Parallel()
		require.NoError(t, kv1.Set(ctx, "prefix:item1", []byte("1")))
		require.NoError(t, kv1.Set(ctx, "prefix:item2", []byte("2")))
		require.NoError(t, kv1.Set(ctx, "other:item3", []byte("3")))

		keys, err := kv1.Keys(ctx, "prefix:")
		assert.NoError(t, err)
		assert.Equal(t, []string{"prefix:item1", "prefix:item2"}, keys)

		keys, err = kv1.Keys(ctx, "nonexistent:")
		assert.NoError(t, err)
		assert.Empty(t, keys)
	})
}

func TestProvider_EmptyFile(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "empty.json")

	err := os.WriteFile(dbPath, []byte(""), 0o644)
	require.NoError(t, err)

	p, err := jsonfile.New(dbPath)
	assert.NoError(t, err, "provider should handle empty files")
	assert.NotNil(t, p)
}

func TestProvider_CorruptedFile(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "corrupted.json")

	err := os.WriteFile(dbPath, []byte("{ invalid json"), 0o644)
	require.NoError(t, err)

	_, err = jsonfile.New(dbPath)
	assert.Error(t, err, "expected error when loading corrupted JSON")
}

func TestProvider_Errors(t *testing.T) {
	t.Parallel()

	t.Run("load_directory_error", func(t *testing.T) {
		t.Parallel()
		// Passing a directory path instead of a file path will trigger
		// a read directory error from os.ReadFile (e.g. "is a directory")
		_, err := jsonfile.New(t.TempDir())
		assert.Error(t, err)
	})

	t.Run("save_write_error", func(t *testing.T) {
		t.Parallel()
		// Passing a path inside a non-existent directory succeeds in New/load()
		// because it returns NotExist (which returns nil),
		// but fails on subsequent save() attempts during Set()
		p, err := jsonfile.New("/invalid_dir/db.json")
		require.NoError(t, err)

		err = p.KV("test").Set(t.Context(), "key", []byte("val"))
		assert.Error(t, err)
	})

	t.Run("uninitialized_namespace_operations", func(t *testing.T) {
		t.Parallel()
		tmpDir := t.TempDir()
		dbPath := filepath.Join(tmpDir, "uninit.json")
		p, err := jsonfile.New(dbPath)
		require.NoError(t, err)

		kv := p.KV("never_existed")

		_, err = kv.Get(t.Context(), "key")
		assert.ErrorIs(t, err, storage.ErrNotFound)

		exists, err := kv.Has(t.Context(), "key")
		assert.NoError(t, err)
		assert.False(t, exists)

		err = kv.Delete(t.Context(), "key")
		assert.NoError(t, err)

		keys, err := kv.Keys(t.Context(), "prefix:")
		assert.NoError(t, err)
		assert.Empty(t, keys)
	})
}

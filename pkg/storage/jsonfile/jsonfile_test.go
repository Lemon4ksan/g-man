// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package jsonfile_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lemon4ksan/g-man/pkg/storage"
	"github.com/lemon4ksan/g-man/pkg/storage/jsonfile"
)

func TestProvider_Persistence(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "storage.json")
	ctx := context.Background()

	p1, err := jsonfile.New(dbPath)
	require.NoError(t, err, "failed to create provider")

	account := "test_user"
	token := "v1_refresh_token_xyz"

	err = p1.Auth().SaveRefreshToken(ctx, account, token)
	require.NoError(t, err, "failed to save token")

	err = p1.Close()
	require.NoError(t, err, "failed to close p1")

	// Reload from same path
	p2, err := jsonfile.New(dbPath)
	require.NoError(t, err, "failed to reload provider")

	got, err := p2.Auth().GetRefreshToken(ctx, account)
	assert.NoError(t, err, "failed to get token after reload")
	assert.Equal(t, token, got)
}

func TestAuthStore_MachineID(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "auth.json")
	ctx := context.Background()

	p, err := jsonfile.New(dbPath)
	require.NoError(t, err)

	store := p.Auth()

	account := "bot_01"
	machineID := []byte{0xDE, 0xAD, 0xBE, 0xEF}

	t.Run("Save and Get", func(t *testing.T) {
		err := store.SaveMachineID(ctx, account, machineID)
		assert.NoError(t, err)

		got, err := store.GetMachineID(ctx, account)
		assert.NoError(t, err)
		assert.Equal(t, machineID, got, "machine ID mismatch")
	})

	t.Run("Not Found", func(t *testing.T) {
		_, err := store.GetMachineID(ctx, "non_existent")
		assert.ErrorIs(t, err, storage.ErrNotFound)
	})

	t.Run("Clear", func(t *testing.T) {
		err := store.Clear(ctx, account)
		assert.NoError(t, err)

		_, err = store.GetRefreshToken(ctx, account)
		assert.ErrorIs(t, err, storage.ErrNotFound, "expected token to be cleared")
	})
}

func TestKVStore(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "kv.json")
	ctx := context.Background()

	p, err := jsonfile.New(dbPath)
	require.NoError(t, err)

	kv1 := p.KV("settings")
	kv2 := p.KV("cache")

	t.Run("Namespace Isolation", func(t *testing.T) {
		require.NoError(t, kv1.Set(ctx, "theme", []byte("dark")))
		require.NoError(t, kv2.Set(ctx, "theme", []byte("light")))

		v1, _ := kv1.Get(ctx, "theme")
		v2, _ := kv2.Get(ctx, "theme")

		assert.NotEqual(t, string(v1), string(v2), "namespaces should be isolated")
	})

	t.Run("CRUD Operations", func(t *testing.T) {
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
}

func TestProvider_EmptyFile(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "empty.json")

	err := os.WriteFile(dbPath, []byte(""), 0o644)
	require.NoError(t, err)

	p, err := jsonfile.New(dbPath)
	assert.NoError(t, err, "provider should handle empty files")
	assert.NotNil(t, p)
}

func TestProvider_CorruptedFile(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "corrupted.json")

	err := os.WriteFile(dbPath, []byte("{ invalid json"), 0o644)
	require.NoError(t, err)

	_, err = jsonfile.New(dbPath)
	assert.Error(t, err, "expected error when loading corrupted JSON")
}

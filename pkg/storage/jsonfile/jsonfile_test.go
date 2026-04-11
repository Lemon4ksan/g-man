// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package jsonfile_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/lemon4ksan/g-man/pkg/storage"
	"github.com/lemon4ksan/g-man/pkg/storage/jsonfile"
)

func TestProvider_Persistence(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "storage.json")
	ctx := context.Background()

	p1, err := jsonfile.New(dbPath)
	if err != nil {
		t.Fatalf("failed to create provider: %v", err)
	}

	account := "test_user"
	token := "v1_refresh_token_xyz"

	err = p1.Auth().SaveRefreshToken(ctx, account, token)
	if err != nil {
		t.Fatalf("failed to save token: %v", err)
	}

	if err := p1.Close(); err != nil {
		t.Fatalf("failed to close p1: %v", err)
	}

	p2, err := jsonfile.New(dbPath)
	if err != nil {
		t.Fatalf("failed to reload provider: %v", err)
	}

	got, err := p2.Auth().GetRefreshToken(ctx, account)
	if err != nil {
		t.Errorf("failed to get token after reload: %v", err)
	}

	if got != token {
		t.Errorf("expected token %s, got %s", token, got)
	}
}

func TestAuthStore_MachineID(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "auth.json")
	ctx := context.Background()

	p, _ := jsonfile.New(dbPath)
	store := p.Auth()

	account := "bot_01"
	machineID := []byte{0xDE, 0xAD, 0xBE, 0xEF}

	t.Run("Save and Get", func(t *testing.T) {
		if err := store.SaveMachineID(ctx, account, machineID); err != nil {
			t.Fatal(err)
		}

		got, err := store.GetMachineID(ctx, account)
		if err != nil {
			t.Fatal(err)
		}

		if string(got) != string(machineID) {
			t.Errorf("machine ID mismatch: %v", got)
		}
	})

	t.Run("Not Found", func(t *testing.T) {
		_, err := store.GetMachineID(ctx, "non_existent")
		if err != storage.ErrNotFound {
			t.Errorf("expected ErrNotFound, got %v", err)
		}
	})

	t.Run("Clear", func(t *testing.T) {
		_ = store.Clear(ctx, account)
		_, err := store.GetRefreshToken(ctx, account)
		if err != storage.ErrNotFound {
			t.Error("expected token to be cleared")
		}
	})
}

func TestKVStore(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "kv.json")
	ctx := context.Background()

	p, _ := jsonfile.New(dbPath)

	kv1 := p.KV("settings")
	kv2 := p.KV("cache")

	t.Run("Namespace Isolation", func(t *testing.T) {
		_ = kv1.Set(ctx, "theme", []byte("dark"))
		_ = kv2.Set(ctx, "theme", []byte("light"))

		v1, _ := kv1.Get(ctx, "theme")
		v2, _ := kv2.Get(ctx, "theme")

		if string(v1) == string(v2) {
			t.Error("namespaces should be isolated")
		}
	})

	t.Run("CRUD Operations", func(t *testing.T) {
		key := "my_key"
		val := []byte("my_value")

		// Has (false)
		exists, _ := kv1.Has(ctx, key)
		if exists {
			t.Error("key should not exist yet")
		}

		// Set & Get
		_ = kv1.Set(ctx, key, val)
		got, _ := kv1.Get(ctx, key)
		if string(got) != string(val) {
			t.Error("value mismatch")
		}

		// Has (true)
		exists, _ = kv1.Has(ctx, key)
		if !exists {
			t.Error("key should exist")
		}

		// Delete
		_ = kv1.Delete(ctx, key)
		_, err := kv1.Get(ctx, key)
		if err != storage.ErrNotFound {
			t.Error("key should be deleted")
		}
	})
}

func TestProvider_EmptyFile(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "empty.json")

	if err := os.WriteFile(dbPath, []byte(""), 0644); err != nil {
		t.Fatal(err)
	}

	p, err := jsonfile.New(dbPath)
	if err != nil {
		t.Fatalf("provider should handle empty files: %v", err)
	}

	if p == nil {
		t.Fatal("provider is nil")
	}
}

func TestProvider_CorruptedFile(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "corrupted.json")

	if err := os.WriteFile(dbPath, []byte("{ invalid json"), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := jsonfile.New(dbPath)
	if err == nil {
		t.Error("expected error when loading corrupted JSON")
	}
}

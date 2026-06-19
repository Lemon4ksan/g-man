// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package storage provides interfaces and implementations for persisting bot state.
//
// # Key Components
//
// - [Provider]: The master interface implemented by all storage backends.
// - [KV]: A generic key-value store isolated by distinct namespaces.
// - [jsonfile.Provider]: An implementation that persists data to a single JSON file.
// - [memory.Provider]: A transient, fast in-memory storage implementation.
//
// # Basic Usage Example
//
//	package main
//
//	import (
//		"context"
//		"fmt"
//		"github.com/lemon4ksan/g-man/pkg/storage/jsonfile"
//	)
//
//	func main() {
//		ctx := context.Background()
//
//		// Initialize a JSON file storage provider
//		provider, err := jsonfile.New("storage.json")
//		if err != nil {
//			fmt.Println("Initialization failed:", err)
//			return
//		}
//		defer provider.Close()
//
//		// Retrieve a namespace-isolated KV store
//		kv := provider.KV("app_config")
//		_ = kv.Set(ctx, "theme", []byte("dark"))
//	}
package storage

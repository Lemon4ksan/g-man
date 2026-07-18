// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package storage persists bot state using namespace-isolated key-value stores.
//
// Use the [Provider] interface to manage database lifecycles and spawn independent [KV] stores.
// Each [KV] store is bound to a specific namespace, preventing key collisions between unrelated modules.
//
// # Quick Start
//
// Create a JSON-backed store and update a session key:
//
//	db, _ := jsonfile.New("storage.json")
//	defer db.Close()
//
//	kv := db.KV("auth")
//	_ = kv.Set(ctx, "session_id", data)
package storage

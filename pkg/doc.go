// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package pkg provides the entry point and public API surface of the G-man SDK—a
// modular, high-performance toolkit for building automated Steam services.
//
// G-man is designed as a set of decoupled components, allowing you to choose
// the level of control you need—from low-level socket connections to high-level
// trading and account behaviors.
//
// # Key Packages
//
//   - [pkg/steam]: Connects to the Steam network, handles authentication, and dispatches messages.
//   - [pkg/storage]: Persists bot state using namespace-isolated key-value stores.
//   - [pkg/trading/engine]: Evaluates and processes trade offers using middleware logic.
//   - [pkg/behavior]: Orchestrates automated user routines like Steam Guard confirmations and achievements.
//
// # Design Guarantees
//
//   - Thread Safety: All service clients and engine coordinators are safe for concurrent use.
//   - Context Support: Network and blocking operations accept a context.Context for timeout and cancellation control.
//   - Structured Errors: Protocol and transport failures are categorized into typed Go errors, avoiding hidden failures.
package pkg

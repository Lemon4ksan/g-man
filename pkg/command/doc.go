// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package command coordinates registration, validation, parsing, and execution of text commands.
//
// It supports raw string handlers, unified typed slice handlers, and dynamic, reflection-based
// custom signatures.
//
// # Key Components
//
//   - [Engine]: The central orchestrator used to register custom type parsers, validate schemas, and execute commands.
//   - [Command]: Wraps execution handlers with structural arguments schemas and permission scopes.
//   - [Caller]: Defines an interface representing the identity executing a command.
//   - [ArgSchema]: Defines the name, type, and optionality of an individual command argument.
//
// # Basic Usage Example
//
// Here is a complete, self-contained example demonstrating how to register and execute a basic command:
//
//	package main
//
//	import (
//		"context"
//		"fmt"
//		"github.com/lemon4ksan/g-man/pkg/command"
//	)
//
//	// MockCaller implements command.Caller for testing.
//	type MockCaller struct{}
//
//	func (c MockCaller) ID() string          { return "console" }
//	func (c MockCaller) DisplayName() string { return "System Console" }
//	func (c MockCaller) IsAdmin() bool       { return true }
//
//	func main() {
//		engine := command.NewEngine()
//
//		// Register a basic command
//		engine.Register("ping", func(ctx context.Context, args []string) (string, error) {
//			return "pong", nil
//		}, command.WithDescription("Responds with pong"))
//
//		ctx := command.WithCaller(context.Background(), MockCaller{})
//
//		// Execute the command
//		res, err := engine.Execute(ctx, "ping")
//		if err != nil {
//			fmt.Println("Error:", err)
//			return
//		}
//		fmt.Println("Result:", res)
//	}
package command

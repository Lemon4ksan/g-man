// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package jobs provides a concurrent-safe mechanism for tracking asynchronous
// request-response cycles.
//
// It is designed for building protocol implementations (like TCP, UDP, or WebSockets)
// where a request is sent with a unique correlation ID, and a response is expected
// later. The package handles job lifecycle, including timeouts, context
// cancellation, and synchronous waiting.
//
// # Key Components
//
//   - [Manager]: The central orchestrator that coordinates job registration,
//     resolution, and lifetime.
//   - [Callback]: A function signature used to process job results asynchronously.
//   - [Option]: A configuration function used to customize timeouts and contexts.
//
// # Basic Usage (Callback style)
//
//	mgr := jobs.NewManager[string](100)
//	id := mgr.NextID()
//
//	err := mgr.Add(id, func(res string, err error) {
//		if err != nil {
//			log.Printf("Job %d failed: %v", id, err)
//			return
//		}
//		fmt.Printf("Job %d received: %s", id, res)
//	})
//
//	// Somewhere else when response arrives:
//	mgr.Resolve(id, "Hello World", nil)
//
// # Synchronous Usage (Blocking style)
//
//	mgr := jobs.NewManager[string](0)
//	id := mgr.NextID()
//
//	// Add with WithWait option to enable WaitFor
//	mgr.Add(id, nil, jobs.WithWait[string](), jobs.WithTimeout[string](time.Second))
//
//	// Block until response or timeout
//	res, err := mgr.WaitFor(context.Background(), id)
//	if err != nil {
//		log.Fatal(err)
//	}
package jobs

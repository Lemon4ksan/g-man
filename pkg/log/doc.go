// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

/*
Package log provides a high-performance, asynchronous, structured logger
designed for both human readability and machine efficiency.

The logger uses a non-blocking architecture where log messages are formatted
in the calling goroutine and then sent to a background worker via a fixed-size
buffer (channel). This ensures that logging operations have minimal impact
on the latency of the main application logic.

# Structured Logging

Structured logging is achieved through the use of Fields. Rather than formatting
strings manually, you provide a message and a set of typed fields:

	logger.Info("user logged in",
	    log.String("username", "john_doe"),
	    log.Int("attempts", 3),
	)

# Module Hierarchy

The logger supports a hierarchical module system. You can create sub-loggers
that represent different components of your system. Depending on the Config,
these will be displayed as a tree or as a full path string.

	root := log.New(log.DefaultConfig(log.InfoLevel))
	apiLogger := root.With(log.Module("API"))
	dbLogger := apiLogger.With(log.Component("Database"))

	dbLogger.Info("connection established")
	// Output: 12:00:00.000 [I]    └─ API └─ Database connection established

# Asynchronous Nature and Shutdown

Because logging is asynchronous, it is critical to call Close() before the
application exits. This flushes the internal queue and ensures all pending
messages are written to the output.

	l := log.New(log.DefaultConfig(log.InfoLevel))
	defer l.Close()

# Performance

The package is optimized to reduce GC pressure by using sync.Pool for byte
buffers and specialized formatters (strconv) instead of fmt.Sprintf where
possible.
*/
package log

// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package socket

import "github.com/lemon4ksan/aoni/fast"

// DefaultMaxHeartbeatFailures defines the consecutive failed heartbeats before transport reset.
const DefaultMaxHeartbeatFailures = 3

// Config configures the Steam socket subsystem.
type Config struct {
	FastClient           *fast.Client
	Connector            ConnectorConfig
	Processor            ProcessorConfig
	MaxJobs              int
	MaxHeartbeatFailures int
}

// DefaultConfig builds recommended socket subsystem settings.
func DefaultConfig() Config {
	return Config{
		Connector:            DefaultConnectorConfig(),
		Processor:            DefaultProcessorConfig(),
		MaxJobs:              1000,
		MaxHeartbeatFailures: DefaultMaxHeartbeatFailures,
	}
}

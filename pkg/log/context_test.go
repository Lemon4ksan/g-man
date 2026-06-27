// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package log_test

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/lemon4ksan/g-man/pkg/log"
)

func TestCorrelationID(t *testing.T) {
	t.Parallel()

	ctx := t.Context()

	id, ok := log.CorrelationID(ctx)
	assert.False(t, ok)
	assert.Empty(t, id)

	testID := log.GenerateCorrelationID()
	assert.Len(t, testID, 32)

	ctx = log.WithCorrelationID(ctx, testID)
	id, ok = log.CorrelationID(ctx)
	assert.True(t, ok)
	assert.Equal(t, testID, id)
}

func TestContextLogger_TextFormatting(t *testing.T) {
	t.Parallel()

	buf := new(bytes.Buffer)
	cfg := log.DefaultConfig(log.LevelDebug)
	cfg.Output = buf
	cfg.Colors = false
	cfg.JSON = false

	logger := log.New(cfg)
	defer func() { _ = logger.Close() }()

	testID := "test-text-correlation-id"
	ctx := log.WithCorrelationID(t.Context(), testID)

	logger.InfoContext(ctx, "hello from context message", log.String("custom_field", "value"))

	_ = logger.Close()

	logOutput := buf.String()
	assert.Contains(t, logOutput, "hello from context message")
	assert.Contains(t, logOutput, "correlation_id=test-text-correlation-id")
	assert.Contains(t, logOutput, "custom_field=value")
}

func TestContextLogger_JSONFormatting(t *testing.T) {
	t.Parallel()

	buf := new(bytes.Buffer)
	cfg := log.DefaultConfig(log.LevelDebug)
	cfg.Output = buf
	cfg.JSON = true

	logger := log.New(cfg)
	defer func() { _ = logger.Close() }()

	testID := "test-json-correlation-id"
	ctx := log.WithCorrelationID(t.Context(), testID)

	logger.ErrorContext(ctx, "an error has occurred", log.String("custom_field", "value"))

	_ = logger.Close()

	logOutput := buf.String()
	assert.NotEmpty(t, logOutput)

	var logMap map[string]any

	err := json.Unmarshal(buf.Bytes(), &logMap)
	assert.NoError(t, err)

	assert.Equal(t, "ERROR", logMap["level"])
	assert.Equal(t, "an error has occurred", logMap["message"])
	assert.Equal(t, "test-json-correlation-id", logMap["correlation_id"])
	assert.Equal(t, "value", logMap["custom_field"])
}

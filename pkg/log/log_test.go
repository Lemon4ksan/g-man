// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package log

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lemon4ksan/g-man/pkg/steam/protocol/enums"
)

type logEntry struct {
	Level   string `json:"level"`
	Msg     string `json:"msg"`
	Request string `json:"request_id"`
	Source  string `json:"source"`
	Value   int    `json:"val"`
}

type blockingWriter struct {
	mu sync.Mutex
}

func (w *blockingWriter) Write(p []byte) (n int, err error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return len(p), nil
}

func TestLevel_Short(t *testing.T) {
	assert.Equal(t, "D", LevelDebug.Short())
	assert.Equal(t, "I", LevelInfo.Short())
	assert.Equal(t, "W", LevelWarn.Short())
	assert.Equal(t, "E", LevelError.Short())
	assert.Equal(t, "?", Level(100).Short())
}

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig(LevelInfo)
	assert.Equal(t, LevelInfo, cfg.Level)
	assert.True(t, cfg.Colors)
	assert.False(t, cfg.FullPath)
}

func TestLogger_Lifecycle(t *testing.T) {
	var buf bytes.Buffer

	cfg := DefaultConfig(LevelDebug)
	cfg.Output = &buf
	cfg.Colors = false
	cfg.AsyncSize = 10

	l := New(cfg)
	l.Debug("debug msg", String("k", "v"))
	l.Info("info msg")
	l.Warn("warn msg")
	l.Error("error msg")

	err := l.Close()
	require.NoError(t, err)

	// Test double close safety
	assert.NoError(t, l.Close())

	output := buf.String()
	assert.Contains(t, output, "[D] debug msg")
	assert.Contains(t, output, "k=v")
	assert.Contains(t, output, "[I] info msg")
	assert.Contains(t, output, "[W] warn msg")
	assert.Contains(t, output, "[E] error msg")

	// Ensure no logs after close
	buf.Reset()
	l.Info("after close")
	assert.Empty(t, buf.String())
}

func TestLogger_LevelFiltering(t *testing.T) {
	var buf bytes.Buffer

	cfg := DefaultConfig(LevelWarn)
	cfg.Output = &buf
	l := New(cfg)

	l.Info("ignore me")
	l.Warn("keep me")
	_ = l.Close()

	assert.NotContains(t, buf.String(), "ignore me")
	assert.Contains(t, buf.String(), "keep me")
}

func TestLogger_With(t *testing.T) {
	var buf bytes.Buffer

	cfg := DefaultConfig(LevelInfo)
	cfg.Output = &buf
	cfg.Colors = false
	cfg.FullPath = true // Crucial: so we can see the whole path in the output string
	l := New(cfg)

	// Branch coverage: Field is not module/component
	l2 := l.With(String("ctx", "val"))

	// Branch coverage: Field is module/component
	l3 := l2.With(Module("Auth"), Component("Database"))

	// Branch coverage: Module name is not a string
	l4 := l3.With(Field{Key: "module", Value: 999})

	l4.Info("query")

	_ = l.Close()

	output := buf.String()
	assert.Contains(t, output, "ctx=val")
	assert.Contains(t, output, "Auth › Database › 999")
}

func TestLogger_Formatting(t *testing.T) {
	tests := []struct {
		name     string
		fullPath bool
		setup    func(Logger) Logger
		msg      string
		fields   []Field
		expected []string
	}{
		{
			name:     "Tree Structure Padding",
			fullPath: false,
			setup:    func(l Logger) Logger { return l.With(Module("M1"), Module("M2")) },
			msg:      "hello",
			fields:   []Field{String("long", strings.Repeat("a", 50))},
			expected: []string{"   └─ M2", "long:"},
		},
		{
			name:     "Full Path Padding",
			fullPath: true,
			setup:    func(l Logger) Logger { return l.With(Module("M1"), Module("M2")) },
			msg:      "hello",
			fields:   []Field{String("long", strings.Repeat("a", 50))},
			expected: []string{"M1 › M2", "long:"},
		},
		{
			name:     "Alignment Logic",
			fullPath: false,
			setup:    func(l Logger) Logger { return l },
			msg:      "short",
			fields:   []Field{String("k", "v")},
			expected: []string{"short" + strings.Repeat(" ", 5)}, // Based on Default AlignWidth
		},
		{
			name:     "Message Exceeds Alignment",
			fullPath: false,
			setup:    func(l Logger) Logger { return l },
			msg:      strings.Repeat("m", 100),
			fields:   []Field{String("k", "v")},
			expected: []string{"  k=v"}, // Should fallback to 2 spaces
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer

			cfg := DefaultConfig(LevelInfo)
			cfg.Output = &buf
			cfg.Colors = false
			cfg.FullPath = tt.fullPath
			cfg.AlignWidth = 50

			l := New(cfg)
			child := tt.setup(l)
			child.Info(tt.msg, tt.fields...)

			_ = l.Close()

			for _, exp := range tt.expected {
				assert.Contains(t, buf.String(), exp)
			}
		})
	}
}

func TestFormatValue(t *testing.T) {
	now := time.Now()

	tests := []struct {
		input    any
		expected string
	}{
		{"plain", "plain"},
		{"with space", `"with space"`},
		{int(1), "1"},
		{int8(1), "1"},
		{int16(1), "1"},
		{int32(1), "1"},
		{int64(1), "1"},
		{uint(1), "1"},
		{uint8(1), "1"},
		{uint16(1), "1"},
		{uint32(1), "1"},
		{uint64(1), "1"},
		{float32(1.5), "1.5"},
		{float64(1.5), "1.5"},
		{true, "true"},
		{false, "false"},
		{[]byte("hello"), "68656c6c6f"},
		{make([]byte, 30), "[ 30 bytes"},
		{errors.New("fail"), "fail"},
		{time.Second, "1s"},
		{now, now.Format("15:04:05.000")},
		{net.ParseIP("1.1.1.1"), "1.1.1.1"},
		{struct{ Name string }{"Test"}, "{Name:Test}"},
	}

	for _, tt := range tests {
		t.Run(fmt.Sprintf("%T", tt.input), func(t *testing.T) {
			assert.Contains(t, formatValue(tt.input), tt.expected)
		})
	}
}

func TestLogger_QueueOverflow(t *testing.T) {
	bw := &blockingWriter{}
	bw.mu.Lock() // Lock the writer to force queue to fill

	cfg := DefaultConfig(LevelInfo)
	cfg.Output = bw
	cfg.AsyncSize = 1
	l := New(cfg)
	al := l.(*AsyncLogger)

	// 1. First msg enters the loop and blocks on the writer
	l.Info("msg 1")
	// 2. Second msg fills the channel (size 1)
	al.queue <- new(bytes.Buffer)
	// 3. Third msg must drop
	l.Info("msg 3")

	assert.Greater(t, al.dropped.Load(), uint64(0))

	bw.mu.Unlock() // Allow drain

	_ = l.Close()
}

func TestLogger_RaceAndMidClose(t *testing.T) {
	// Tests the safety check: if l.closed.Load() { ... } inside log()
	for range 50 {
		l := New(DefaultConfig(LevelInfo))
		l.(*AsyncLogger).cfg.Output = io.Discard

		var wg sync.WaitGroup
		wg.Add(2)

		go func() {
			defer wg.Done()

			for range 100 {
				l.Info("log")
			}
		}()
		go func() {
			defer wg.Done()

			_ = l.Close()
		}()

		wg.Wait()
	}
}

func TestHelpers(t *testing.T) {
	assert.Equal(t, Field{Key: "k", Value: "v"}, Any("k", "v"))
	assert.Equal(t, "v", String("k", "v").Value)
	assert.Equal(t, 1, Int("k", 1).Value)
	assert.Equal(t, int32(1), Int32("k", 1).Value)
	assert.Equal(t, int64(1), Int64("k", 1).Value)
	assert.Equal(t, uint(1), Uint("k", 1).Value)
	assert.Equal(t, uint32(1), Uint32("k", 1).Value)
	assert.Equal(t, uint64(1), Uint64("k", 1).Value)
	assert.Equal(t, 1.5, Float64("k", 1.5).Value)
	assert.Equal(t, true, Bool("k", true).Value)
	assert.Equal(t, time.Second, Duration("k", time.Second).Value)
	assert.IsType(t, time.Time{}, Time("k", time.Now()).Value)
	assert.Equal(t, "error", Err(errors.New("err")).Key)
	assert.Equal(t, "module", Module("m").Key)
	assert.Equal(t, "component", Component("c").Key)
	assert.Equal(t, "k", Strings("k", []string{"a"}).Key)
	assert.Equal(t, "k", Ints("k", []int{1}).Key)
	assert.Equal(t, "k", Uints("k", []uint{1}).Key)
	assert.Equal(t, "k", Bools("k", []bool{true}).Key)
	assert.Equal(t, "k", Bytes("k", []byte{1}).Key)
	assert.Equal(t, "val", ByteString("k", []byte("val")).Value)
	assert.Equal(t, "0102", HexF("k", []byte{1, 2}).Value)
	assert.Equal(t, "127.0.0.1", IP("k", net.ParseIP("127.0.0.1")).Value)
	assert.Equal(t, 80, Port("k", 80).Value)
	assert.Equal(t, "h:80", HostPort("k", "h", 80).Value)
	assert.Equal(t, "k", StringOpt("k", "v").Key)
	assert.Empty(t, StringOpt("k", "").Key)
	assert.Equal(t, "k", IntOpt("k", 1).Key)
	assert.Empty(t, IntOpt("k", 0).Key)
	assert.Equal(t, "steam_id", SteamID(1).Key)
	assert.Equal(t, "job_id", JobID(1).Key)
	assert.Equal(t, "emsg", EMsg(enums.EMsg(1)).Key)
	assert.Equal(t, "eresult", EResult(enums.EResult(1)).Key)
}

func TestMasking(t *testing.T) {
	assert.Equal(t, "****", Mask("123"))
	assert.Equal(t, "****", Mask("1234"))
	assert.Equal(t, "se...et", Mask("secret"))

	assert.Equal(t, "path/****/file", MaskPath("path/user/file", "user"))
	assert.Equal(t, "path", MaskPath("path", ""))
}

func TestLevelColorDefault(t *testing.T) {
	// Coverage for the default case in levelColor switch
	assert.Equal(t, ansiReset, levelColor(Level(127)))
}

func TestDiscardLogger(t *testing.T) {
	d := Discard
	assert.NoError(t, d.Close())
	assert.Equal(t, d, d.With(String("k", "v")))

	// Coverage for non-interface specific methods on the struct
	_ = d.(*discard)

	// No-op calls
	d.Debug("test")
	d.Info("test")
	d.Warn("test")
	d.Error("test")

	ctx := context.Background()
	d.DebugContext(ctx, "test")
	d.InfoContext(ctx, "test")
	d.WarnContext(ctx, "test")
	d.ErrorContext(ctx, "test")
}

func TestEmptyKeyField(t *testing.T) {
	var buf bytes.Buffer

	cfg := DefaultConfig(LevelInfo)
	cfg.Output = &buf
	cfg.Colors = false
	l := New(cfg)

	// Hit branch: if f.Key == "" { return }
	l.Info("msg", Field{Key: "", Value: "ignore"})
	_ = l.Close()

	assert.NotContains(t, buf.String(), "ignore")
}

func TestSlogAdapter(t *testing.T) {
	var buf bytes.Buffer

	h := slog.NewJSONHandler(&buf, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	})
	baseSlog := slog.New(h)

	l := FromSlog(baseSlog)

	t.Run("Levels and Basic Logging", func(t *testing.T) {
		buf.Reset()

		l.Debug("debug msg")
		l.Info("info msg")
		l.Warn("warn msg")
		l.Error("error msg")

		ctx := context.Background()
		l.DebugContext(ctx, "debug msg ctx")
		l.InfoContext(ctx, "info msg ctx")
		l.WarnContext(ctx, "warn msg ctx")
		l.ErrorContext(ctx, "error msg ctx")

		output := buf.String()
		assert.Contains(t, output, `"level":"DEBUG","msg":"debug msg"`)
		assert.Contains(t, output, `"level":"INFO","msg":"info msg"`)
		assert.Contains(t, output, `"level":"WARN","msg":"warn msg"`)
		assert.Contains(t, output, `"level":"ERROR","msg":"error msg"`)
		assert.Contains(t, output, `"level":"DEBUG","msg":"debug msg ctx"`)
		assert.Contains(t, output, `"level":"INFO","msg":"info msg ctx"`)
		assert.Contains(t, output, `"level":"WARN","msg":"warn msg ctx"`)
		assert.Contains(t, output, `"level":"ERROR","msg":"error msg ctx"`)
	})

	t.Run("Fields mapping", func(t *testing.T) {
		buf.Reset()

		l.Info("message with fields",
			String("source", "steam"),
			Int("val", 42),
		)

		var entry logEntry

		err := json.Unmarshal(buf.Bytes(), &entry)
		require.NoError(t, err)

		assert.Equal(t, "INFO", entry.Level)
		assert.Equal(t, "message with fields", entry.Msg)
		assert.Equal(t, "steam", entry.Source)
		assert.Equal(t, 42, entry.Value)
	})

	t.Run("With context fields", func(t *testing.T) {
		buf.Reset()

		child := l.With(String("request_id", "abc-123"))
		child.Info("starting process")

		var entry logEntry

		err := json.Unmarshal(buf.Bytes(), &entry)
		require.NoError(t, err)

		assert.Equal(t, "abc-123", entry.Request)
		assert.Equal(t, "starting process", entry.Msg)
	})

	t.Run("Nesting With", func(t *testing.T) {
		buf.Reset()

		l1 := l.With(String("source", "main"))
		l2 := l1.With(Int("val", 100))

		l2.Info("final")

		var entry logEntry

		err := json.Unmarshal(buf.Bytes(), &entry)
		require.NoError(t, err)

		assert.Equal(t, "main", entry.Source)
		assert.Equal(t, 100, entry.Value)
		assert.Equal(t, "final", entry.Msg)
	})

	t.Run("Lifecycle methods", func(t *testing.T) {
		assert.NoError(t, l.Close())
	})
}

func TestLogger_JSONFormat(t *testing.T) {
	var buf bytes.Buffer

	cfg := DefaultConfig(LevelDebug)
	cfg.Output = &buf
	cfg.JSON = true
	cfg.Colors = false

	l := New(cfg)
	l = l.With(Module("Core"), Component("Database"), String("ctx_field", "ctx_val"))

	now := time.Now()
	ip := net.ParseIP("192.168.1.1")
	errTest := errors.New("database connection reset")

	l.Info("user logged in",
		String("username", "alice"),
		Int("age", 30),
		Int64("count", 9999999999),
		Bool("active", true),
		Err(errTest),
		Bytes("raw_data", []byte("hello world")),
		Time("login_time", now),
		IP("client_ip", ip),
		Any("fallback", struct{ X int }{X: 10}),
	)

	err := l.Close()
	require.NoError(t, err)

	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	require.Len(t, lines, 1)

	var entry map[string]any

	err = json.Unmarshal([]byte(lines[0]), &entry)
	require.NoError(t, err)

	assert.Equal(t, "INFO", entry["level"])
	assert.Equal(t, "user logged in", entry["message"])
	assert.NotEmpty(t, entry["time"])

	// Verify modules array
	mods, ok := entry["module"].([]any)
	require.True(t, ok)
	require.Len(t, mods, 2)
	assert.Equal(t, "Core", mods[0])
	assert.Equal(t, "Database", mods[1])

	// Verify fields
	assert.Equal(t, "ctx_val", entry["ctx_field"])
	assert.Equal(t, "alice", entry["username"])
	assert.Equal(t, float64(30), entry["age"])
	assert.Equal(t, float64(9999999999), entry["count"])
	assert.Equal(t, true, entry["active"])
	assert.Equal(t, "database connection reset", entry["error"])
	assert.Equal(t, "68656c6c6f20776f726c64", entry["raw_data"])
	assert.Equal(t, "192.168.1.1", entry["client_ip"])
	assert.Contains(t, entry["fallback"], "{X:10}")
}

func TestLogger_ContextAware(t *testing.T) {
	var buf bytes.Buffer

	cfg := DefaultConfig(LevelDebug)
	cfg.Output = &buf
	cfg.Colors = false

	l := New(cfg)

	type ctxKey string

	ctx := context.WithValue(context.Background(), ctxKey("trace_id"), "12345")

	l.DebugContext(ctx, "debug log in ctx", String("k", "v"))
	l.InfoContext(ctx, "info log in ctx")
	l.WarnContext(ctx, "warn log in ctx")
	l.ErrorContext(ctx, "error log in ctx")

	err := l.Close()
	require.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "[D] debug log in ctx")
	assert.Contains(t, output, "k=v")
	assert.Contains(t, output, "[I] info log in ctx")
	assert.Contains(t, output, "[W] warn log in ctx")
	assert.Contains(t, output, "[E] error log in ctx")
}

func TestLogger_JSONFormat_ExtraTypes(t *testing.T) {
	var buf bytes.Buffer

	cfg := DefaultConfig(LevelDebug)
	cfg.Output = &buf
	cfg.JSON = true
	cfg.Colors = false

	l := New(cfg)

	// Log with different levels to cover level formatting branch in formatJSON
	l.Debug("debug msg")
	l.Warn("warn msg")
	l.Error("error msg")
	l.(*AsyncLogger).log(Level(100), "unknown level msg", nil)

	// Log all coverage-specific types
	l.Info("extra types log",
		Field{Key: "i8", Value: int8(-8)},
		Field{Key: "i16", Value: int16(-16)},
		Field{Key: "i32", Value: int32(-32)},
		Field{Key: "u", Value: uint(10)},
		Field{Key: "u8", Value: uint8(8)},
		Field{Key: "u16", Value: uint16(16)},
		Field{Key: "u32", Value: uint32(32)},
		Field{Key: "u64", Value: uint64(64)},
		Field{Key: "f32", Value: float32(1.23)},
		Field{Key: "f64", Value: float64(4.56)},
		Field{Key: "bool_false", Value: false},
		Field{Key: "dur", Value: 5 * time.Second},
		Field{Key: "ip_raw", Value: net.ParseIP("10.0.0.1")},
		Field{Key: "", Value: "ignore_json"},
	)

	err := l.Close()
	require.NoError(t, err)

	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	require.Len(t, lines, 5)

	// Check Debug line
	var entryDebug map[string]any

	err = json.Unmarshal([]byte(lines[0]), &entryDebug)
	require.NoError(t, err)
	assert.Equal(t, "DEBUG", entryDebug["level"])

	// Check Warn line
	var entryWarn map[string]any

	err = json.Unmarshal([]byte(lines[1]), &entryWarn)
	require.NoError(t, err)
	assert.Equal(t, "WARN", entryWarn["level"])

	// Check Error line
	var entryError map[string]any

	err = json.Unmarshal([]byte(lines[2]), &entryError)
	require.NoError(t, err)
	assert.Equal(t, "ERROR", entryError["level"])

	// Check Unknown Level line
	var entryUnknown map[string]any

	err = json.Unmarshal([]byte(lines[3]), &entryUnknown)
	require.NoError(t, err)
	assert.Equal(t, "UNKNOWN", entryUnknown["level"])

	// Check extra types fields line
	var entry map[string]any

	err = json.Unmarshal([]byte(lines[4]), &entry)
	require.NoError(t, err)

	assert.Equal(t, float64(-8), entry["i8"])
	assert.Equal(t, float64(-16), entry["i16"])
	assert.Equal(t, float64(-32), entry["i32"])
	assert.Equal(t, float64(10), entry["u"])
	assert.Equal(t, float64(8), entry["u8"])
	assert.Equal(t, float64(16), entry["u16"])
	assert.Equal(t, float64(32), entry["u32"])
	assert.Equal(t, float64(64), entry["u64"])
	assert.InDelta(t, 1.23, entry["f32"], 0.0001)
	assert.InDelta(t, 4.56, entry["f64"], 0.0001)
	assert.Equal(t, false, entry["bool_false"])
	assert.Equal(t, "5s", entry["dur"])
	assert.Equal(t, "10.0.0.1", entry["ip_raw"])
}

func TestLogger_OmitFields(t *testing.T) {
	t.Run("Standard format omitting fields", func(t *testing.T) {
		var buf bytes.Buffer

		cfg := DefaultConfig(LevelDebug)
		cfg.Output = &buf
		cfg.Colors = false
		cfg.OmitFields = []string{"account", "steam_id"}

		l := New(cfg)
		l.Info("testing", String("account", "arseny_sn"), SteamID(12345), String("keep", "me"))
		err := l.Close()
		require.NoError(t, err)

		output := buf.String()
		assert.Contains(t, output, "keep=me")
		assert.NotContains(t, output, "account=")
		assert.NotContains(t, output, "steam_id=")
	})

	t.Run("JSON format omitting fields", func(t *testing.T) {
		var buf bytes.Buffer

		cfg := DefaultConfig(LevelDebug)
		cfg.Output = &buf
		cfg.Colors = false
		cfg.JSON = true
		cfg.OmitFields = []string{"account", "steam_id"}

		l := New(cfg)
		l.Info("testing", String("account", "arseny_sn"), SteamID(12345), String("keep", "me"))
		err := l.Close()
		require.NoError(t, err)

		var entry map[string]any

		err = json.Unmarshal(buf.Bytes(), &entry)
		require.NoError(t, err)

		assert.Equal(t, "me", entry["keep"])
		_, ok := entry["account"]
		assert.False(t, ok)
		_, ok = entry["steam_id"]
		assert.False(t, ok)
	})
}

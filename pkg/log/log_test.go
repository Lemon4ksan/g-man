// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package log

import (
	"bytes"
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
	assert.Equal(t, "D", DebugLevel.Short())
	assert.Equal(t, "I", InfoLevel.Short())
	assert.Equal(t, "W", WarnLevel.Short())
	assert.Equal(t, "E", ErrorLevel.Short())
	assert.Equal(t, "?", Level(100).Short())
}

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig(InfoLevel)
	assert.Equal(t, InfoLevel, cfg.Level)
	assert.True(t, cfg.Colors)
	assert.False(t, cfg.FullPath)
}

func TestLogger_Lifecycle(t *testing.T) {
	var buf bytes.Buffer

	cfg := DefaultConfig(DebugLevel)
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

	cfg := DefaultConfig(WarnLevel)
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

	cfg := DefaultConfig(InfoLevel)
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

			cfg := DefaultConfig(InfoLevel)
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

	cfg := DefaultConfig(InfoLevel)
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
		l := New(DefaultConfig(InfoLevel))
		l.(*AsyncLogger).cfg.Output = io.Discard

		var wg sync.WaitGroup
		wg.Add(2)

		go func() {
			defer wg.Done()

			for j := 0; j < 100; j++ {
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
	impl := d.(*discard)
	assert.Equal(t, d, impl.WithModule("m"))
	assert.False(t, impl.IsDebugEnabled())

	// No-op calls
	d.Debug("test")
	d.Info("test")
	d.Warn("test")
	d.Error("test")
}

func TestEmptyKeyField(t *testing.T) {
	var buf bytes.Buffer

	cfg := DefaultConfig(InfoLevel)
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

		output := buf.String()
		assert.Contains(t, output, `"level":"DEBUG","msg":"debug msg"`)
		assert.Contains(t, output, `"level":"INFO","msg":"info msg"`)
		assert.Contains(t, output, `"level":"WARN","msg":"warn msg"`)
		assert.Contains(t, output, `"level":"ERROR","msg":"error msg"`)
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

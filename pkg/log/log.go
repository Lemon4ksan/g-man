// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package log provides a high-performance, asynchronous, structured logger
// designed for both human readability and machine efficiency.
package log

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"io"
	"net"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/lemon4ksan/g-man/pkg/steam/socket/protocol"
)

// Level represents the severity of the log message.
type Level int8

const (
	// DebugLevel is typically used in development to trace logic.
	DebugLevel Level = iota - 1
	// InfoLevel is the default logging level for general operational events.
	InfoLevel
	// WarnLevel represents non-critical issues that might require attention.
	WarnLevel
	// ErrorLevel represents high-priority failures.
	ErrorLevel
)

// Short returns a single-character representation of the log level.
func (l Level) Short() string {
	switch l {
	case DebugLevel:
		return "D"
	case InfoLevel:
		return "I"
	case WarnLevel:
		return "W"
	case ErrorLevel:
		return "E"
	default:
		return "?"
	}
}

// Field represents a single key-value pair used in structured logging.
// It is recommended to use the provided helper functions (e.g., log.String(), log.Int())
// to create Fields.
type Field struct {
	Key   string
	Value any
}

// Logger defines the primary interface for logging operations.
type Logger interface {
	// Debug logs a message at the Debug level.
	Debug(msg string, fields ...Field)
	// Info logs a message at the Info level.
	Info(msg string, fields ...Field)
	// Warn logs a message at the Warn level.
	Warn(msg string, fields ...Field)
	// Error logs a message at the Error level.
	Error(msg string, fields ...Field)

	// With returns a new Logger instance carrying the provided fields as context.
	// If a field key is "module" or "component", it is expected to update the module path instead.
	// Example:
	//   requestLogger := logger.With(log.String("request_id", "abc-123"))
	//   requestLogger.Info("Processing") // Includes request_id automatically
	With(fields ...Field) Logger

	// Close flushes the asynchronous queue and stops the writer loop.
	Close() error
}

// Config controls the behavior and visual style of the logger.
type Config struct {
	// Level is the minimum severity to log.
	Level Level
	// Output is the destination for logs (e.g., os.Stdout or a file).
	Output io.Writer
	// TimeFormat defines the timestamp layout (Go standard time formatting).
	TimeFormat string
	// AsyncSize is the capacity of the non-blocking log queue.
	AsyncSize int
	// Colors enables ANSI terminal color codes.
	Colors bool
	// FullPath, if true, prints "Mod > Sub > Service" instead of a tree structure.
	FullPath bool
	// PathSep is the separator used when FullPath is true.
	PathSep string
	// AlignWidth is the horizontal offset where fields start, ensuring messages are aligned.
	AlignWidth int
}

// DefaultConfig returns a configuration balanced for local development.
// It enables colors, tree-style modules, and a reasonable async buffer.
func DefaultConfig(level Level) Config {
	return Config{
		Level:      level,
		Output:     os.Stdout,
		TimeFormat: "15:04:05.000",
		AsyncSize:  2048,
		Colors:     true,
		FullPath:   false,
		PathSep:    " › ",
		AlignWidth: 75,
	}
}

// AsyncLogger implements the Logger interface with a non-blocking background writer.
// Logs are formatted in the calling goroutine, then sent to a channel for writing.
type AsyncLogger struct {
	cfg     Config
	path    []string
	context []Field

	queue chan *bytes.Buffer
	wg    *sync.WaitGroup

	// Synchronization for safe shutdown
	mu     sync.RWMutex
	closed atomic.Bool

	// dropped tracks the number of messages discarded due to a full queue.
	dropped atomic.Uint64
}

// New creates and starts a background goroutine to process logs based on the provided Config.
//
// Example:
//
//	l := log.New(log.DefaultConfig(log.InfoLevel))
//	defer l.Close()
//	l.Info("Application started")
func New(cfg Config) Logger {
	l := &AsyncLogger{
		cfg:   cfg,
		queue: make(chan *bytes.Buffer, cfg.AsyncSize),
		wg:    &sync.WaitGroup{},
	}
	l.wg.Add(1)
	go l.writerLoop()
	return l
}

// Close gracefully shuts down the logger. It stops accepting new logs,
// closes the internal queue, and waits for pending logs to be written.
func (l *AsyncLogger) Close() error {
	// Prevent multiple closures
	if l.closed.Swap(true) {
		return nil
	}

	l.mu.Lock()
	close(l.queue)
	l.mu.Unlock()

	l.wg.Wait()
	return nil
}

// With appends fields to the current logger context. If a field key is "module"
// or "component", it updates the module path instead.
func (l *AsyncLogger) With(fields ...Field) Logger {
	child := &AsyncLogger{
		cfg:     l.cfg,
		queue:   l.queue,
		wg:      l.wg,
		path:    make([]string, len(l.path)),
		context: make([]Field, len(l.context), len(l.context)+len(fields)),
	}

	copy(child.path, l.path)
	copy(child.context, l.context)

	for _, f := range fields {
		if f.Key == "module" || f.Key == "component" {
			var val string
			if s, ok := f.Value.(string); ok {
				val = s
			} else {
				val = fmt.Sprint(f.Value)
			}

			if len(child.path) == 0 || child.path[len(child.path)-1] != val {
				child.path = append(child.path, val)
			}
		} else {
			child.context = append(child.context, f)
		}
	}

	return child
}

func (l *AsyncLogger) Debug(msg string, f ...Field) { l.log(DebugLevel, msg, f) }
func (l *AsyncLogger) Info(msg string, f ...Field)  { l.log(InfoLevel, msg, f) }
func (l *AsyncLogger) Warn(msg string, f ...Field)  { l.log(WarnLevel, msg, f) }
func (l *AsyncLogger) Error(msg string, f ...Field) { l.log(ErrorLevel, msg, f) }

func (l *AsyncLogger) log(lvl Level, msg string, fields []Field) {
	// Fast check for level and closed state
	if lvl < l.cfg.Level || l.closed.Load() {
		return
	}

	buf := bufPool.Get().(*bytes.Buffer)
	buf.Reset()

	l.format(buf, lvl, msg, fields)

	// Safe enqueue using RLock to prevent race with Close()
	l.mu.RLock()
	if l.closed.Load() {
		l.mu.RUnlock()
		bufPool.Put(buf)
		return
	}

	select {
	case l.queue <- buf:
	default:
		// Queue is full. Record the drop and discard the buffer.
		l.dropped.Add(1)
		bufPool.Put(buf)
	}
	l.mu.RUnlock()
}

func (l *AsyncLogger) writerLoop() {
	defer l.wg.Done()
	for buf := range l.queue {
		// Report dropped messages periodically or on the next successful write
		if drops := l.dropped.Swap(0); drops > 0 {
			warnMsg := fmt.Sprintf("%s[LOGGER WARNING] Dropped %d messages due to full queue%s\n", ansiRedBold, drops, ansiReset)
			_, _ = l.cfg.Output.Write([]byte(warnMsg))
		}

		_, _ = l.cfg.Output.Write(buf.Bytes())
		bufPool.Put(buf)
	}
}

// ANSI Escape Codes for terminal coloring
const (
	ansiReset   = "\033[0m"
	ansiDim     = "\033[2m"
	ansiBold    = "\033[1m"
	ansiRedBold = "\033[1;31m"
	ansiGreen   = "\033[32m"
	ansiYellow  = "\033[33m"
	ansiBlue    = "\033[34m"
	ansiMagenta = "\033[35m"
	ansiCyan    = "\033[36m"
	ansiWhite   = "\033[37m"
	ansiGray    = "\033[90m"
)

func (l *AsyncLogger) writeColor(b *bytes.Buffer, colorCode string) {
	if l.cfg.Colors {
		b.WriteString(colorCode)
	}
}

// format handles the complex logic of building the log line, including:
// 1. Timestamp and Level
// 2. Module Tree (indentation)
// 3. Main Message
// 4. Inline Fields (short values)
// 5. Block Fields (multi-line or very long values)
func (l *AsyncLogger) format(b *bytes.Buffer, lvl Level, msg string, callFields []Field) {
	visibleLen := 0

	// Time
	ts := time.Now().Format(l.cfg.TimeFormat)
	l.writeColor(b, ansiGray)
	b.WriteString(ts)
	l.writeColor(b, ansiReset)
	b.WriteByte(' ')
	visibleLen += len(ts) + 1

	// Level
	l.writeColor(b, levelColor(lvl))
	b.WriteByte('[')
	b.WriteString(lvl.Short())
	b.WriteByte(']')
	l.writeColor(b, ansiReset)
	b.WriteByte(' ')
	visibleLen += 4 // "[L] "

	// Path / Tree
	depth := len(l.path)
	if depth > 0 {
		if l.cfg.FullPath {
			fullPath := strings.Join(l.path, l.cfg.PathSep)
			l.writeColor(b, ansiBlue)
			b.WriteString(fullPath)
			l.writeColor(b, ansiReset)
			b.WriteByte(' ')
			visibleLen += len(fullPath) + 1
		} else {
			indent := strings.Repeat("   ", depth-1)
			l.writeColor(b, ansiGray)
			b.WriteString(indent)
			b.WriteString("└─ ")
			l.writeColor(b, ansiBlue)
			b.WriteString(l.path[depth-1])
			l.writeColor(b, ansiReset)
			b.WriteByte(' ')
			visibleLen += len(indent) + 3 + len(l.path[depth-1]) + 1
		}
	}

	// Message
	b.WriteString(msg)
	visibleLen += len(msg)

	// Fields
	totalFields := len(l.context) + len(callFields)
	if totalFields == 0 {
		b.WriteByte('\n')
		return
	}

	var inline, blocks []Field
	var inlineStrs, blockStrs []string

	processField := func(f Field) {
		if f.Key == "" {
			return
		}
		valStr := formatValue(f.Value)
		if len(valStr) > 40 || strings.Contains(valStr, "\n") {
			blocks = append(blocks, f)
			blockStrs = append(blockStrs, valStr)
		} else {
			inline = append(inline, f)
			inlineStrs = append(inlineStrs, valStr)
		}
	}

	for _, f := range l.context {
		processField(f)
	}
	for _, f := range callFields {
		processField(f)
	}

	// Write Inline Fields
	if len(inline) > 0 {
		if visibleLen < l.cfg.AlignWidth {
			b.WriteString(strings.Repeat(" ", l.cfg.AlignWidth-visibleLen))
		} else {
			b.WriteString("  ")
		}

		for i, f := range inline {
			l.writeColor(b, ansiCyan)
			b.WriteString(f.Key)
			l.writeColor(b, ansiGray)
			b.WriteByte('=')
			l.writeColor(b, ansiReset)
			b.WriteString(inlineStrs[i])
			b.WriteByte(' ')
		}
	}
	b.WriteByte('\n')

	// Write Block Fields
	if len(blocks) > 0 {
		paddingStr := strings.Repeat(" ", l.blockPadding(depth))
		for i, f := range blocks {
			b.WriteString(paddingStr)
			l.writeColor(b, ansiCyan)
			b.WriteString(f.Key)
			b.WriteByte(':')
			l.writeColor(b, ansiReset)
			b.WriteByte(' ')
			b.WriteString(blockStrs[i])
			b.WriteByte('\n')
		}
	}
}

// blockPadding calculates the indentation for block fields to align them under the message.
func (l *AsyncLogger) blockPadding(depth int) int {
	base := len(l.cfg.TimeFormat) + 5
	if depth > 0 {
		if l.cfg.FullPath {
			pathStr := strings.Join(l.path, l.cfg.PathSep)
			base += len(pathStr) + 1
		} else {
			base += (depth-1)*3 + 3
		}
	}
	return base
}

func levelColor(lvl Level) string {
	switch lvl {
	case DebugLevel:
		return ansiMagenta
	case InfoLevel:
		return ansiGreen
	case WarnLevel:
		return ansiYellow
	case ErrorLevel:
		return ansiRedBold
	default:
		return ansiReset
	}
}

// formatValue stringifies a value with minimal allocations.
// Heavily optimized using strconv instead of fmt.Sprintf.
func formatValue(v any) string {
	switch val := v.(type) {
	case string:
		if strings.Contains(val, " ") || strings.Contains(val, "\n") {
			return strconv.Quote(val)
		}
		return val
	case int:
		return strconv.Itoa(val)
	case int8:
		return strconv.FormatInt(int64(val), 10)
	case int16:
		return strconv.FormatInt(int64(val), 10)
	case int32:
		return strconv.FormatInt(int64(val), 10)
	case int64:
		return strconv.FormatInt(val, 10)
	case uint:
		return strconv.FormatUint(uint64(val), 10)
	case uint8:
		return strconv.FormatUint(uint64(val), 10)
	case uint16:
		return strconv.FormatUint(uint64(val), 10)
	case uint32:
		return strconv.FormatUint(uint64(val), 10)
	case uint64:
		return strconv.FormatUint(val, 10)
	case float32:
		return strconv.FormatFloat(float64(val), 'g', -1, 32)
	case float64:
		return strconv.FormatFloat(val, 'g', -1, 64)
	case bool:
		if val {
			return "true"
		}
		return "false"
	case []byte:
		if len(val) > 24 {
			return fmt.Sprintf("[ %d bytes | preview: %x... ]", len(val), val[:16])
		}
		return hex.EncodeToString(val)
	case error:
		return val.Error()
	case time.Duration:
		return val.String()
	case time.Time:
		return val.Format("15:04:05.000")
	case net.IP:
		return val.String()
	default:
		return fmt.Sprintf("%+v", v) // Fallback for complex structs
	}
}

// Global buffer pool to reduce GC pressure for high-frequency logging.
var bufPool = sync.Pool{
	New: func() any { return new(bytes.Buffer) },
}

// --- Field Helpers (Zero-allocation wrappers) ---
// These helpers are used to provide structured context to log messages.

// Basic Types
func String(k, v string) Field                 { return Field{Key: k, Value: v} }
func Int(k string, v int) Field                { return Field{Key: k, Value: v} }
func Int32(k string, v int32) Field            { return Field{Key: k, Value: v} }
func Int64(k string, v int64) Field            { return Field{Key: k, Value: v} }
func Uint(k string, v uint) Field              { return Field{Key: k, Value: v} }
func Uint32(k string, v uint32) Field          { return Field{Key: k, Value: v} }
func Uint64(k string, v uint64) Field          { return Field{Key: k, Value: v} }
func Float64(k string, v float64) Field        { return Field{Key: k, Value: v} }
func Bool(k string, v bool) Field              { return Field{Key: k, Value: v} }
func Duration(k string, v time.Duration) Field { return Field{Key: k, Value: v} }
func Time(k string, v time.Time) Field         { return Field{Key: k, Value: v} }
func Err(err error) Field                      { return Field{Key: "error", Value: err} }
func Any(k string, v any) Field                { return Field{Key: k, Value: v} }
func Module(name string) Field                 { return Field{Key: "module", Value: name} }
func Component(name string) Field              { return Field{Key: "component", Value: name} }

// Collections
func Strings(k string, v []string) Field  { return Field{Key: k, Value: v} }
func Ints(k string, v []int) Field        { return Field{Key: k, Value: v} }
func Uints(k string, v []uint) Field      { return Field{Key: k, Value: v} }
func Bools(k string, v []bool) Field      { return Field{Key: k, Value: v} }
func Bytes(k string, v []byte) Field      { return Field{Key: k, Value: v} }
func ByteString(k string, v []byte) Field { return Field{Key: k, Value: string(v)} }
func HexF(k string, v []byte) Field       { return Field{Key: k, Value: hex.EncodeToString(v)} }

// Network Helpers
func IP(k string, v net.IP) Field { return Field{Key: k, Value: v.String()} }
func Port(k string, v int) Field  { return Field{Key: k, Value: v} }
func HostPort(k string, h string, p int) Field {
	return Field{Key: k, Value: fmt.Sprintf("%s:%d", h, p)}
}

// Optionals: These return an empty Field (ignored) if the value is zero-equivalent.
func StringOpt(k, v string) Field {
	if v == "" {
		return Field{}
	}
	return Field{Key: k, Value: v}
}
func IntOpt(k string, v int) Field {
	if v == 0 {
		return Field{}
	}
	return Field{Key: k, Value: v}
}

// --- Steam Specific Helpers ---
// Designed for integration with Steam protocol implementations.

// SteamID logs a 64-bit Steam identifier.
func SteamID(v uint64) Field { return Field{Key: "steam_id", Value: v} }

// JobID logs an asynchronous correlation ID.
func JobID(v uint64) Field { return Field{Key: "job_id", Value: v} }

// EMsg logs a Steam protocol message type as a readable string.
func EMsg(v protocol.EMsg) Field {
	return Field{Key: "emsg", Value: v.String()}
}

// EResult logs a Steam result code as a readable string.
func EResult(v protocol.EResult) Field {
	return Field{Key: "eresult", Value: v.String()}
}

// Discard Logger Pattern: Useful for tests or disabling logs.
var Discard Logger = &discard{}

type discard struct{}

func (d *discard) Close() error                 { return nil }
func (d *discard) With(fields ...Field) Logger  { return d }
func (d *discard) WithModule(mod string) Logger { return d }
func (d *discard) Debug(msg string, f ...Field) {}
func (d *discard) Info(msg string, f ...Field)  {}
func (d *discard) Warn(msg string, f ...Field)  {}
func (d *discard) Error(msg string, f ...Field) {}
func (d *discard) IsDebugEnabled() bool         { return false }

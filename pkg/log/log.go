// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package log

import (
	"bytes"
	"context"
	"encoding/hex"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/lemon4ksan/g-man/pkg/steam/protocol/enums"
)

// Level represents the severity of a log message.
// The logger will ignore messages with a level lower than the configured threshold.
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
// to create Fields rather than constructing this struct manually.
type Field struct {
	Key   string
	Value any
}

// Logger defines the primary interface for logging operations.
//
// All logging methods (Debug, Info, Warn, Error) are safe for concurrent use.
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
	//
	// If a field key is "module" or "component", it updates the module path
	// instead of adding a standard field.
	//
	// Example:
	//   requestLogger := logger.With(log.String("request_id", "abc-123"))
	//   requestLogger.Info("Processing") // Includes request_id automatically
	With(fields ...Field) Logger

	// Close flushes the asynchronous queue and stops the writer loop.
	// It should be called exactly once during application shutdown.
	Close() error
}

// Config controls the behavior and visual style of the logger.
type Config struct {
	// Level is the minimum severity to log.
	Level Level
	// Output is the destination for logs (e.g., os.Stdout or a file).
	Output io.Writer
	// TimeFormat defines the timestamp layout using Go standard time formatting.
	TimeFormat string
	// AsyncSize is the capacity of the non-blocking log queue.
	// If the queue fills up, new messages are discarded to prevent blocking the caller.
	AsyncSize int
	// Colors enables ANSI terminal color codes in the output.
	Colors bool
	// FullPath, if true, prints "Mod › Sub › Service" instead of a tree structure.
	FullPath bool
	// PathSep is the separator used when FullPath is true. Defaults to " › ".
	PathSep string
	// AlignWidth is the horizontal offset where fields start, ensuring messages are aligned.
	AlignWidth int
}

// DefaultConfig returns a configuration balanced for local development.
// It enables colors, tree-style modules, and a buffer size of 2048.
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
// The user is responsible for calling Close() on the returned Logger to ensure all
// logs are flushed to the output destination.
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

// Debug logs a message at the Debug level.
func (l *AsyncLogger) Debug(msg string, f ...Field) { l.log(DebugLevel, msg, f) }

// Info logs a message at the Info level.
func (l *AsyncLogger) Info(msg string, f ...Field) { l.log(InfoLevel, msg, f) }

// Warn logs a message at the Warn level.
func (l *AsyncLogger) Warn(msg string, f ...Field) { l.log(WarnLevel, msg, f) }

// Error logs a message at the Error level.
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
			warnMsg := fmt.Sprintf(
				"%s[LOGGER WARNING] Dropped %d messages due to full queue%s\n",
				ansiRedBold,
				drops,
				ansiReset,
			)
			_, _ = l.cfg.Output.Write([]byte(warnMsg))
		}

		_, _ = l.cfg.Output.Write(buf.Bytes())
		bufPool.Put(buf)
	}
}

// ANSI Escape Codes for terminal coloring
const (
	ansiReset   = "\033[0m"
	ansiRedBold = "\033[1;31m"
	ansiGreen   = "\033[32m"
	ansiYellow  = "\033[33m"
	ansiBlue    = "\033[34m"
	ansiMagenta = "\033[35m"
	ansiCyan    = "\033[36m"
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

	var (
		inline, blocks              []Field
		inlineStrings, blockStrings []string
	)

	processField := func(f Field) {
		if f.Key == "" {
			return
		}

		valStr := formatValue(f.Value)
		if len(valStr) > 40 || strings.Contains(valStr, "\n") {
			blocks = append(blocks, f)
			blockStrings = append(blockStrings, valStr)
		} else {
			inline = append(inline, f)
			inlineStrings = append(inlineStrings, valStr)
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
			b.WriteString(inlineStrings[i])
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
			b.WriteString(blockStrings[i])
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

// String creates a Field for a string value.
func String(k, v string) Field { return Field{Key: k, Value: v} }

// Int creates a Field for an integer value.
func Int(k string, v int) Field { return Field{Key: k, Value: v} }

// Int32 creates a Field for an int32 value.
func Int32(k string, v int32) Field { return Field{Key: k, Value: v} }

// Int64 creates a Field for an int64 value.
func Int64(k string, v int64) Field { return Field{Key: k, Value: v} }

// Uint creates a Field for an unsigned integer value.
func Uint(k string, v uint) Field { return Field{Key: k, Value: v} }

// Uint32 creates a Field for a uint32 value.
func Uint32(k string, v uint32) Field { return Field{Key: k, Value: v} }

// Uint64 creates a Field for a uint64 value.
func Uint64(k string, v uint64) Field { return Field{Key: k, Value: v} }

// Float64 creates a Field for a float64 value.
func Float64(k string, v float64) Field { return Field{Key: k, Value: v} }

// Bool creates a Field for a boolean value.
func Bool(k string, v bool) Field { return Field{Key: k, Value: v} }

// Duration creates a Field for a time.Duration value.
func Duration(k string, v time.Duration) Field { return Field{Key: k, Value: v} }

// Time creates a Field for a time.Time value.
func Time(k string, v time.Time) Field { return Field{Key: k, Value: v} }

// Err creates a Field for an error.
func Err(err error) Field { return Field{Key: "error", Value: err} }

// Any creates a Field for an arbitrary value.
func Any(k string, v any) Field { return Field{Key: k, Value: v} }

// Module creates a special Field that defines a new level in the logger's module hierarchy.
func Module(name string) Field { return Field{Key: "module", Value: name} }

// Component is an alias for Module, used to denote a subsection of a module.
func Component(name string) Field { return Field{Key: "component", Value: name} }

// Strings creates a Field for a slice of strings.
func Strings(k string, v []string) Field { return Field{Key: k, Value: v} }

// Ints creates a Field for a slice of integers.
func Ints(k string, v []int) Field { return Field{Key: k, Value: v} }

// Uints creates a Field for a slice of unsigned integers.
func Uints(k string, v []uint) Field { return Field{Key: k, Value: v} }

// Bools creates a Field for a slice of booleans.
func Bools(k string, v []bool) Field { return Field{Key: k, Value: v} }

// Bytes creates a Field for a slice of bytes (logged as hex).
func Bytes(k string, v []byte) Field { return Field{Key: k, Value: v} }

// ByteString creates a Field for a slice of bytes logged as a string.
func ByteString(k string, v []byte) Field { return Field{Key: k, Value: string(v)} }

// HexF creates a Field for a slice of bytes logged as a hex string.
func HexF(k string, v []byte) Field { return Field{Key: k, Value: hex.EncodeToString(v)} }

// IP creates a Field for a net.IP value.
func IP(k string, v net.IP) Field { return Field{Key: k, Value: v.String()} }

// Port creates a Field for a network port.
func Port(k string, v int) Field { return Field{Key: k, Value: v} }

// HostPort creates a Field for a combined host and port.
func HostPort(k, h string, p int) Field {
	return Field{Key: k, Value: fmt.Sprintf("%s:%d", h, p)}
}

// StringOpt returns a Field if the value is not empty.
func StringOpt(k, v string) Field {
	if v == "" {
		return Field{}
	}

	return Field{Key: k, Value: v}
}

// IntOpt returns a Field if the value is not zero.
func IntOpt(k string, v int) Field {
	if v == 0 {
		return Field{}
	}

	return Field{Key: k, Value: v}
}

// SteamID logs a 64-bit Steam identifier.
func SteamID(v uint64) Field { return Field{Key: "steam_id", Value: v} }

// JobID logs an asynchronous correlation ID.
func JobID(v uint64) Field { return Field{Key: "job_id", Value: v} }

// EMsg logs a Steam protocol message type as a readable string.
func EMsg(v enums.EMsg) Field {
	return Field{Key: "emsg", Value: v.String()}
}

// EResult logs a Steam result code as a readable string.
func EResult(v enums.EResult) Field {
	return Field{Key: "eresult", Value: v.String()}
}

// Mask returns a hidden version of a sensitive string (e.g. "password123" -> "pa...23").
// If the string is 4 characters or shorter, it returns "****".
func Mask(s string) string {
	if len(s) <= 4 {
		return "****"
	}

	return s[:2] + "..." + s[len(s)-2:]
}

// MaskPath searches a string (like a file path or URL) for sensitive data and masks it.
func MaskPath(path, sensitive string) string {
	if sensitive == "" {
		return path
	}

	return strings.ReplaceAll(path, sensitive, Mask(sensitive))
}

type slogAdapter struct {
	l *slog.Logger
}

// FromSlog wraps the standard slog.Logger to implement Logger interface.
func FromSlog(l *slog.Logger) Logger {
	return &slogAdapter{l: l}
}

func (s *slogAdapter) Info(msg string, fields ...Field)  { s.log(slog.LevelInfo, msg, fields) }
func (s *slogAdapter) Debug(msg string, fields ...Field) { s.log(slog.LevelDebug, msg, fields) }
func (s *slogAdapter) Warn(msg string, fields ...Field)  { s.log(slog.LevelWarn, msg, fields) }
func (s *slogAdapter) Error(msg string, fields ...Field) { s.log(slog.LevelError, msg, fields) }

func (s *slogAdapter) log(lvl slog.Level, msg string, fields []Field) {
	attrs := make([]any, len(fields))
	for i, f := range fields {
		attrs[i] = slog.Any(f.Key, f.Value)
	}

	s.l.Log(context.Background(), lvl, msg, attrs...)
}

func (s *slogAdapter) With(fields ...Field) Logger {
	attrs := make([]any, len(fields))
	for i, f := range fields {
		attrs[i] = slog.Any(f.Key, f.Value)
	}

	return &slogAdapter{l: s.l.With(attrs...)}
}

func (s *slogAdapter) Close() error { return nil }

// Discard is a no-op Logger implementation. It is useful for unit tests
// or for disabling logging entirely in certain environments.
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

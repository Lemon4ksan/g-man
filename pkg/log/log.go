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
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/lemon4ksan/g-man/pkg/steam/protocol/enums"
)

// Level represents the severity of a log message.
//
// The logger ignores messages with a level lower than the configured threshold.
type Level int8

const (
	// LevelDebug is typically used in development to trace detailed application logic.
	LevelDebug Level = iota - 1
	// LevelInfo is the default logging level for general operational events.
	LevelInfo
	// LevelWarn represents non-critical issues that might require attention.
	LevelWarn
	// LevelError represents high-priority failures.
	LevelError
)

// Short returns a single-character string representation of the log level.
func (l Level) Short() string {
	switch l {
	case LevelDebug:
		return "D"
	case LevelInfo:
		return "I"
	case LevelWarn:
		return "W"
	case LevelError:
		return "E"
	default:
		return "?"
	}
}

// Field represents a single key-value pair used in structured logging.
//
// Avoid constructing this struct manually. Use the provided helper constructor
// functions such as [String] or [Any] instead.
type Field struct {
	// Key is the unique identifier for the logging attribute.
	Key string
	// Value is the raw value of the logging attribute.
	Value any
}

// Logger defines the primary interface for logging operations.
//
// All logging methods are safe for concurrent use. The context-aware methods
// allow downstream propagation of trace and correlation identifiers.
type Logger interface {
	// Debug logs a message at the Debug level.
	Debug(msg string, fields ...Field)

	// DebugContext logs a message at the Debug level with the provided context.
	DebugContext(ctx context.Context, msg string, fields ...Field)

	// Info logs a message at the Info level.
	Info(msg string, fields ...Field)

	// InfoContext logs a message at the Info level with the provided context.
	InfoContext(ctx context.Context, msg string, fields ...Field)

	// Warn logs a message at the Warn level.
	Warn(msg string, fields ...Field)

	// WarnContext logs a message at the Warn level with the provided context.
	WarnContext(ctx context.Context, msg string, fields ...Field)

	// Error logs a message at the Error level.
	Error(msg string, fields ...Field)

	// ErrorContext logs a message at the Error level with the provided context.
	ErrorContext(ctx context.Context, msg string, fields ...Field)

	// With returns a new Logger instance carrying the provided fields as context.
	//
	// If a field key is "module" or "component", it updates the module path
	// instead of adding a standard field.
	With(fields ...Field) Logger

	// Close flushes the asynchronous queue and stops the writer loop.
	//
	// It should be called exactly once during application shutdown.
	Close() error
}

// Config controls the behavior, destination, and visual style of the logger.
//
// To build a new configuration, use [DefaultConfig] as a starting point.
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

	// Colors enables ANSI terminal color codes in the output (ignored if JSON is true).
	Colors bool

	// FullPath, if true, prints "Mod › Sub › Service" instead of a tree structure.
	FullPath bool

	// PathSep is the separator used when FullPath is true. Defaults to " › ".
	PathSep string

	// AlignWidth is the horizontal offset where fields start, ensuring messages are aligned.
	AlignWidth int

	// JSON, if true, outputs logs as structured JSON lines instead of human-readable text.
	JSON bool

	// OmitFields defines a list of field keys to exclude from the printed output (e.g., "account", "steam_id").
	OmitFields []string
}

// DefaultConfig returns a configuration balanced for local development.
//
// It enables colors, tree-style modules, a buffer size of 2048, and standard text output.
// If the level is invalid, it defaults to [LevelInfo].
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
		JSON:       false,
	}
}

// AsyncLogger implements the [Logger] interface with a non-blocking background writer.
//
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
// If the configuration's Output is nil, it defaults to [os.Stdout].
// Callers must call [AsyncLogger.Close] on the returned Logger during shutdown to ensure
// all logs are flushed.
func New(cfg Config) Logger {
	if cfg.Output == nil {
		cfg.Output = os.Stdout
	}

	l := &AsyncLogger{
		cfg:   cfg,
		queue: make(chan *bytes.Buffer, cfg.AsyncSize),
		wg:    &sync.WaitGroup{},
	}
	l.wg.Go(l.writerLoop)

	return l
}

// Close gracefully shuts down the logger.
// It returns nil and never errors.
func (l *AsyncLogger) Close() error {
	if l.closed.Swap(true) {
		return nil
	}

	l.mu.Lock()
	close(l.queue)
	l.mu.Unlock()

	l.wg.Wait()

	return nil
}

// With appends fields to the current logger context.
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
func (l *AsyncLogger) Debug(msg string, f ...Field) { l.log(LevelDebug, msg, f) }

// DebugContext logs a message at the Debug level with the provided context.
func (l *AsyncLogger) DebugContext(ctx context.Context, msg string, f ...Field) {
	l.logCtx(ctx, LevelDebug, msg, f)
}

// Info logs a message at the Info level.
func (l *AsyncLogger) Info(msg string, f ...Field) { l.log(LevelInfo, msg, f) }

// InfoContext logs a message at the Info level with the provided context.
func (l *AsyncLogger) InfoContext(ctx context.Context, msg string, f ...Field) {
	l.logCtx(ctx, LevelInfo, msg, f)
}

// Warn logs a message at the Warn level.
func (l *AsyncLogger) Warn(msg string, f ...Field) { l.log(LevelWarn, msg, f) }

// WarnContext logs a message at the Warn level with the provided context.
func (l *AsyncLogger) WarnContext(ctx context.Context, msg string, f ...Field) {
	l.logCtx(ctx, LevelWarn, msg, f)
}

// Error logs a message at the Error level.
func (l *AsyncLogger) Error(msg string, f ...Field) { l.log(LevelError, msg, f) }

// ErrorContext logs a message at the Error level with the provided context.
func (l *AsyncLogger) ErrorContext(ctx context.Context, msg string, f ...Field) {
	l.logCtx(ctx, LevelError, msg, f)
}

func (l *AsyncLogger) log(lvl Level, msg string, fields []Field) {
	l.logCtx(context.Background(), lvl, msg, fields)
}

func (l *AsyncLogger) logCtx(ctx context.Context, lvl Level, msg string, fields []Field) {
	if lvl < l.cfg.Level || l.closed.Load() {
		return
	}

	buf := bufPool.Get().(*bytes.Buffer)
	buf.Reset()

	if ctx != nil {
		if id, ok := CorrelationID(ctx); ok {
			fieldsWithCorr := make([]Field, 0, len(fields)+1)
			fieldsWithCorr = append(fieldsWithCorr, fields...)
			fieldsWithCorr = append(fieldsWithCorr, String("correlation_id", id))
			fields = fieldsWithCorr
		}
	}

	if l.cfg.JSON {
		l.formatJSON(buf, lvl, msg, fields)
	} else {
		l.format(buf, lvl, msg, fields)
	}

	l.mu.RLock()

	if l.closed.Load() {
		l.mu.RUnlock()
		bufPool.Put(buf)
		return
	}

	select {
	case l.queue <- buf:
	default:
		l.dropped.Add(1)
		bufPool.Put(buf)
	}

	l.mu.RUnlock()
}

func (l *AsyncLogger) writerLoop() {
	for buf := range l.queue {
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

func (l *AsyncLogger) formatJSON(b *bytes.Buffer, lvl Level, msg string, callFields []Field) {
	b.WriteString(`{"time":`)
	b.WriteString(strconv.Quote(time.Now().Format(l.cfg.TimeFormat)))
	b.WriteString(`,"level":`)

	switch lvl {
	case LevelDebug:
		b.WriteString(`"DEBUG"`)
	case LevelInfo:
		b.WriteString(`"INFO"`)
	case LevelWarn:
		b.WriteString(`"WARN"`)
	case LevelError:
		b.WriteString(`"ERROR"`)
	default:
		b.WriteString(`"UNKNOWN"`)
	}

	if len(l.path) > 0 {
		b.WriteString(`,"module":[`)

		for i, p := range l.path {
			if i > 0 {
				b.WriteByte(',')
			}

			b.WriteString(strconv.Quote(p))
		}

		b.WriteByte(']')
	}

	b.WriteString(`,"message":`)
	b.WriteString(strconv.Quote(msg))

	writeFieldJSON := func(f Field) {
		if f.Key == "" {
			return
		}

		if slices.Contains(l.cfg.OmitFields, f.Key) {
			return
		}

		b.WriteByte(',')
		b.WriteString(strconv.Quote(f.Key))
		b.WriteByte(':')

		switch val := f.Value.(type) {
		case string:
			b.WriteString(strconv.Quote(val))
		case int:
			b.WriteString(strconv.Itoa(val))
		case int8:
			b.WriteString(strconv.FormatInt(int64(val), 10))
		case int16:
			b.WriteString(strconv.FormatInt(int64(val), 10))
		case int32:
			b.WriteString(strconv.FormatInt(int64(val), 10))
		case int64:
			b.WriteString(strconv.FormatInt(val, 10))
		case uint:
			b.WriteString(strconv.FormatUint(uint64(val), 10))
		case uint8:
			b.WriteString(strconv.FormatUint(uint64(val), 10))
		case uint16:
			b.WriteString(strconv.FormatUint(uint64(val), 10))
		case uint32:
			b.WriteString(strconv.FormatUint(uint64(val), 10))
		case uint64:
			b.WriteString(strconv.FormatUint(val, 10))
		case float32:
			b.WriteString(strconv.FormatFloat(float64(val), 'g', -1, 32))
		case float64:
			b.WriteString(strconv.FormatFloat(val, 'g', -1, 64))
		case bool:
			if val {
				b.WriteString("true")
			} else {
				b.WriteString("false")
			}

		case error:
			b.WriteString(strconv.Quote(val.Error()))
		case []byte:
			b.WriteString(strconv.Quote(formatValue(val)))
		case time.Duration:
			b.WriteString(strconv.Quote(val.String()))
		case time.Time:
			var buf [32]byte
			b.WriteString(strconv.Quote(string(val.AppendFormat(buf[:0], "15:04:05.000"))))
		case net.IP:
			b.WriteString(strconv.Quote(val.String()))
		default:
			b.WriteString(strconv.Quote(fmt.Sprintf("%+v", val)))
		}
	}

	for _, f := range l.context {
		writeFieldJSON(f)
	}

	for _, f := range callFields {
		writeFieldJSON(f)
	}

	b.WriteString("}\n")
}

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

		if slices.Contains(l.cfg.OmitFields, f.Key) {
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
	case LevelDebug:
		return ansiMagenta
	case LevelInfo:
		return ansiGreen
	case LevelWarn:
		return ansiYellow
	case LevelError:
		return ansiRedBold
	default:
		return ansiReset
	}
}

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
			var buf [64]byte

			b := buf[:0]
			b = append(b, "[ "...)
			b = strconv.AppendInt(b, int64(len(val)), 10)
			b = append(b, " bytes | preview: "...)
			offset := len(b)
			b = b[:offset+32]
			hex.Encode(b[offset:], val[:16])
			b = append(b, "..."...)
			b = append(b, " ]"...)

			return string(b)
		}

		return hex.EncodeToString(val)

	case error:
		return val.Error()
	case time.Duration:
		return val.String()
	case time.Time:
		var buf [32]byte
		return string(val.AppendFormat(buf[:0], "15:04:05.000"))
	case net.IP:
		return val.String()
	default:
		return fmt.Sprintf("%+v", v) // Fallback for complex reflection formatting
	}
}

// Global buffer pool to reduce GC pressure for high-frequency logging.
var bufPool = sync.Pool{
	New: func() any { return new(bytes.Buffer) },
}

// String returns a Field with a string value.
func String(k, v string) Field { return Field{Key: k, Value: v} }

// Int returns a Field with an integer value.
func Int(k string, v int) Field { return Field{Key: k, Value: v} }

// Int32 returns a Field with an int32 value.
func Int32(k string, v int32) Field { return Field{Key: k, Value: v} }

// Int64 returns a Field with an int64 value.
func Int64(k string, v int64) Field { return Field{Key: k, Value: v} }

// Uint returns a Field with an unsigned integer value.
func Uint(k string, v uint) Field { return Field{Key: k, Value: v} }

// Uint32 returns a Field with a uint32 value.
func Uint32(k string, v uint32) Field { return Field{Key: k, Value: v} }

// Uint64 returns a Field with a uint64 value.
func Uint64(k string, v uint64) Field { return Field{Key: k, Value: v} }

// Float64 returns a Field with a float64 value.
func Float64(k string, v float64) Field { return Field{Key: k, Value: v} }

// Bool returns a Field with a boolean value.
func Bool(k string, v bool) Field { return Field{Key: k, Value: v} }

// Duration returns a Field with a time.Duration value.
func Duration(k string, v time.Duration) Field { return Field{Key: k, Value: v} }

// Time returns a Field with a time.Time value.
func Time(k string, v time.Time) Field { return Field{Key: k, Value: v} }

// Err returns a Field containing the provided error with the key "error".
func Err(err error) Field { return Field{Key: "error", Value: err} }

// Any returns a Field containing an arbitrary value.
func Any(k string, v any) Field { return Field{Key: k, Value: v} }

// Module returns a Field defining a new level in the logger's module hierarchy.
func Module(name string) Field { return Field{Key: "module", Value: name} }

// Component returns a Field defining a subsection of a module.
func Component(name string) Field { return Field{Key: "component", Value: name} }

// Strings returns a Field with a slice of strings.
func Strings(k string, v []string) Field { return Field{Key: k, Value: v} }

// Ints returns a Field with a slice of integers.
func Ints(k string, v []int) Field { return Field{Key: k, Value: v} }

// Uints returns a Field with a slice of unsigned integers.
func Uints(k string, v []uint) Field { return Field{Key: k, Value: v} }

// Bools returns a Field with a slice of booleans.
func Bools(k string, v []bool) Field { return Field{Key: k, Value: v} }

// Bytes returns a Field containing a byte slice logged as a hex string.
func Bytes(k string, v []byte) Field { return Field{Key: k, Value: v} }

// ByteString returns a Field containing a byte slice logged as a standard string.
func ByteString(k string, v []byte) Field { return Field{Key: k, Value: string(v)} }

// HexF returns a Field containing a byte slice formatted as a hex string.
func HexF(k string, v []byte) Field { return Field{Key: k, Value: hex.EncodeToString(v)} }

// IP returns a Field containing a net.IP value as a string.
func IP(k string, v net.IP) Field { return Field{Key: k, Value: v.String()} }

// Port returns a Field with a network port.
func Port(k string, v int) Field { return Field{Key: k, Value: v} }

// HostPort returns a Field with a combined host and port string.
func HostPort(k, h string, p int) Field {
	return Field{Key: k, Value: fmt.Sprintf("%s:%d", h, p)}
}

// StringOpt returns a Field if the value is not empty, otherwise it returns an empty Field.
func StringOpt(k, v string) Field {
	if v == "" {
		return Field{}
	}

	return Field{Key: k, Value: v}
}

// IntOpt returns a Field if the value is not zero, otherwise it returns an empty Field.
func IntOpt(k string, v int) Field {
	if v == 0 {
		return Field{}
	}

	return Field{Key: k, Value: v}
}

// SteamID returns a Field with a 64-bit Steam identifier.
func SteamID(v uint64) Field { return Field{Key: "steam_id", Value: v} }

// JobID returns a Field with a 64-bit job correlation ID.
func JobID(v uint64) Field { return Field{Key: "job_id", Value: v} }

// EMsg returns a Field representing a Steam protocol message type.
func EMsg(v enums.EMsg) Field {
	return Field{Key: "emsg", Value: v.String()}
}

// EResult returns a Field representing a Steam result code.
func EResult(v enums.EResult) Field {
	return Field{Key: "eresult", Value: v.String()}
}

// Mask returns a hidden version of a sensitive string.
func Mask(s string) string {
	if len(s) <= 4 {
		return "****"
	}

	return s[:2] + "..." + s[len(s)-2:]
}

// MaskPath searches a path string for sensitive data and masks it.
func MaskPath(path, sensitive string) string {
	if sensitive == "" {
		return path
	}

	return strings.ReplaceAll(path, sensitive, Mask(sensitive))
}

type slogAdapter struct {
	l *slog.Logger
}

// FromSlog wraps the standard slog.Logger to implement the [Logger] interface.
func FromSlog(l *slog.Logger) Logger {
	return &slogAdapter{l: l}
}

func (s *slogAdapter) Info(msg string, fields ...Field) {
	s.log(context.Background(), slog.LevelInfo, msg, fields)
}

func (s *slogAdapter) Debug(msg string, fields ...Field) {
	s.log(context.Background(), slog.LevelDebug, msg, fields)
}

func (s *slogAdapter) Warn(msg string, fields ...Field) {
	s.log(context.Background(), slog.LevelWarn, msg, fields)
}

func (s *slogAdapter) Error(msg string, fields ...Field) {
	s.log(context.Background(), slog.LevelError, msg, fields)
}

func (s *slogAdapter) InfoContext(ctx context.Context, msg string, fields ...Field) {
	s.log(ctx, slog.LevelInfo, msg, fields)
}

func (s *slogAdapter) DebugContext(ctx context.Context, msg string, fields ...Field) {
	s.log(ctx, slog.LevelDebug, msg, fields)
}

func (s *slogAdapter) WarnContext(ctx context.Context, msg string, fields ...Field) {
	s.log(ctx, slog.LevelWarn, msg, fields)
}

func (s *slogAdapter) ErrorContext(ctx context.Context, msg string, fields ...Field) {
	s.log(ctx, slog.LevelError, msg, fields)
}

func (s *slogAdapter) log(ctx context.Context, lvl slog.Level, msg string, fields []Field) {
	var attrs []any
	if ctx != nil {
		if id, ok := CorrelationID(ctx); ok {
			attrs = append(attrs, slog.String("correlation_id", id))
		}
	}

	for _, f := range fields {
		attrs = append(attrs, slog.Any(f.Key, f.Value))
	}

	s.l.Log(ctx, lvl, msg, attrs...)
}

func (s *slogAdapter) With(fields ...Field) Logger {
	attrs := make([]any, len(fields))
	for i, f := range fields {
		attrs[i] = slog.Any(f.Key, f.Value)
	}

	return &slogAdapter{l: s.l.With(attrs...)}
}

func (s *slogAdapter) Close() error { return nil }

// Discard is a no-op Logger implementation useful for unit tests or disabling logging.
var Discard Logger = &discard{}

type discard struct{}

func (d *discard) Close() error                                             { return nil }
func (d *discard) With(fields ...Field) Logger                              { return d }
func (d *discard) Debug(msg string, f ...Field)                             { _ = msg }
func (d *discard) DebugContext(ctx context.Context, msg string, f ...Field) { _ = msg }
func (d *discard) Info(msg string, f ...Field)                              { _ = msg }
func (d *discard) InfoContext(ctx context.Context, msg string, f ...Field)  { _ = msg }
func (d *discard) Warn(msg string, f ...Field)                              { _ = msg }
func (d *discard) WarnContext(ctx context.Context, msg string, f ...Field)  { _ = msg }
func (d *discard) Error(msg string, f ...Field)                             { _ = msg }
func (d *discard) ErrorContext(ctx context.Context, msg string, f ...Field) { _ = msg }

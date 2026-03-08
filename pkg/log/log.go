// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package log

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"io"
	"net"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/lemon4ksan/g-man/pkg/steam/protocol"
)

// Level represents log severity.
type Level int8

const (
	DebugLevel Level = iota - 1
	InfoLevel
	WarnLevel
	ErrorLevel
)

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

// Field is a key-value pair for structured logging.
type Field struct {
	Key   string
	Value any
}

// Logger interface.
type Logger interface {
	Debug(msg string, fields ...Field)
	Info(msg string, fields ...Field)
	Warn(msg string, fields ...Field)
	Error(msg string, fields ...Field)
	With(fields ...Field) Logger
	WithModule(string) Logger
	Close() error
	IsDebugEnabled() bool
}

// Config controls logger behavior.
type Config struct {
	Level        Level
	Output       io.Writer
	TimeFormat   string
	AsyncSize    int    // Buffer size for async channel
	DebugEnabled bool   // Ignore Debug logs if false
	Colors       bool   // Enable ANSI colors
	FullPath     bool   // Show full module path vs tree style
	PathSep      string // Path separator for FullPath mode
	AlignWidth   int    // Minimum width before printing inline fields
}

// DefaultConfig returns sensible defaults balancing beauty and performance.
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

// AsyncLogger implements a high-performance, beautiful, non-blocking logger.
type AsyncLogger struct {
	cfg     Config
	path    []string
	context []Field

	queue chan *bytes.Buffer
	wg    *sync.WaitGroup
}

// New creates and starts an AsyncLogger.
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

func (l *AsyncLogger) Close() error {
	close(l.queue)
	l.wg.Wait()
	return nil
}

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
			val := fmt.Sprint(f.Value)
			if len(child.path) == 0 || child.path[len(child.path)-1] != val {
				child.path = append(child.path, val)
			}
		} else {
			child.context = append(child.context, f)
		}
	}

	return child
}

func (l *AsyncLogger) WithModule(mod string) Logger {
	child := &AsyncLogger{
		cfg:     l.cfg,
		queue:   l.queue,
		wg:      l.wg,
		path:    make([]string, len(l.path)),
		context: make([]Field, len(l.context), len(l.context)+1),
	}

	copy(child.path, l.path)
	copy(child.context, l.context)

	if len(child.path) == 0 || child.path[len(child.path)-1] != mod {
		child.path = append(child.path, mod)
	}

	return child
}

func (l *AsyncLogger) Debug(msg string, f ...Field) { l.log(DebugLevel, msg, f) }
func (l *AsyncLogger) Info(msg string, f ...Field)  { l.log(InfoLevel, msg, f) }
func (l *AsyncLogger) Warn(msg string, f ...Field)  { l.log(WarnLevel, msg, f) }
func (l *AsyncLogger) Error(msg string, f ...Field) { l.log(ErrorLevel, msg, f) }

func (l *AsyncLogger) IsDebugEnabled() bool { return l.cfg.DebugEnabled }

func (l *AsyncLogger) log(lvl Level, msg string, fields []Field) {
	if lvl < l.cfg.Level {
		return
	}

	buf := bufPool.Get().(*bytes.Buffer)
	buf.Reset()

	l.format(buf, lvl, msg, fields)

	select {
	case l.queue <- buf:
	default:
		// Queue is full, drop to avoid blocking and return buffer
		bufPool.Put(buf)
	}
}

func (l *AsyncLogger) writerLoop() {
	defer l.wg.Done()
	for buf := range l.queue {
		_, _ = l.cfg.Output.Write(buf.Bytes())
		bufPool.Put(buf)
	}
}

// ANSI Escape Codes
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

func (l *AsyncLogger) format(b *bytes.Buffer, lvl Level, msg string, callFields []Field) {
	visibleLen := 0 // Tracks visual length for alignment

	// Time
	ts := time.Now().Format(l.cfg.TimeFormat)
	l.writeColor(b, ansiGray)
	b.WriteString(ts)
	l.writeColor(b, ansiReset)
	b.WriteByte(' ')
	visibleLen += len(ts) + 1

	// Level
	l.writeColor(b, levelColor(lvl))
	lvlStr := fmt.Sprintf("[%s]", lvl.Short())
	b.WriteString(lvlStr)
	l.writeColor(b, ansiReset)
	b.WriteByte(' ')
	visibleLen += len(lvlStr) + 1

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

	// Separate inline and block fields
	// Optimisation: use simple slices, they are tiny and GC eats them fast
	var inline, blocks []Field
	var inlineStrs []string
	var blockStrs []string

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
			b.WriteString("=")
			l.writeColor(b, ansiReset)
			b.WriteString(inlineStrs[i])
			b.WriteByte(' ')
		}
	}
	b.WriteByte('\n')

	// Write Block Fields (Multi-line / Long)
	if len(blocks) > 0 {
		paddingStr := strings.Repeat(" ", l.blockPadding(depth))
		for i, f := range blocks {
			b.WriteString(paddingStr)
			l.writeColor(b, ansiCyan)
			b.WriteString(f.Key)
			b.WriteString(":")
			l.writeColor(b, ansiReset)
			b.WriteByte(' ')
			b.WriteString(blockStrs[i])
			b.WriteByte('\n')
		}
	}
}

func (l *AsyncLogger) blockPadding(depth int) int {
	base := len(l.cfg.TimeFormat) + 5 // Time + "[L] "
	if depth > 0 {
		if l.cfg.FullPath {
			pathStr := strings.Join(l.path, l.cfg.PathSep)
			base += len(pathStr) + 1
		} else {
			base += (depth-1)*3 + 3 // Indent + "└─ "
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

// formatValue stringifies a value quickly without reflection where possible
func formatValue(v any) string {
	switch val := v.(type) {
	case string:
		if strings.Contains(val, " ") {
			return fmt.Sprintf("%q", val)
		}
		return val
	case int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64:
		return fmt.Sprintf("%d", val)
	case float32, float64:
		return fmt.Sprintf("%g", val)
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
		return fmt.Sprintf("%+v", v)
	}
}

// Global buffer pool. Shared across all loggers to reduce GC pressure.
var bufPool = sync.Pool{
	New: func() any { return new(bytes.Buffer) },
}

// --- Field Helpers (Zero alloc wrappers) ---

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

// Slices / Complex types
func Strings(k string, v []string) Field  { return Field{Key: k, Value: v} }
func Ints(k string, v []int) Field        { return Field{Key: k, Value: v} }
func Uints(k string, v []uint) Field      { return Field{Key: k, Value: v} }
func Bools(k string, v []bool) Field      { return Field{Key: k, Value: v} }
func Bytes(k string, v []byte) Field      { return Field{Key: k, Value: v} }
func ByteString(k string, v []byte) Field { return Field{Key: k, Value: string(v)} }
func HexF(k string, v []byte) Field       { return Field{Key: k, Value: hex.EncodeToString(v)} }

// Network
func IP(k string, v net.IP) Field { return Field{Key: k, Value: v.String()} }
func Port(k string, v int) Field  { return Field{Key: k, Value: v} }
func HostPort(k string, h string, p int) Field {
	return Field{Key: k, Value: fmt.Sprintf("%s:%d", h, p)}
}

// Optional helpers
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

// Steam Specific Helpers
func SteamID(k string, v uint64) Field { return Field{Key: k, Value: v} }
func JobID(v uint64) Field             { return Field{Key: "job_id", Value: v} }
func EMsg(k string, v protocol.EMsg) Field {
	return Field{Key: k, Value: v.String()}
}
func EResult(v protocol.EResult) Field {
	return Field{Key: "eresult", Value: v.String()}
}

// Discard Logger Pattern
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

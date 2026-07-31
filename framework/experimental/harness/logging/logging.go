// Package logging implements the harness's own operational logs
// (distinct from the per-session event log in package session).
//
// Per § Logging:
//
//   - Structured JSON, one object per line.
//   - Daily-rotated files at
//     ~/.local/state/gofastr/harness/log/harness-YYYYMMDD.log.
//   - Components: engine, multiplex, provider.<name>, transport.<name>,
//     mcpclient, mcpserver, skill, hook, auth.
//   - Per-component level via --log-level engine=debug,provider.copilot=trace.
package logging

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Level is the log level enum.
type Level uint8

const (
	LevelTrace Level = iota
	LevelDebug
	LevelInfo
	LevelWarn
	LevelError
)

// String returns the lowercase name.
func (l Level) String() string {
	switch l {
	case LevelTrace:
		return "trace"
	case LevelDebug:
		return "debug"
	case LevelInfo:
		return "info"
	case LevelWarn:
		return "warn"
	case LevelError:
		return "error"
	default:
		return "info"
	}
}

// ParseLevel parses a level string.
func ParseLevel(s string) (Level, error) {
	switch strings.ToLower(s) {
	case "trace":
		return LevelTrace, nil
	case "debug":
		return LevelDebug, nil
	case "info":
		return LevelInfo, nil
	case "warn", "warning":
		return LevelWarn, nil
	case "error":
		return LevelError, nil
	}
	return LevelInfo, fmt.Errorf("logging: unknown level %q", s)
}

// Logger writes structured JSON log lines.
type Logger struct {
	mu        sync.Mutex
	out       io.Writer
	component string
	level     Level

	// perComponent overrides level per component name.
	perComponent map[string]Level
}

// New returns a Logger writing to out at the given default level.
func New(out io.Writer, defaultLevel Level) *Logger {
	return &Logger{
		out:          out,
		level:        defaultLevel,
		perComponent: make(map[string]Level),
	}
}

// WithComponent returns a child Logger tagged with the given
// component name. Component levels override the default.
func (l *Logger) WithComponent(component string) *Logger {
	return &Logger{
		out:          l.out,
		component:    component,
		level:        l.level,
		perComponent: l.perComponent,
	}
}

// SetComponentLevel overrides the level for one component.
func (l *Logger) SetComponentLevel(component string, level Level) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.perComponent[component] = level
}

// ApplyOverrides parses a string like
// "engine=debug,provider.openrouter=trace" and updates component
// levels accordingly.
func (l *Logger) ApplyOverrides(spec string) error {
	if spec == "" {
		return nil
	}
	for _, part := range strings.Split(spec, ",") {
		p := strings.TrimSpace(part)
		if p == "" {
			continue
		}
		eq := strings.Index(p, "=")
		if eq < 0 {
			return fmt.Errorf("logging: bad override %q (want component=level)", p)
		}
		lvl, err := ParseLevel(strings.TrimSpace(p[eq+1:]))
		if err != nil {
			return err
		}
		l.SetComponentLevel(strings.TrimSpace(p[:eq]), lvl)
	}
	return nil
}

// effective returns the level for the logger's current component.
func (l *Logger) effective() Level {
	if l.component == "" {
		return l.level
	}
	l.mu.Lock()
	if lvl, ok := l.perComponent[l.component]; ok {
		l.mu.Unlock()
		return lvl
	}
	l.mu.Unlock()
	return l.level
}

// Log writes one structured record. Use Trace/Debug/Info/Warn/Error
// helpers for level-tagged shortcuts.
func (l *Logger) Log(level Level, msg string, fields map[string]any) {
	if level < l.effective() {
		return
	}
	rec := map[string]any{
		"ts":    time.Now().UTC().Format(time.RFC3339Nano),
		"level": level.String(),
		"msg":   msg,
	}
	if l.component != "" {
		rec["component"] = l.component
	}
	for k, v := range fields {
		rec[k] = v
	}
	data, err := json.Marshal(rec)
	if err != nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	_, _ = l.out.Write(data)
	_, _ = l.out.Write([]byte{'\n'})
}

// Trace / Debug / Info / Warn / Error are level-tagged shortcuts.
func (l *Logger) Trace(msg string, kv ...any) { l.Log(LevelTrace, msg, kvToMap(kv)) }
func (l *Logger) Debug(msg string, kv ...any) { l.Log(LevelDebug, msg, kvToMap(kv)) }
func (l *Logger) Info(msg string, kv ...any)  { l.Log(LevelInfo, msg, kvToMap(kv)) }
func (l *Logger) Warn(msg string, kv ...any)  { l.Log(LevelWarn, msg, kvToMap(kv)) }
func (l *Logger) Error(msg string, kv ...any) { l.Log(LevelError, msg, kvToMap(kv)) }

func kvToMap(kv []any) map[string]any {
	if len(kv) == 0 {
		return nil
	}
	if len(kv)%2 != 0 {
		// Odd kv list: bag the leftover under "extra".
		kv = append(kv, "<missing>")
	}
	m := make(map[string]any, len(kv)/2)
	for i := 0; i < len(kv); i += 2 {
		k, ok := kv[i].(string)
		if !ok {
			k = fmt.Sprintf("%v", kv[i])
		}
		m[k] = kv[i+1]
	}
	return m
}

// DailyFileWriter writes to a per-day file at the given directory,
// rotating at UTC midnight.
type DailyFileWriter struct {
	dir string

	mu      sync.Mutex
	current *os.File
	day     string // YYYY-MM-DD that current was opened on
}

// NewDailyFileWriter returns a writer that opens
// `<dir>/harness-YYYYMMDD.log` and rotates on day change.
func NewDailyFileWriter(dir string) (*DailyFileWriter, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	return &DailyFileWriter{dir: dir}, nil
}

func (w *DailyFileWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	today := time.Now().UTC().Format("20060102")
	if w.current == nil || w.day != today {
		if w.current != nil {
			_ = w.current.Close()
		}
		path := filepath.Join(w.dir, "harness-"+today+".log")
		f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
		if err != nil {
			return 0, err
		}
		w.current = f
		w.day = today
	}
	return w.current.Write(p)
}

// Close closes the underlying file.
func (w *DailyFileWriter) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.current == nil {
		return nil
	}
	err := w.current.Close()
	w.current = nil
	return err
}

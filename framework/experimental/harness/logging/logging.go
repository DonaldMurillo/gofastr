package logging

import (
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Level is a log level, ordered Trace < Debug < Info < Warn < Error.
type Level uint8

const (
	LevelTrace Level = iota
	LevelDebug
	LevelInfo
	LevelWarn
	LevelError
)

// String returns the lowercase name, "info" for an unknown level.
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

// ParseLevel parses a level name, case-insensitive, accepting "warning" for warn.
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

// Logger writes one JSON object per line, filtered by a default and per-component level.
type Logger struct {
	mu           sync.Mutex
	out          io.Writer
	component    string
	level        Level
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

// WithComponent returns a Logger that tags records with component and shares the parent's writer and overrides.
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

// ApplyOverrides applies a spec like "engine=debug,provider.openrouter=trace" as component levels.
func (l *Logger) ApplyOverrides(spec string) error {
	if spec == "" {
		return nil
	}
	for part := range strings.SplitSeq(spec, ",") {
		p := strings.TrimSpace(part)
		if p == "" {
			continue
		}
		before, after, ok := strings.Cut(p, "=")
		if !ok {
			return fmt.Errorf("logging: bad override %q (want component=level)", p)
		}
		lvl, err := ParseLevel(strings.TrimSpace(after))
		if err != nil {
			return err
		}
		l.SetComponentLevel(strings.TrimSpace(before), lvl)
	}
	return nil
}

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

// Log writes one record with ts, level, msg, component and fields when level passes the filter.
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
	maps.Copy(rec, fields)
	data, err := json.Marshal(rec)
	if err != nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	_, _ = l.out.Write(data)
	_, _ = l.out.Write([]byte{'\n'})
}

// Trace, Debug, Info, Warn and Error log at their level with alternating key, value pairs.
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

// DailyFileWriter appends to <dir>/harness-YYYYMMDD.log, switching file at UTC midnight.
type DailyFileWriter struct {
	dir string

	mu      sync.Mutex
	current *os.File
	day     string
}

// NewDailyFileWriter creates dir (0700) and returns a writer into it.
func NewDailyFileWriter(dir string) (*DailyFileWriter, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
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
		f, err := openDailyFile(path)
		if err != nil {
			return 0, err
		}
		w.current = f
		w.day = today
	}
	return w.current.Write(p)
}

func openDailyFile(path string) (*os.File, error) {
	if fi, err := os.Lstat(path); err == nil && fi.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("logging: %s is a symlink; refusing to write the harness log through it", path)
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, err
	}
	if err := f.Chmod(0o600); err != nil {
		_ = f.Close()
		return nil, err
	}
	return f, nil
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

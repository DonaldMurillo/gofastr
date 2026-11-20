package log

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/DonaldMurillo/gofastr/core/textsafe"
	"golang.org/x/term"
)

// ConsoleMode controls whether a human-readable colorized console sink is
// attached alongside the file/webhook sinks.
type ConsoleMode int

const (
	// ConsoleAuto is the zero value: the console sink is attached only when
	// stderr is a terminal and NO_COLOR is unset.
	ConsoleAuto ConsoleMode = iota
	// ConsoleOn forces the console sink on regardless of TTY.
	ConsoleOn
	// ConsoleOff disables the console sink entirely.
	ConsoleOff
)

// ConsoleOpts configures a console sink.
type ConsoleOpts struct {
	// Writer is the destination.
	Writer io.Writer

	// Color forces color on (true) or off (false).
	Color *bool

	// TimeFormat overrides the timestamp format.
	TimeFormat string
}

// ConsoleSink is a Sink that renders each JSON entry as a single
// human-readable, optionally colorized line:
func ConsoleSink(opts ConsoleOpts) Sink {
	w := opts.Writer
	if w == nil {
		w = os.Stderr
	}
	color := opts.Color != nil && *opts.Color
	if opts.Color == nil {
		color = shouldColor(w)
	}
	timeFmt := opts.TimeFormat
	if timeFmt == "" {
		timeFmt = "15:04:05.000"
	}
	a := ansiNone
	if color {
		a = ansiColor
	}
	return &consoleSink{w: w, a: a, timeFmt: timeFmt}
}

type consoleSink struct {
	mu      sync.Mutex
	w       io.Writer
	a       ansi
	timeFmt string
	closed  bool
}

type ansi struct {
	reset, dim, bold                     string
	gray, red, green, yellow, blue, cyan string
}

var ansiColor = ansi{
	reset: "\x1b[0m", dim: "\x1b[2m", bold: "\x1b[1m",
	gray: "\x1b[90m", red: "\x1b[31m", green: "\x1b[32m",
	yellow: "\x1b[33m", blue: "\x1b[34m", cyan: "\x1b[36m",
}

var ansiNone ansi

func shouldColor(w io.Writer) bool {
	if v, ok := os.LookupEnv("NO_COLOR"); ok && v != "" {
		return false
	}
	if f, ok := w.(*os.File); ok {
		return term.IsTerminal(int(f.Fd()))
	}
	return false
}

func (s *consoleSink) Write(entry []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return ErrSinkClosed
	}
	if line, ok := s.render(entry); ok {
		_, err := s.w.Write(line)
		return err
	}
	_, err := io.WriteString(s.w, escapeUnsafe(string(entry))+"\n")
	return err
}

func (s *consoleSink) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closed = true
	return nil
}

type kv struct {
	key string
	val json.RawMessage
}

func (s *consoleSink) render(entry []byte) ([]byte, bool) {
	dec := json.NewDecoder(bytes.NewReader(entry))
	tok, err := dec.Token()
	if err != nil || tok != json.Delim('{') {
		return nil, false
	}
	var ts, level, msg string
	var rest []kv
	for dec.More() {
		ktok, err := dec.Token()
		if err != nil {
			return nil, false
		}
		key, ok := ktok.(string)
		if !ok {
			return nil, false
		}
		var raw json.RawMessage
		if err := dec.Decode(&raw); err != nil {
			return nil, false
		}
		switch key {
		case "time":
			ts = escapeUnsafe(jsonText(raw))
		case "level":
			level = escapeUnsafe(jsonText(raw))
		case "msg":
			msg = escapeUnsafe(jsonText(raw))
		default:
			rest = append(rest, kv{key: escapeUnsafe(key), val: raw})
		}
	}

	var b strings.Builder
	if ts != "" {
		b.WriteString(s.a.dim)
		if t, err := time.Parse(time.RFC3339Nano, ts); err == nil {
			b.WriteString(t.Format(s.timeFmt))
		} else {
			b.WriteString(ts)
		}
		b.WriteString(s.a.reset)
		b.WriteByte(' ')
	}
	b.WriteString(levelColor(s.a, level))
	b.WriteString(padLevel(level))
	b.WriteString(s.a.reset)
	b.WriteByte(' ')
	if msg != "" {
		b.WriteString(s.a.bold)
		b.WriteString(msg)
		b.WriteString(s.a.reset)
	}
	for _, p := range rest {
		b.WriteByte(' ')
		b.WriteString(s.a.gray)
		b.WriteString(p.key)
		b.WriteString(s.a.reset)
		b.WriteByte('=')
		b.WriteString(s.a.cyan)
		b.WriteString(formatValue(p.val))
		b.WriteString(s.a.reset)
	}
	b.WriteByte('\n')
	return []byte(b.String()), true
}

func levelColor(a ansi, level string) string {
	switch level {
	case "DEBUG":
		return a.gray
	case "INFO":
		return a.blue
	case "WARN":
		return a.yellow
	case "ERROR":
		return a.red
	default:
		return ""
	}
}

func padLevel(level string) string {
	const width = 5
	if len(level) >= width {
		return level
	}
	return level + strings.Repeat(" ", width-len(level))
}

func formatValue(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	if raw[0] == '"' {
		var s string
		if err := json.Unmarshal(raw, &s); err != nil {
			return string(raw)
		}
		if needsQuoting(s) {
			return quoteTerminalSafe(s)
		}
		return s
	}
	return string(raw)
}

func jsonText(raw json.RawMessage) string {
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s
	}
	return string(raw)
}

func escapeUnsafe(s string) string {
	if !textsafe.ContainsUnsafe(s) {
		return s
	}
	var b strings.Builder
	b.Grow(len(s) + 8)
	for _, r := range s {
		switch {
		case r == '\n':
			b.WriteString(`\n`)
		case r == '\r':
			b.WriteString(`\r`)
		case r == '\t':
			b.WriteString(`\t`)
		case textsafe.IsUnsafe(r):
			fmt.Fprintf(&b, `\u%04x`, r)
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

func quoteTerminalSafe(s string) string {
	var b strings.Builder
	b.Grow(len(s) + 2)
	b.WriteByte('"')
	for _, r := range s {
		switch r {
		case '"':
			b.WriteString(`\"`)
		case '\\':
			b.WriteString(`\\`)
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		case '\t':
			b.WriteString(`\t`)
		default:
			if textsafe.IsUnsafe(r) {
				fmt.Fprintf(&b, `\u%04x`, r)
				continue
			}
			b.WriteRune(r)
		}
	}
	b.WriteByte('"')
	return b.String()
}

func needsQuoting(s string) bool {
	if s == "" {
		return true
	}
	for _, r := range s {
		if r <= ' ' || r == '=' || r == '"' || r == '\\' || textsafe.IsUnsafe(r) {
			return true
		}
	}
	return false
}

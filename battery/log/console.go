package log

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"strings"
	"sync"
	"time"

	"golang.org/x/term"
)

// ConsoleMode controls whether a human-readable colorized console sink
// is attached alongside the file/webhook sinks. It is the Config.Console
// field's type.
type ConsoleMode int

const (
	// ConsoleAuto is the zero value: the console sink is attached only
	// when stderr is a terminal and NO_COLOR is unset. This is the
	// "zero-config dev color, nothing in prod" behavior — Config{}
	// gives a local developer a colorized stderr feed without leaking
	// ANSI into journald / container logs where stderr is captured, not
	// shown.
	ConsoleAuto ConsoleMode = iota
	// ConsoleOn forces the console sink on regardless of TTY. Useful
	// under process supervisors that capture stderr as plain text: you
	// get human-readable lines instead of JSON without the dev running
	// in a real terminal. Coloring still follows TTY + NO_COLOR, so
	// piping the output drops ANSI and stays greppable.
	ConsoleOn
	// ConsoleOff disables the console sink entirely.
	ConsoleOff
)

// ConsoleOpts configures a console sink. The zero value is ready to use:
// writes to os.Stderr, color auto-detected from the writer being a TTY
// and NO_COLOR being unset.
type ConsoleOpts struct {
	// Writer is the destination. Nil defaults to os.Stderr.
	Writer io.Writer

	// Color forces color on (true) or off (false). When nil (default),
	// color is enabled only when Writer is a terminal and NO_COLOR is
	// unset — so piping to a file or through `cat` drops the ANSI codes
	// and the output stays greppable.
	Color *bool

	// TimeFormat overrides the timestamp format. Defaults to
	// "15:04:05.000" (HH:MM:SS.mmm). Set to "" for the same default.
	TimeFormat string
}

// ConsoleSink is a Sink that renders each JSON entry as a single
// human-readable, optionally colorized line:
//
//	14:32:07.412 INFO  app.start app="myapp" go="go1.24.1"
//
// It parses the JSON the fanout emits (see the Sink.Write contract) so
// it can apply level colors, bold the message, and dim the timestamp.
// Attr order is preserved via token decoding — operators see fields in
// the order the code emitted them, not json's randomized map order.
//
// If the entry is not a JSON object the raw bytes are written verbatim
// plus a newline, so a malformed entry is still visible rather than
// silently dropped.
//
// Concurrency: safe for concurrent use. Write serializes on a mutex so
// concurrent entries don't interleave on the terminal. After Close,
// Write returns ErrSinkClosed — consistent with the other sinks so the
// fanout's post-Stop drop counter advances correctly.
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

// ansi holds the ANSI escape sequences used by the renderer. When color
// is disabled every field is the empty string, so the same render path
// produces greppable plain text without branching.
type ansi struct {
	reset, dim, bold                     string
	gray, red, green, yellow, blue, cyan string
}

var ansiColor = ansi{
	reset: "\x1b[0m", dim: "\x1b[2m", bold: "\x1b[1m",
	gray: "\x1b[90m", red: "\x1b[31m", green: "\x1b[32m",
	yellow: "\x1b[33m", blue: "\x1b[34m", cyan: "\x1b[36m",
}

var ansiNone ansi // zero value → all-empty → plain text

// shouldColor reports whether w should receive ANSI color. Honors the
// NO_COLOR convention (https://no-color.org): a non-empty value
// disables color regardless of TTY. Otherwise color is enabled only
// when w is an *os.File backed by a terminal.
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
	// Malformed JSON: write raw + newline so the entry is at least
	// visible. Better to surface garbage than to silently drop it.
	_, err := s.w.Write(append(entry, '\n'))
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

// render formats one JSON entry as a (possibly colorized) line ending
// in '\n'. Returns ok=false if entry is not a JSON object.
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
			ts = strings.Trim(string(raw), `"`)
		case "level":
			level = strings.Trim(string(raw), `"`)
		case "msg":
			msg = strings.Trim(string(raw), `"`)
		default:
			rest = append(rest, kv{key: key, val: raw})
		}
	}

	var b strings.Builder
	// Timestamp: dimmed, reformatted to HH:MM:SS.mmm. Falls back to the
	// raw RFC3339 string if it doesn't parse.
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
	// Level: colored by severity, padded to a fixed 5-wide column so
	// the message column aligns across entries.
	b.WriteString(levelColor(s.a, level))
	b.WriteString(padLevel(level))
	b.WriteString(s.a.reset)
	b.WriteByte(' ')
	// Message: bold.
	if msg != "" {
		b.WriteString(s.a.bold)
		b.WriteString(msg)
		b.WriteString(s.a.reset)
	}
	// Attrs: key (gray) = value (cyan), in emission order.
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

// padLevel left-pads the level name to a 5-wide column so DEBUG/INFO/
// WARN/ERROR all align. Unknown levels are padded the same way.
func padLevel(level string) string {
	const width = 5
	if len(level) >= width {
		return level
	}
	return level + strings.Repeat(" ", width-len(level))
}

// formatValue renders a JSON RawMessage for the key=value display.
// Strings are shown bare when simple (no spaces, '=', quotes, control
// chars) and JSON-quoted otherwise; numbers/bools/null/objects/arrays
// are passed through as compact JSON. Multi-line values (e.g. panic
// stacks) keep their newlines escaped so each entry stays one line —
// the full unescaped value is always available in the file sink.
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
			out, _ := json.Marshal(s)
			return string(out)
		}
		return s
	}
	return string(raw)
}

func needsQuoting(s string) bool {
	if s == "" {
		return true
	}
	for _, r := range s {
		if r <= ' ' || r == '=' || r == '"' || r == '\\' || r == '\x1b' {
			return true
		}
	}
	return false
}

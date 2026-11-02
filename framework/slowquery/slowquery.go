package slowquery

import (
	"context"
	"database/sql"
	"log/slog"
	"strings"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"github.com/DonaldMurillo/gofastr/framework/db"
)

// SlowQueryLogger emits a slog warning for any DB query whose execution time
// exceeds Threshold. Wrap the framework's *sql.DB at construction time
// (or any db.Executor) and pass the result into NewCrudHandler in place of
// the raw DB.
//
// Threshold of 0 disables logging entirely (default zero-value).
//
// The wrapper is dialect-agnostic, it simply times QueryContext /
// QueryRowContext / ExecContext on the underlying connection and emits a
// structured log line when the duration crosses the threshold.
type SlowQueryLogger struct {
	inner     db.Executor
	threshold time.Duration
	getLogger func() *slog.Logger
	hits      atomic.Uint64
}

// NewSlowQueryLogger wraps inner with a slow-query observer. When threshold
// is zero, the wrapper is a no-op pass-through (the same db.Executor with no
// instrumentation cost).
//
// logger is a pinned *slog.Logger. Slow-query lines write through it for the
// lifetime of the wrapper. To follow App.Logger swaps (battery/log replaces
// it during plugin Init), use NewSlowQueryLoggerFn instead and pass
// app.Logger as the accessor.
func NewSlowQueryLogger(inner db.Executor, threshold time.Duration, logger *slog.Logger) *SlowQueryLogger {
	if logger == nil {
		return NewSlowQueryLoggerFn(inner, threshold, nil)
	}
	return &SlowQueryLogger{
		inner:     inner,
		threshold: threshold,
		getLogger: func() *slog.Logger { return logger },
	}
}

// NewSlowQueryLoggerFn is like NewSlowQueryLogger but takes a per-call
// accessor so log lines route through the current logger at observation
// time. Use with app.Logger so battery/log's SetLogger swap takes effect
// without re-wrapping the executor.
//
// If getLogger is nil or returns nil, slog.Default is used as a safety
// fallback (slow-query lines never disappear silently).
func NewSlowQueryLoggerFn(inner db.Executor, threshold time.Duration, getLogger func() *slog.Logger) *SlowQueryLogger {
	if getLogger == nil {
		getLogger = func() *slog.Logger { return slog.Default() }
	}
	return &SlowQueryLogger{inner: inner, threshold: threshold, getLogger: getLogger}
}

// Hits returns the running count of queries that crossed the threshold.
// Useful for assertions in tests and for /.debug/stats surface metrics.
func (s *SlowQueryLogger) Hits() uint64 { return s.hits.Load() }

// QueryContext mirrors sql.DB.QueryContext, timing the call.
func (s *SlowQueryLogger) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	t := time.Now()
	rows, err := s.inner.QueryContext(ctx, query, args...)
	s.observe(ctx, "query", query, args, elapsedSince(t), err)
	return rows, err
}

// QueryRowContext mirrors sql.DB.QueryRowContext, timing the call.
func (s *SlowQueryLogger) QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row {
	t := time.Now()
	row := s.inner.QueryRowContext(ctx, query, args...)
	s.observe(ctx, "query_row", query, args, elapsedSince(t), nil)
	return row
}

// ExecContext mirrors sql.DB.ExecContext, timing the call.
func (s *SlowQueryLogger) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	t := time.Now()
	res, err := s.inner.ExecContext(ctx, query, args...)
	s.observe(ctx, "exec", query, args, elapsedSince(t), err)
	return res, err
}

// elapsedSince clamps the platform timer's zero-duration result to one
// nanosecond. Very fast calls can occupy the same clock tick on Windows;
// reporting them as exactly zero makes a positive 1ns threshold flaky.
func elapsedSince(start time.Time) time.Duration {
	d := time.Since(start)
	if d <= 0 {
		return time.Nanosecond
	}
	return d
}

// BeginTx forwards transaction starts to the inner DB if it supports them.
// Tx-bound queries don't go through this wrapper (the framework hands the
// raw *sql.Tx to the in-tx handler copy), so multi-statement transactions
// aren't slow-logged here today. A future enhancement could wrap the tx too.
func (s *SlowQueryLogger) BeginTx(ctx context.Context, opts *sql.TxOptions) (*sql.Tx, error) {
	if b, ok := s.inner.(interface {
		BeginTx(ctx context.Context, opts *sql.TxOptions) (*sql.Tx, error)
	}); ok {
		return b.BeginTx(ctx, opts)
	}
	return nil, sql.ErrConnDone
}

func (s *SlowQueryLogger) observe(ctx context.Context, kind, query string, args []any, dur time.Duration, err error) {
	if s.threshold <= 0 || dur < s.threshold {
		return
	}
	s.hits.Add(1)
	attrs := []any{
		slog.String("kind", kind),
		slog.Duration("dur", dur),
		slog.String("sql", TrimSQL(query)),
		slog.Int("args", len(args)),
	}
	if err != nil {
		attrs = append(attrs, slog.String("err", err.Error()))
	}
	logger := s.getLogger()
	if logger == nil {
		logger = slog.Default()
	}
	logger.LogAttrs(ctx, slog.LevelWarn, "slow query", asSlogAttrs(attrs)...)
}

// maxLoggedSQL caps the printed statement, in bytes.
const maxLoggedSQL = 240

// TrimSQL masks the statement's literal values, collapses internal
// whitespace so log lines stay readable, and caps the printed query at
// 240 bytes (cut on a rune boundary, then "…") to keep logs from
// exploding on big builders.
//
// A string literal ('…', E'…', $tag$…$tag$) prints as '?' (E'?',
// $tag$?$tag$): a slow-query line is for the statement's shape, and a
// value spelled inline (ALTER ROLE … PASSWORD '…', SET app.key = '…') went
// to every sink verbatim. Placeholders, numbers, identifiers and quoted
// identifiers stay. Bound arguments were never logged, only their count.
func TrimSQL(q string) string {
	q = maskLiterals(q)
	out := make([]byte, 0, min(len(q), maxLoggedSQL+1))
	lastSpace := false
	for i := 0; i < len(q) && len(out) <= maxLoggedSQL; i++ {
		c := q[i]
		if c < 0x20 || c == 0x7f {
			c = ' '
		}
		if c == ' ' {
			if lastSpace {
				continue
			}
			lastSpace = true
		} else {
			lastSpace = false
		}
		out = append(out, c)
	}
	if len(out) <= maxLoggedSQL {
		return string(out)
	}
	// Over the cap: cut at maxLoggedSQL, backed off to the start of the
	// rune that straddles it, so the line stays valid UTF-8.
	cut := maxLoggedSQL
	for cut > 0 && !utf8.RuneStart(out[cut]) {
		cut--
	}
	return string(out[:cut]) + "…"
}

// maskLiterals replaces the body of every SQL string literal in q with
// "?": standard '…' strings (a doubled quote escapes one), E'…' strings
// (backslash escapes too) and Postgres dollar-quoted $tag$…$tag$ bodies.
// Quoted identifiers ("…") and comments are copied through so a quote
// inside one does not open a literal. An unterminated literal masks to
// the end of the statement.
func maskLiterals(q string) string {
	if !strings.ContainsAny(q, "'$") {
		return q
	}
	var b strings.Builder
	b.Grow(len(q))
	for i := 0; i < len(q); {
		c := q[i]
		switch {
		case c == '-' && i+1 < len(q) && q[i+1] == '-':
			end := strings.IndexByte(q[i:], '\n')
			if end < 0 {
				end = len(q) - i
			}
			b.WriteString(q[i : i+end])
			i += end
		case c == '/' && i+1 < len(q) && q[i+1] == '*':
			end := strings.Index(q[i+2:], "*/")
			if end < 0 {
				b.WriteString(q[i:])
				return b.String()
			}
			b.WriteString(q[i : i+2+end+2])
			i += 2 + end + 2
		case c == '"':
			j := i + 1
			for j < len(q) {
				if q[j] == '"' {
					if j+1 < len(q) && q[j+1] == '"' {
						j += 2
						continue
					}
					j++
					break
				}
				j++
			}
			b.WriteString(q[i:j])
			i = j
		case c == '\'':
			escapes := i > 0 && (q[i-1] == 'E' || q[i-1] == 'e') && (i < 2 || !isIdentByte(q[i-2]))
			j := i + 1
			closed := false
			for j < len(q) {
				if escapes && q[j] == '\\' {
					j += 2
					continue
				}
				if q[j] == '\'' {
					if j+1 < len(q) && q[j+1] == '\'' {
						j += 2
						continue
					}
					closed = true
					j++
					break
				}
				j++
			}
			if !closed {
				b.WriteString("'?")
				return b.String()
			}
			b.WriteString("'?'")
			i = j
		case c == '$' && (i == 0 || !isIdentByte(q[i-1])):
			tag, ok := dollarTag(q[i:])
			if !ok {
				b.WriteByte(c)
				i++
				continue
			}
			body := q[i+len(tag):]
			end := strings.Index(body, tag)
			b.WriteString(tag)
			b.WriteByte('?')
			if end < 0 {
				return b.String()
			}
			b.WriteString(tag)
			i += len(tag) + end + len(tag)
		default:
			b.WriteByte(c)
			i++
		}
	}
	return b.String()
}

// dollarTag returns the opening "$tag$" (or "$$") at the start of s. A
// "$" followed by a digit is a placeholder ($1), not a dollar quote.
func dollarTag(s string) (string, bool) {
	if len(s) < 2 {
		return "", false
	}
	if s[1] == '$' {
		return "$$", true
	}
	if !isIdentStart(s[1]) {
		return "", false
	}
	for j := 2; j < len(s); j++ {
		switch {
		case s[j] == '$':
			return s[:j+1], true
		case !isIdentByte(s[j]):
			return "", false
		}
	}
	return "", false
}

func isIdentStart(c byte) bool {
	return c == '_' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || c >= 0x80
}

func isIdentByte(c byte) bool {
	return isIdentStart(c) || (c >= '0' && c <= '9') || c == '$'
}

// asSlogAttrs converts our []any (alternating slog.Attr) into the expected
// slice shape. Lets us build attrs incrementally without slog.Group noise.
func asSlogAttrs(a []any) []slog.Attr {
	out := make([]slog.Attr, 0, len(a))
	for _, v := range a {
		if attr, ok := v.(slog.Attr); ok {
			out = append(out, attr)
		}
	}
	return out
}

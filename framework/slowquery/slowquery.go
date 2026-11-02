package slowquery

import (
	"context"
	"database/sql"
	"log/slog"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"github.com/DonaldMurillo/gofastr/framework/db"
	"github.com/DonaldMurillo/gofastr/internal/sqlredact"
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
	dialect   sqlredact.Dialect
}

// Option configures a SlowQueryLogger.
type Option func(*SlowQueryLogger)

// WithDialect names the database engine, so the sql attr masks its
// literals by that engine's quoting rules: "postgres" (or "postgresql",
// "pgx"), "sqlite3" (or "sqlite"), "mysql" (or "mariadb"). The values
// of framework.DetectDialect are accepted as they are. Without it, or
// with a name it does not know, the line masks under every engine's
// rules at once (see TrimSQL), which also hides PostgreSQL's
// "quoted identifiers".
func WithDialect(name string) Option {
	return func(s *SlowQueryLogger) { s.dialect = sqlredact.ForSystem(name) }
}

// NewSlowQueryLogger wraps inner with a slow-query observer. When threshold
// is zero, the wrapper is a no-op pass-through (the same db.Executor with no
// instrumentation cost).
//
// logger is a pinned *slog.Logger. Slow-query lines write through it for the
// lifetime of the wrapper. To follow App.Logger swaps (battery/log replaces
// it during plugin Init), use NewSlowQueryLoggerFn instead and pass
// app.Logger as the accessor.
func NewSlowQueryLogger(inner db.Executor, threshold time.Duration, logger *slog.Logger, opts ...Option) *SlowQueryLogger {
	if logger == nil {
		return NewSlowQueryLoggerFn(inner, threshold, nil, opts...)
	}
	return NewSlowQueryLoggerFn(inner, threshold, func() *slog.Logger { return logger }, opts...)
}

// NewSlowQueryLoggerFn is like NewSlowQueryLogger but takes a per-call
// accessor so log lines route through the current logger at observation
// time. Use with app.Logger so battery/log's SetLogger swap takes effect
// without re-wrapping the executor.
//
// If getLogger is nil or returns nil, slog.Default is used as a safety
// fallback (slow-query lines never disappear silently).
func NewSlowQueryLoggerFn(inner db.Executor, threshold time.Duration, getLogger func() *slog.Logger, opts ...Option) *SlowQueryLogger {
	if getLogger == nil {
		getLogger = func() *slog.Logger { return slog.Default() }
	}
	s := &SlowQueryLogger{inner: inner, threshold: threshold, getLogger: getLogger}
	for _, o := range opts {
		if o != nil {
			o(s)
		}
	}
	return s
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
		slog.String("sql", trimSQL(query, s.dialect)),
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

// TrimSQL is the statement's shape, as the slow-query line prints it
// when no dialect was named: every string and number literal replaced by
// ?, comments dropped, whitespace collapsed, control bytes escaped, and
// the result capped at 240 bytes (cut on a rune boundary, then "…") to
// keep logs from exploding on big builders. Placeholders ($1, ?, :n)
// and identifiers stay. Bound arguments were never logged, only their
// count.
//
// A slow-query line is for the statement's shape, and a value spelled
// inline (ALTER ROLE … PASSWORD '…', SET app.key = '…') went to every
// sink verbatim. Where a literal ends depends on the engine, and TrimSQL
// does not know it, so it masks under every engine's rules at once:
// "double-quoted" text prints as ? too (SQLite and MySQL can read it as
// a string), # opens a comment (MySQL), block comments nest
// (PostgreSQL), and a string whose end depends on whether a backslash
// escapes masks the rest of the statement. Name the engine with
// WithDialect to keep quoted identifiers on PostgreSQL. It is the masker
// core/tracing puts on db.query.text.
func TrimSQL(q string) string {
	return trimSQL(q, sqlredact.Unknown)
}

func trimSQL(q string, d sqlredact.Dialect) string {
	out := sqlredact.Text(q, d)
	if len(out) <= maxLoggedSQL {
		return out
	}
	// Over the cap: cut at maxLoggedSQL, backed off to the start of the
	// rune that straddles it, so the line stays valid UTF-8.
	cut := maxLoggedSQL
	for cut > 0 && !utf8.RuneStart(out[cut]) {
		cut--
	}
	return out[:cut] + "…"
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

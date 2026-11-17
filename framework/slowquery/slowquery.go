// Package slowquery logs and counts database statements slower than a threshold: NewSlowQueryLogger, MetricsCollector.
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

// SlowQueryLogger is a db.Executor that logs a warning for each statement at or over its threshold; zero disables it.
type SlowQueryLogger struct {
	inner     db.Executor
	threshold time.Duration
	getLogger func() *slog.Logger
	hits      atomic.Uint64
	dialect   sqlredact.Dialect
}

// Option configures a SlowQueryLogger.
type Option func(*SlowQueryLogger)

// WithDialect masks literals by the named engine's quoting (a framework.DetectDialect value); otherwise every engine's rules apply.
func WithDialect(name string) Option {
	return func(s *SlowQueryLogger) { s.dialect = sqlredact.ForSystem(name) }
}

// NewSlowQueryLogger wraps inner, writing through logger (slog.Default when nil).
func NewSlowQueryLogger(inner db.Executor, threshold time.Duration, logger *slog.Logger, opts ...Option) *SlowQueryLogger {
	if logger == nil {
		return NewSlowQueryLoggerFn(inner, threshold, nil, opts...)
	}
	return NewSlowQueryLoggerFn(inner, threshold, func() *slog.Logger { return logger }, opts...)
}

// NewSlowQueryLoggerFn is NewSlowQueryLogger reading the logger from getLogger per line, so an App.Logger swap applies.
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

// Hits returns how many statements crossed the threshold.
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

func elapsedSince(start time.Time) time.Duration {
	d := time.Since(start)
	if d <= 0 {
		return time.Nanosecond
	}
	return d
}

// BeginTx forwards to the inner executor, or returns sql.ErrConnDone when it cannot begin; statements on the tx are not timed.
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

const maxLoggedSQL = 240

// TrimSQL returns a statement's shape under every engine's quoting: literals as ?, comments dropped, whitespace collapsed, at most 240 bytes.
func TrimSQL(q string) string {
	return trimSQL(q, sqlredact.Unknown)
}

func trimSQL(q string, d sqlredact.Dialect) string {
	out := sqlredact.Text(q, d)
	if len(out) <= maxLoggedSQL {
		return out
	}
	cut := maxLoggedSQL
	for cut > 0 && !utf8.RuneStart(out[cut]) {
		cut--
	}
	return out[:cut] + "…"
}

func asSlogAttrs(a []any) []slog.Attr {
	out := make([]slog.Attr, 0, len(a))
	for _, v := range a {
		if attr, ok := v.(slog.Attr); ok {
			out = append(out, attr)
		}
	}
	return out
}

package slowquery

import (
	"context"
	"database/sql"
	"log/slog"
	"sync/atomic"
	"time"

	"github.com/gofastr/gofastr/framework/db"
)

// SlowQueryLogger emits a slog warning for any DB query whose execution time
// exceeds Threshold. Wrap the framework's *sql.DB at construction time
// (or any db.Executor) and pass the result into NewCrudHandler in place of
// the raw DB.
//
// Threshold of 0 disables logging entirely (default zero-value).
//
// The wrapper is dialect-agnostic — it simply times QueryContext /
// QueryRowContext / ExecContext on the underlying connection and emits a
// structured log line when the duration crosses the threshold.
type SlowQueryLogger struct {
	inner     db.Executor
	threshold time.Duration
	logger    *slog.Logger
	hits      atomic.Uint64
}

// NewSlowQueryLogger wraps inner with a slow-query observer. When threshold
// is zero, the wrapper is a no-op pass-through (the same db.Executor with no
// instrumentation cost).
func NewSlowQueryLogger(inner db.Executor, threshold time.Duration, logger *slog.Logger) *SlowQueryLogger {
	if logger == nil {
		logger = slog.Default()
	}
	return &SlowQueryLogger{inner: inner, threshold: threshold, logger: logger}
}

// Hits returns the running count of queries that crossed the threshold.
// Useful for assertions in tests and for /.debug/stats surface metrics.
func (s *SlowQueryLogger) Hits() uint64 { return s.hits.Load() }

// QueryContext mirrors sql.DB.QueryContext, timing the call.
func (s *SlowQueryLogger) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	t := time.Now()
	rows, err := s.inner.QueryContext(ctx, query, args...)
	s.observe(ctx, "query", query, args, time.Since(t), err)
	return rows, err
}

// QueryRowContext mirrors sql.DB.QueryRowContext, timing the call.
func (s *SlowQueryLogger) QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row {
	t := time.Now()
	row := s.inner.QueryRowContext(ctx, query, args...)
	s.observe(ctx, "query_row", query, args, time.Since(t), nil)
	return row
}

// ExecContext mirrors sql.DB.ExecContext, timing the call.
func (s *SlowQueryLogger) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	t := time.Now()
	res, err := s.inner.ExecContext(ctx, query, args...)
	s.observe(ctx, "exec", query, args, time.Since(t), err)
	return res, err
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
	s.logger.LogAttrs(ctx, slog.LevelWarn, "slow query", asSlogAttrs(attrs)...)
}

// TrimSQL collapses internal whitespace so log lines stay readable. Caps the
// printed query at 240 chars to keep logs from exploding on big builders.
func TrimSQL(q string) string {
	out := make([]byte, 0, len(q))
	lastSpace := false
	for i := 0; i < len(q); i++ {
		c := q[i]
		if c == '\n' || c == '\t' || c == '\r' {
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
		if len(out) == 240 {
			out = append(out, "…"...)
			break
		}
	}
	return string(out)
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

package auth

import (
	"context"
	"database/sql"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/DonaldMurillo/gofastr/core/query"
	"github.com/DonaldMurillo/gofastr/framework/migrate"
	"github.com/DonaldMurillo/gofastr/framework/ratelimit"
)

// RateLimitStore is a limiter backend shared by replicas, so a budget and a block hold across them.
type RateLimitStore interface {
	Allow(ctx context.Context, key string, cfg RateLimiterConfig) (allowed bool, retryAfter time.Duration, err error)
}

// SQLRateLimitStore is a RateLimitStore in two SQLite or PostgreSQL tables: attempts and blocks.
type SQLRateLimitStore struct {
	db      *sql.DB
	table   string
	dialect storeDialect

	schemaReady atomic.Bool // lock-free fast path once the schema exists
	schemaMu    sync.Mutex  // serializes first-time schema creation

	mu        sync.Mutex           // guards lastSweep
	lastSweep map[string]time.Time // per-scope, so one busy scope can't starve others' GC
}

// NewSQLRateLimitStore returns a store on table and table_attempts; it panics on an unsafe name.
func NewSQLRateLimitStore(db *sql.DB, table string) *SQLRateLimitStore {
	query.MustIdent(table)
	query.MustIdent(table + "_attempts")
	return &SQLRateLimitStore{db: db, table: table, lastSweep: map[string]time.Time{}}
}

// EnsureSchema creates both tables; Allow calls it until it first succeeds.
func (s *SQLRateLimitStore) EnsureSchema(ctx context.Context) error {
	for _, table := range []string{s.table, s.table + "_attempts"} {
		if err := refuseCaseKeptTable(ctx, s.db, s.dialect.of(s.db), table); err != nil {
			return err
		}
	}
	stmts := []string{
		fmt.Sprintf("CREATE TABLE IF NOT EXISTS %s (rl_key TEXT PRIMARY KEY NOT NULL, blocked_until_ms BIGINT NOT NULL)",
			query.QuoteFolded(s.table)),
		fmt.Sprintf("CREATE TABLE IF NOT EXISTS %s (rl_key TEXT NOT NULL, attempted_at_ms BIGINT NOT NULL)",
			query.QuoteFolded(s.table+"_attempts")),
		fmt.Sprintf("CREATE INDEX IF NOT EXISTS %s (rl_key, attempted_at_ms)",
			query.IndexOn(s.dialect.of(s.db) == migrate.DialectSQLite,
				query.QuoteIndexName("idx_"+s.table+"_attempts_key_time"), query.QuoteFolded(s.table+"_attempts"))),
	}
	for _, stmt := range stmts {
		if _, err := s.db.ExecContext(ctx, stmt); err != nil {
			return err
		}
	}
	return nil
}

func (s *SQLRateLimitStore) ensureSchemaOnce(ctx context.Context) error {
	if s.schemaReady.Load() {
		return nil
	}
	s.schemaMu.Lock()
	defer s.schemaMu.Unlock()
	if s.schemaReady.Load() {
		return nil
	}
	if err := s.EnsureSchema(ctx); err != nil {
		return err
	}
	s.schemaReady.Store(true)
	return nil
}

// Allow records the attempt, then counts the key's in-window attempts, with the in-process limiter's window and block rules; a store error refuses.
func (s *SQLRateLimitStore) Allow(ctx context.Context, key string, cfg RateLimiterConfig) (bool, time.Duration, error) {
	if err := s.ensureSchemaOnce(ctx); err != nil {
		return false, 0, err
	}

	now := time.Now()
	nowMs := now.UnixMilli()
	cutoffMs := now.Add(-cfg.Window).UnixMilli()
	blocks := query.QuoteFolded(s.table)
	attempts := query.QuoteFolded(s.table + "_attempts")

	s.mu.Lock()
	sweep := now.Sub(s.lastSweep[cfg.Scope]) >= cfg.Window
	if sweep {
		s.lastSweep[cfg.Scope] = now
	}
	s.mu.Unlock()
	if sweep {
		scopePrefix := ratelimit.NamespaceScope(cfg.Scope) + "|"
		const inScope = "substr(a.rl_key, 1, length(CAST($1 AS TEXT))) = $1"
		if _, err := s.db.ExecContext(ctx, fmt.Sprintf(`DELETE FROM %s AS a WHERE %s AND attempted_at_ms <= $2`, attempts, inScope), scopePrefix, cutoffMs); err != nil {
			return false, 0, err
		}
		if _, err := s.db.ExecContext(ctx, fmt.Sprintf(`DELETE FROM %s AS a WHERE %s AND EXISTS (SELECT 1 FROM %s b WHERE b.rl_key = a.rl_key AND b.blocked_until_ms <= $2 AND a.attempted_at_ms < b.blocked_until_ms)`, attempts, inScope, blocks), scopePrefix, nowMs); err != nil {
			return false, 0, err
		}
		if _, err := s.db.ExecContext(ctx, fmt.Sprintf(`DELETE FROM %s AS a WHERE %s AND blocked_until_ms <= $2`, blocks, inScope), scopePrefix, nowMs); err != nil {
			return false, 0, err
		}
	}

	var blockedUntilMs int64
	err := s.db.QueryRowContext(ctx, fmt.Sprintf("SELECT blocked_until_ms FROM %s WHERE rl_key = $1", blocks), key).Scan(&blockedUntilMs)
	switch {
	case err == sql.ErrNoRows:
	case err != nil:
		return false, 0, err
	case nowMs < blockedUntilMs:
		return false, time.Duration(blockedUntilMs-nowMs) * time.Millisecond, nil
	default:
		if _, err := s.db.ExecContext(ctx, fmt.Sprintf("DELETE FROM %s WHERE rl_key = $1 AND blocked_until_ms = $2", blocks), key, blockedUntilMs); err != nil {
			return false, 0, err
		}
		if _, err := s.db.ExecContext(ctx, fmt.Sprintf("DELETE FROM %s WHERE rl_key = $1 AND attempted_at_ms < $2", attempts), key, blockedUntilMs); err != nil {
			return false, 0, err
		}
	}

	if _, err := s.db.ExecContext(ctx, fmt.Sprintf("DELETE FROM %s WHERE rl_key = $1 AND attempted_at_ms <= $2", attempts), key, cutoffMs); err != nil {
		return false, 0, err
	}

	if _, err := s.db.ExecContext(ctx, fmt.Sprintf("INSERT INTO %s (rl_key, attempted_at_ms) VALUES ($1, $2)", attempts), key, nowMs); err != nil {
		return false, 0, err
	}
	var n int
	if err := s.db.QueryRowContext(ctx, fmt.Sprintf("SELECT COUNT(*) FROM %s WHERE rl_key = $1 AND attempted_at_ms > $2", attempts), key, cutoffMs).Scan(&n); err != nil {
		return false, 0, err
	}
	if n > cfg.MaxAttempts {
		blockedMs := now.Add(cfg.BlockDuration).UnixMilli()
		upsert := fmt.Sprintf("INSERT INTO %s (rl_key, blocked_until_ms) VALUES ($1, $2) ON CONFLICT (rl_key) DO UPDATE SET blocked_until_ms = excluded.blocked_until_ms", blocks)
		if _, err := s.db.ExecContext(ctx, upsert, key, blockedMs); err != nil {
			return false, 0, err
		}
		return false, cfg.BlockDuration, nil
	}
	return true, 0, nil
}

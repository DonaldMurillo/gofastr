package auth

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/DonaldMurillo/gofastr/core/query"
	"github.com/DonaldMurillo/gofastr/framework/ratelimit"
)

// escapeLikeLiteral escapes the LIKE wildcards (%, _) and the escape char
// itself so a string can be used as a literal prefix in a `LIKE ... ESCAPE
// '\'` clause. Scope names contain '_' (e.g. login_ip), which would
// otherwise match any character and over-delete across scopes.
func escapeLikeLiteral(s string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(s)
}

// RateLimitStore is the optional shared backend for RateLimiter. When
// RateLimiterConfig.Store is set, every replica consults the same
// attempt ledger, so the brute-force budget stays MaxAttempts total
// instead of MaxAttempts × replicas and a block on one replica holds on
// all of them.
//
// The store receives the limiter's resolved config on every call so one
// store instance can serve limiters with different budgets (login,
// register, 2FA challenge), implementations must derive per-limiter
// state from the key alone, which the RateLimiter already namespaces.
type RateLimitStore interface {
	Allow(ctx context.Context, key string, cfg RateLimiterConfig) (allowed bool, retryAfter time.Duration, err error)
}

// SQLRateLimitStore is a RateLimitStore backed by two database tables
// (SQLite or PostgreSQL): one row per attempt plus a per-key block row.
// Timestamps are stored as unix milliseconds so window comparisons are
// plain integer arithmetic on both dialects.
//
// Usage:
//
//	shared := auth.NewSQLRateLimitStore(db, "auth_rate_limits")
//	mgr := auth.New(auth.AuthConfig{
//	    LoginRateLimit: &auth.RateLimiterConfig{Store: shared},
//	    ...
//	})
//
// The schema is created lazily on first use, hosts never hand-roll the
// DDL. Admission holds exactly under concurrency, across any number of
// replicas: Allow records the attempt before it counts (see Allow), so a
// burst on one key admits at most MaxAttempts callers.
type SQLRateLimitStore struct {
	db    *sql.DB
	table string

	schemaReady atomic.Bool // lock-free fast path once the schema exists
	schemaMu    sync.Mutex  // serializes first-time schema creation

	mu        sync.Mutex           // guards lastSweep
	lastSweep map[string]time.Time // per-scope, so one busy scope can't starve others' GC
}

// NewSQLRateLimitStore creates a shared rate-limit store on the given
// table base name (an "_attempts" sibling table is derived from it).
// Panics if the table name contains unsafe characters.
func NewSQLRateLimitStore(db *sql.DB, table string) *SQLRateLimitStore {
	query.MustIdent(table)
	query.MustIdent(table + "_attempts")
	return &SQLRateLimitStore{db: db, table: table, lastSweep: map[string]time.Time{}}
}

// EnsureSchema creates both tables if absent. Idempotent; invoked
// lazily by Allow, exported for hosts that migrate eagerly at boot.
func (s *SQLRateLimitStore) EnsureSchema(ctx context.Context) error {
	stmts := []string{
		fmt.Sprintf("CREATE TABLE IF NOT EXISTS %s (rl_key TEXT PRIMARY KEY, blocked_until_ms BIGINT NOT NULL)",
			query.QuoteIdent(s.table)),
		fmt.Sprintf("CREATE TABLE IF NOT EXISTS %s (rl_key TEXT NOT NULL, attempted_at_ms BIGINT NOT NULL)",
			query.QuoteIdent(s.table+"_attempts")),
		fmt.Sprintf("CREATE INDEX IF NOT EXISTS %s ON %s (rl_key, attempted_at_ms)",
			query.QuoteIdent("idx_"+s.table+"_attempts_key_time"), query.QuoteIdent(s.table+"_attempts")),
	}
	for _, stmt := range stmts {
		if _, err := s.db.ExecContext(ctx, stmt); err != nil {
			return err
		}
	}
	return nil
}

// ensureSchemaOnce runs EnsureSchema until it first succeeds, then never
// again. Unlike a sync.Once that latches the *result*, a transient
// failure (DB unreachable during boot warm-up, context deadline) is
// retried on the next Allow, so a single blip can't permanently brick
// this replica's limiter into denying every login until a restart.
// CREATE TABLE IF NOT EXISTS is idempotent, so re-running is cheap.
func (s *SQLRateLimitStore) ensureSchemaOnce(ctx context.Context) error {
	if s.schemaReady.Load() {
		return nil
	}
	// Serialize first-time creation: CREATE TABLE/INDEX IF NOT EXISTS is not
	// race-free on Postgres (two concurrent runs can both pass the existence
	// check and one errors), which would spuriously fail-close a login. Only
	// one goroutine runs EnsureSchema; the rest wait and see schemaReady.
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

// Allow implements RateLimitStore with the same sliding-window +
// block semantics as the in-process limiter.
//
// The admission decision is insert-then-count. Each caller commits its
// own attempt row first and only then counts the key's in-window rows.
// Every statement is autocommit, so a caller's COUNT runs after its own
// INSERT committed: order the committed inserts, and the k-th one sees at
// least k rows. At most MaxAttempts callers can therefore see a count of
// MaxAttempts or less, however many arrive at once and on however many
// replicas. A caller that sees more is denied and writes the block.
//
// The earlier count-then-insert read the quota and acted on it in
// separate statements, so a concurrent burst all read the same
// pre-insert count and all got in (19 to 34 admissions out of 50 against
// MaxAttempts=10 on Postgres). Insert-then-count needs no transaction,
// advisory lock or dialect-specific UPSERT, so the same SQL holds on
// SQLite and Postgres. A denied caller's row stays behind; it predates
// the block it triggered and is cleared when that block expires.
//
// The bound is one-sided. When every insert of an over-budget burst
// commits before any of its counts run, every caller sees the whole
// burst and none is admitted. That only happens to a key that is past
// its budget and about to be blocked anyway, and it errs toward denial.
func (s *SQLRateLimitStore) Allow(ctx context.Context, key string, cfg RateLimiterConfig) (bool, time.Duration, error) {
	if err := s.ensureSchemaOnce(ctx); err != nil {
		return false, 0, err
	}

	now := time.Now()
	nowMs := now.UnixMilli()
	cutoffMs := now.Add(-cfg.Window).UnixMilli()
	blocks := query.QuoteIdent(s.table)
	attempts := query.QuoteIdent(s.table + "_attempts")

	// Amortized sweep so abandoned keys (attacker-minted emails, rotated
	// XFF) don't accumulate forever. At most once per Window PER SCOPE, and
	// scoped to THIS limiter's keys: the _attempts table is shared across
	// scopes with independent Windows, so (a) a short-window co-tenant (e.g.
	// a 2FA challenge limiter) must not delete a long-window limiter's (e.g.
	// 1h login-account) still-valid rows, hence the scope prefix, and (b)
	// each scope owns its own sweep timer, so a busy scope can't monopolize
	// a single shared timer and starve every other scope's GC.
	s.mu.Lock()
	sweep := now.Sub(s.lastSweep[cfg.Scope]) >= cfg.Window
	if sweep {
		s.lastSweep[cfg.Scope] = now
	}
	s.mu.Unlock()
	if sweep {
		// Escape LIKE metacharacters in the (host-settable) scope so a scope
		// containing '_' or '%' can't over-match into a sibling scope's keys.
		scopePrefix := escapeLikeLiteral(ratelimit.NamespaceScope(cfg.Scope)) + "|%"
		if _, err := s.db.ExecContext(ctx, fmt.Sprintf(`DELETE FROM %s WHERE rl_key LIKE $1 ESCAPE '\' AND attempted_at_ms <= $2`, attempts), scopePrefix, cutoffMs); err != nil {
			return false, 0, err
		}
		if _, err := s.db.ExecContext(ctx, fmt.Sprintf(`DELETE FROM %s WHERE rl_key LIKE $1 ESCAPE '\' AND blocked_until_ms <= $2`, blocks), scopePrefix, nowMs); err != nil {
			return false, 0, err
		}
	}

	// Honour an active block; clear an expired one.
	var blockedUntilMs int64
	err := s.db.QueryRowContext(ctx, fmt.Sprintf("SELECT blocked_until_ms FROM %s WHERE rl_key = $1", blocks), key).Scan(&blockedUntilMs)
	switch {
	case err == sql.ErrNoRows:
		// No block on record.
	case err != nil:
		return false, 0, err
	case nowMs < blockedUntilMs:
		return false, time.Duration(blockedUntilMs-nowMs) * time.Millisecond, nil
	default:
		// Clear only the block that was read and the attempts it covered.
		// A concurrent caller may already have written a fresh block or
		// recorded a post-expiry attempt; deleting by key alone would
		// erase those and hand the burst a second budget.
		if _, err := s.db.ExecContext(ctx, fmt.Sprintf("DELETE FROM %s WHERE rl_key = $1 AND blocked_until_ms = $2", blocks), key, blockedUntilMs); err != nil {
			return false, 0, err
		}
		if _, err := s.db.ExecContext(ctx, fmt.Sprintf("DELETE FROM %s WHERE rl_key = $1 AND attempted_at_ms < $2", attempts), key, blockedUntilMs); err != nil {
			return false, 0, err
		}
	}

	// Prune this key's out-of-window attempts.
	if _, err := s.db.ExecContext(ctx, fmt.Sprintf("DELETE FROM %s WHERE rl_key = $1 AND attempted_at_ms <= $2", attempts), key, cutoffMs); err != nil {
		return false, 0, err
	}

	// Record first, count second. The doc comment explains why this order
	// is what makes the decision atomic per key.
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

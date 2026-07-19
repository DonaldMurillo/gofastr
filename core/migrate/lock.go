package migrate

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// lockPollInterval is how often WithAdvisoryLock retries pg_try_advisory_lock
// while another holder has the lock. Kept small so a waiting replica proceeds
// promptly once the holder releases, but not a tight busy-loop.
var lockPollInterval = 200 * time.Millisecond

// AdvisoryLockKey is the fixed 64-bit key used for the migration advisory
// lock on Postgres. It MUST stay stable across releases: during a rolling
// deploy an old and a new instance both try to migrate, and they only
// mutually exclude if they agree on this key. Changing it would let two
// instances run DDL concurrently — the exact race the lock exists to prevent.
//
// The value is arbitrary but fixed (derived once from "gofastr.migrate" and
// frozen). Hosts that run unrelated migration tooling against the same
// database can override it via WithAdvisoryLockKey to avoid cross-tool
// contention.
const AdvisoryLockKey int64 = 6724469554113028193

// SeedAdvisoryLockKey is the fixed 64-bit key used for the SEED advisory
// lock on Postgres — DISTINCT from AdvisoryLockKey so that two replicas
// booting simultaneously serialize schema migration and seeding
// independently. Without a separate key, a replica that finished its DDL
// but still holds the migration lock to run its seeds would block every
// other replica's DDL phase (and vice versa). Same stability rule as
// AdvisoryLockKey: derived once from "gofastr.seed" and frozen — changing
// it would let two replicas run a Seed func concurrently.
//
// Combined with the _gofastr_seeded ledger (framework/migrate/seed.go),
// the lock turns RunSeeds into run-ONCE-globally: whichever replica wins
// the lock runs the Seed body and records the ledger row; the others wait
// for the lock, then short-circuit on the ledger. A crashed lock holder's
// session-level lock is released automatically by Postgres when the
// connection closes — no permanent block.
const SeedAdvisoryLockKey int64 = 7583194026157293042

// WithAdvisoryLock runs fn while holding a database-level lock that serializes
// migration across every process pointed at the same database. fn receives the
// pinned *sql.Conn that holds the lock and MUST do all of its work on that
// connection — running the migration on the same session as the lock is what
// keeps the whole thing correct on a single-connection pool (MaxOpenConns(1)),
// which a separate lock connection would deadlock.
//
//   - Postgres: a session-level advisory lock on the pinned connection,
//     acquired via pg_try_advisory_lock in a ctx-aware poll loop. A second
//     instance waits until the first releases, or returns promptly if its ctx
//     is cancelled. (A poll loop is used rather than the blocking
//     pg_advisory_lock because lib/pq does not interrupt a blocked
//     pg_advisory_lock on context cancellation — a stuck holder would
//     otherwise hang boot forever.) This is the guard that makes
//     auto-migrate-on-boot safe across N replicas.
//   - SQLite: no lock is taken (SQLite serializes writers at the file level),
//     but fn still gets a pinned connection so callers have one uniform code
//     path. The PK upgrade (rebuildTableSQLite) is NOT idempotent against a
//     table that already has group_name values — that is why only Up/Down/
//     Force call ensureCompositeKey, never Status (which is unlocked).
//
// db == nil runs fn(nil) — callers already treat a nil db as a no-op.
func WithAdvisoryLock(ctx context.Context, db *sql.DB, dialect Dialect, fn func(conn *sql.Conn) error) error {
	return WithAdvisoryLockKey(ctx, db, dialect, AdvisoryLockKey, fn)
}

// WithAdvisoryLockKey is WithAdvisoryLock with an explicit lock key, for hosts
// that need to namespace the lock away from other migration tooling sharing
// the database.
func WithAdvisoryLockKey(ctx context.Context, db *sql.DB, dialect Dialect, key int64, fn func(conn *sql.Conn) error) error {
	if db == nil {
		return fn(nil)
	}

	// Pin a single connection for the whole lock lifetime. The lock (Postgres)
	// and the migration work both run on this one session, so the lock is held
	// for the exact duration of the work and unlock targets the same backend.
	conn, err := db.Conn(ctx)
	if err != nil {
		return fmt.Errorf("migrate lock: acquire connection: %w", err)
	}
	defer conn.Close()

	if dialect != DialectPostgres {
		return fn(conn)
	}

	// Poll pg_try_advisory_lock (non-blocking) until we win the lock or ctx is
	// cancelled. Each individual query is short, so ctx cancellation is honored
	// between tries — unlike a single blocking pg_advisory_lock.
	for {
		var got bool
		if err := conn.QueryRowContext(ctx, "SELECT pg_try_advisory_lock($1)", key).Scan(&got); err != nil {
			return fmt.Errorf("migrate lock: pg_try_advisory_lock: %w", err)
		}
		if got {
			break
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("migrate lock: waiting for advisory lock: %w", ctx.Err())
		case <-time.After(lockPollInterval):
		}
	}
	// Release on the same connection. Use a background context so a cancelled
	// ctx (the common "shutdown mid-migration" case) still unlocks rather than
	// leaving the lock dangling until the session is reaped.
	defer func() {
		_, _ = conn.ExecContext(context.Background(), "SELECT pg_advisory_unlock($1)", key)
	}()

	return fn(conn)
}

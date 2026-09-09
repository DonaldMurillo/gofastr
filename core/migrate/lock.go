package migrate

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/DonaldMurillo/gofastr/core/query"
)

// lockPollInterval is how often WithAdvisoryLock retries pg_try_advisory_lock
// while another holder has the lock. Kept small so a waiting replica proceeds
// promptly once the holder releases, but not a tight busy-loop.
var lockPollInterval = 200 * time.Millisecond

// AdvisoryLockKey is the fixed 64-bit key used for the migration advisory
// lock on Postgres. It MUST stay stable across releases: during a rolling
// deploy an old and a new instance both try to migrate, and they only
// mutually exclude if they agree on this key. Changing it would let two
// instances run DDL concurrently, the exact race the lock exists to prevent.
//
// The value is arbitrary but fixed (derived once from "gofastr.migrate" and
// frozen). Hosts that run unrelated migration tooling against the same
// database can override it via WithAdvisoryLockKey to avoid cross-tool
// contention.
const AdvisoryLockKey int64 = 6724469554113028193

// SeedAdvisoryLockKey is the fixed 64-bit key used for the SEED advisory
// lock on Postgres, DISTINCT from AdvisoryLockKey so that two replicas
// booting simultaneously serialize schema migration and seeding
// independently. Without a separate key, a replica that finished its DDL
// but still holds the migration lock to run its seeds would block every
// other replica's DDL phase (and vice versa). Same stability rule as
// AdvisoryLockKey: derived once from "gofastr.seed" and frozen. Changing
// it would let two replicas run a Seed func concurrently.
//
// Combined with the _gofastr_seeded ledger (framework/migrate/seed.go),
// the lock turns RunSeeds into run-ONCE-globally: whichever replica wins
// the lock runs the Seed body and records the ledger row; the others wait
// for the lock, then short-circuit on the ledger. A crashed lock holder's
// session-level lock is released automatically by Postgres when the
// connection closes. No permanent block.
const SeedAdvisoryLockKey int64 = 7583194026157293042

// WithAdvisoryLock runs fn while holding a database-level lock that serializes
// migration across every process pointed at the same database. fn receives the
// pinned *sql.Conn that holds the lock and MUST do all of its work on that
// connection. Running the migration on the same session as the lock is what
// keeps the whole thing correct on a single-connection pool (MaxOpenConns(1)),
// which a separate lock connection would deadlock.
//
//   - Postgres: a session-level advisory lock on the pinned connection,
//     acquired via pg_try_advisory_lock in a ctx-aware poll loop. A second
//     instance waits until the first releases, or returns promptly if its ctx
//     is cancelled. (A poll loop is used rather than the blocking
//     pg_advisory_lock because lib/pq does not interrupt a blocked
//     pg_advisory_lock on context cancellation. A stuck holder would
//     otherwise hang boot forever.) This is the guard that makes
//     auto-migrate-on-boot safe across N replicas.
//   - SQLite: a leased lock row in _gofastr_migrate_lock, the twin of the
//     seed lease (framework/migrate/seed.go). SQLite's file-level locking
//     serializes individual statements, not the applied-versions read →
//     apply → record sequence, so two replicas booting against one file
//     could both compute the same pending set and the loser failed its boot
//     on DDL the winner had already applied. The lease (plus the
//     per-migration tracking-row re-check in runMigrationUp) closes that:
//     the second instance waits for the holder, then no-ops. A crashed
//     holder's lease expires and the next boot proceeds. The PK upgrade
//     (rebuildTableSQLite) is NOT idempotent against a table that already
//     has group_name values. That is why only Up/Down/Force call
//     ensureCompositeKey, never Status (which is unlocked).
//
// db == nil runs fn(nil). Callers already treat a nil db as a no-op.
func WithAdvisoryLock(ctx context.Context, db *sql.DB, dialect Dialect, fn func(conn *sql.Conn) error) error {
	return WithAdvisoryLockKey(ctx, db, dialect, AdvisoryLockKey, fn)
}

// WithAdvisoryLockKey is WithAdvisoryLock with an explicit lock key, for hosts
// that need to namespace the lock away from other migration tooling sharing
// the database. The key names the Postgres advisory lock; the SQLite arm uses
// its own fixed lease table and ignores it.
func WithAdvisoryLockKey(ctx context.Context, db *sql.DB, dialect Dialect, key int64, fn func(conn *sql.Conn) error) error {
	if db == nil {
		return fn(nil)
	}

	// SQLite arm: take the leased lock row BEFORE pinning the connection, so
	// the lease statements (and its heartbeat) run on the pool while fn owns
	// the pinned conn. A process-level mutex orders same-process callers
	// first: two goroutines whose pool connections may not even share a
	// database (an in-memory SQLite pool is per-connection) could not be
	// ordered by any row.
	if dialect == DialectSQLite {
		sqliteMigrateMu.Lock()
		defer sqliteMigrateMu.Unlock()
		release, err := AcquireSQLiteLease(ctx, db, "_gofastr_migrate_lock", migrateLease, "migrate")
		if err != nil {
			return fmt.Errorf("migrate lock: sqlite lease: %w", err)
		}
		defer release()
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
	// between tries, unlike a single blocking pg_advisory_lock.
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
		// best-effort: closing the dedicated connection also releases the
		// session advisory lock; this explicit unlock shortens that window.
		_, _ = conn.ExecContext(context.Background(), "SELECT pg_advisory_unlock($1)", key)
	}()

	return fn(conn)
}

// sqliteMigrateMu serializes WithAdvisoryLock callers within one process on
// SQLite. The leased lock row orders processes against a shared file, but two
// goroutines in one process may hold pool connections that do not even share
// a database (an in-memory SQLite pool is per-connection), which no row could
// order. Same rationale as framework/migrate's sqliteSeedMu.
var sqliteMigrateMu sync.Mutex

// migrateLease is how long a _gofastr_migrate_lock row counts as held before
// another process may steal it. A holder renews at lease/3, so a live process
// never loses the lock; a crashed one blocks other boots for at most one
// lease. If a slow migration still outruns the lease, the runMigrationUp
// tracking-row re-check makes the over-stepping runner converge instead of
// corrupting or failing its boot.
const migrateLease = 60 * time.Second

// AcquireSQLiteLease takes a cross-process leased lock on SQLite: one row in
// the named lock table, acquired by a single atomic upsert whose DO UPDATE
// fires only when the previous lease has expired (SQLite's own clock via
// strftime('%s','now'), so processes disagreeing about wall time don't
// stretch or shrink the lease). While held, a heartbeat renews the row at
// lease/3; the returned release func stops the heartbeat and deletes the
// row. Waiting respects ctx: cancel it to stop waiting for the current
// holder.
//
// It is the canonical lease acquisition formerly duplicated as core/
// migrate's acquireSQLiteMigrateLease (_gofastr_migrate_lock, the migration
// lease) and framework/migrate's acquireSQLiteSeedLease (_gofastr_seed_lock,
// the seed lease); the table is a parameter so a boot holding both locks
// (migrate then seed) never self-deadlocks. what names the lock in the
// heartbeat and release warnings ("migrate", "seed").
func AcquireSQLiteLease(ctx context.Context, db *sql.DB, table string, lease time.Duration, what string) (release func(), err error) {
	lockTable := query.QuoteIdent(query.MustIdent(table))
	if _, err := db.ExecContext(ctx, fmt.Sprintf(
		"CREATE TABLE IF NOT EXISTS %s (id INTEGER PRIMARY KEY CHECK (id = 1), expires_at INTEGER NOT NULL)", lockTable)); err != nil {
		return nil, err
	}
	acquire := fmt.Sprintf(`INSERT INTO %s (id, expires_at)
VALUES (1, CAST(strftime('%%s','now') AS INTEGER) + ?)
ON CONFLICT (id) DO UPDATE SET expires_at = excluded.expires_at
WHERE %s.expires_at <= CAST(strftime('%%s','now') AS INTEGER)`, lockTable, lockTable)
	leaseSeconds := int64(lease / time.Second)
	for {
		res, err := db.ExecContext(ctx, acquire, leaseSeconds)
		if err != nil {
			return nil, err
		}
		if n, _ := res.RowsAffected(); n > 0 {
			break
		}
		// Held by another process. Wait and retry; the holder either
		// releases (row deleted), its lease expires (steal succeeds), or
		// ctx is cancelled (fail closed, nothing acquired by us).
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(100 * time.Millisecond):
		}
	}
	// Heartbeat: the work held under the lease can take arbitrarily long,
	// far past the lease, so renew at lease/3 while the holder is alive. If
	// this process dies the heartbeats stop and the lease expires, which is
	// the crash-release path — there is no SQLite session cleanup to do it
	// for us, unlike a Postgres session advisory lock.
	hbCtx, stopHB := context.WithCancel(context.WithoutCancel(ctx))
	done := make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(lease / 3)
		defer ticker.Stop()
		renew := fmt.Sprintf(
			"UPDATE %s SET expires_at = CAST(strftime('%%s','now') AS INTEGER) + ? WHERE id = 1", lockTable)
		for {
			select {
			case <-hbCtx.Done():
				return
			case <-ticker.C:
				// A failed renewal is survivable but not silent: if it keeps
				// failing the lease expires and another process may steal the
				// run, so surface it.
				if _, err := db.ExecContext(hbCtx, renew, leaseSeconds); err != nil {
					slog.Warn(what+" lock lease renewal failed; the lease expires if this keeps failing",
						"err", err)
				}
			}
		}
	}()
	return func() {
		stopHB() // stops the goroutine before the DELETE races a renewal
		<-done
		if _, err := db.ExecContext(context.WithoutCancel(ctx), fmt.Sprintf("DELETE FROM %s WHERE id = 1", lockTable)); err != nil {
			// The lock still opens: the lease expires on its own after one
			// lease period. Other boots wait that long instead of running
			// immediately, which deserves a log line, not silence.
			slog.Warn(what+" lock release failed; the lease expires instead", "err", err)
		}
	}, nil
}

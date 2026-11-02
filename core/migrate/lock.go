package migrate

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
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
//
// On SQLite the lease lasts migrateLease from when it is taken, and fn
// renews it only through the runner's keep calls (withLeasedLock), made
// between migrations: there is no background renewal while fn holds the
// connection. Work under this exported entry point must finish within the
// lease, or another process may take the lock.
func WithAdvisoryLockKey(ctx context.Context, db *sql.DB, dialect Dialect, key int64, fn func(conn *sql.Conn) error) error {
	return withLeasedLock(ctx, db, dialect, key, func(conn *sql.Conn, _ func(context.Context) error) error {
		return fn(conn)
	})
}

// errLeaseLost reports that another process holds the SQLite lease this
// holder took: its own expired and was taken.
var errLeaseLost = errors.New("lease lost to another process")

// withLeasedLock is WithAdvisoryLockKey for a runner that can renew: fn
// gets keep, which renews the SQLite lease on the pinned connection and
// returns errLeaseLost once another process holds it. keep must run while
// the connection has no open transaction (between migrations): a renewal
// inside one would commit or roll back with it. On Postgres, and with a
// nil db, keep does nothing.
func withLeasedLock(ctx context.Context, db *sql.DB, dialect Dialect, key int64, fn func(conn *sql.Conn, keep func(context.Context) error) error) error {
	noKeep := func(context.Context) error { return nil }
	if db == nil {
		return fn(nil, noKeep)
	}

	// A process-level mutex orders same-process SQLite callers first: two
	// goroutines whose pool connections may not even share a database (an
	// in-memory SQLite pool is per-connection) could not be ordered by any
	// row. It is taken before the pin, so a one-connection pool queues
	// here rather than on the pool.
	if dialect == DialectSQLite {
		sqliteMigrateMu.Lock()
		defer sqliteMigrateMu.Unlock()
	}

	// Pin a single connection for the whole lock lifetime. The lock (Postgres)
	// and the migration work both run on this one session, so the lock is held
	// for the exact duration of the work and unlock targets the same backend.
	conn, err := db.Conn(ctx)
	if err != nil {
		return fmt.Errorf("migrate lock: acquire connection: %w", err)
	}
	defer conn.Close()

	if dialect == DialectSQLite {
		// The lease is taken, renewed and released on the pinned
		// connection: a renewal through the pool waited for this very
		// connection on a one-connection pool, so nothing renewed and the
		// lease expired mid-run; and an in-memory pool's other connections
		// are other databases.
		lease, err := acquireSQLiteLeaseOn(ctx, conn, "_gofastr_migrate_lock", migrateLease, "migrate")
		if err != nil {
			return fmt.Errorf("migrate lock: sqlite lease: %w", err)
		}
		defer lease.release(context.WithoutCancel(ctx), conn)
		return fn(conn, func(ctx context.Context) error { return lease.keep(ctx, conn) })
	}
	if dialect != DialectPostgres {
		return fn(conn, noKeep)
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
	//
	// conn.Close returns the connection to the pool; it does not end the
	// session, so an unlock that fails leaves the lock held by a pooled
	// connection for the life of the process, and every other replica
	// waits on it. A NoTransaction migration that ran its own BEGIN and
	// failed inside it leaves the session in an aborted transaction, which
	// refuses the unlock. Such a connection is retired instead: ending the
	// session releases its locks and discards the aborted transaction.
	defer func() {
		var released bool
		err := conn.QueryRowContext(context.Background(), "SELECT pg_advisory_unlock($1)", key).Scan(&released)
		if err != nil || !released {
			slog.Warn("migrate lock: the advisory unlock failed; retiring the connection so its session releases the lock",
				"err", err, "released", released)
			_ = conn.Raw(func(any) error { return driver.ErrBadConn })
		}
	}()

	return fn(conn, noKeep)
}

// sqliteMigrateMu serializes WithAdvisoryLock callers within one process on
// SQLite. The leased lock row orders processes against a shared file, but two
// goroutines in one process may hold pool connections that do not even share
// a database (an in-memory SQLite pool is per-connection), which no row could
// order. Same rationale as framework/migrate's sqliteSeedMu.
var sqliteMigrateMu sync.Mutex

// migrateLease is how long a _gofastr_migrate_lock row counts as held before
// another process may steal it. The runner renews it on its pinned
// connection before each migration, so a run of any length keeps it as long
// as no single migration outlasts the lease (SQLite's one writer also blocks
// any renewal from elsewhere while a migration's transaction is open). A
// crashed holder blocks other boots for at most one lease. A var so a test
// can shrink it.
var migrateLease = 60 * time.Second

// sqliteLease is a held lease row. fence is the expires_at this holder last
// wrote: renew and release match on it, so a holder whose lease expired and
// was taken cannot extend or delete the new holder's row. Every taking of
// the row writes a later expiry than the row held (it fires only once now
// has reached the old one, and adds a lease of at least a second), so the
// fence never matches another holder's row. A token column would also
// fence, but needs a schema change to every existing lock table, and an
// older binary taking the row during a rolling deploy would leave the
// previous holder's token in place; the expiry it writes still moves.
type sqliteLease struct {
	table      string // quoted
	renewSQL   string // the fenced renewal statement
	releaseSQL string // the fenced release statement
	seconds    int64
	what       string

	mu    sync.Mutex
	fence int64
	lost  bool
}

// leaseConn is what the lease statements run on: the pool, or a pinned
// connection.
type leaseConn interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// acquireSQLiteLeaseOn takes the lease row on x, waiting while another
// process holds it.
func acquireSQLiteLeaseOn(ctx context.Context, x leaseConn, table string, lease time.Duration, what string) (*sqliteLease, error) {
	lockTable := query.QuoteFolded(query.MustIdent(table))
	if _, err := x.ExecContext(ctx, fmt.Sprintf(
		"CREATE TABLE IF NOT EXISTS %s (id INTEGER PRIMARY KEY CHECK (id = 1), expires_at INTEGER NOT NULL)", lockTable)); err != nil {
		return nil, err
	}
	acquire := fmt.Sprintf(`INSERT INTO %s (id, expires_at)
VALUES (1, CAST(strftime('%%s','now') AS INTEGER) + ?)
ON CONFLICT (id) DO UPDATE SET expires_at = excluded.expires_at
WHERE %s.expires_at <= CAST(strftime('%%s','now') AS INTEGER)
RETURNING expires_at`, lockTable, lockTable)
	l := &sqliteLease{
		table: lockTable, seconds: max(int64(lease/time.Second), 1), what: what,
		renewSQL:   fmt.Sprintf("UPDATE %s SET expires_at = CAST(strftime('%%s','now') AS INTEGER) + ? WHERE id = 1 AND expires_at = ? RETURNING expires_at", lockTable),
		releaseSQL: fmt.Sprintf("DELETE FROM %s WHERE id = 1 AND expires_at = ?", lockTable),
	}
	for {
		err := x.QueryRowContext(ctx, acquire, l.seconds).Scan(&l.fence)
		if err == nil {
			return l, nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return nil, err
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
}

// renew extends the lease on x if this holder still owns the row. It
// returns errLeaseLost, logged once, when another process holds it.
func (l *sqliteLease) renew(ctx context.Context, x leaseConn) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.lost {
		return errLeaseLost
	}
	err := x.QueryRowContext(ctx, l.renewSQL, l.seconds, l.fence).Scan(&l.fence)
	if errors.Is(err, sql.ErrNoRows) {
		l.markLost()
		return errLeaseLost
	}
	return err
}

// keep is the runner's renewal between migrations: a lost lease stops the
// run; any other failure is logged and the run goes on, since the lease
// may still hold and the next keep tries again.
func (l *sqliteLease) keep(ctx context.Context, x leaseConn) error {
	err := l.renew(ctx, x)
	if err != nil && !errors.Is(err, errLeaseLost) {
		slog.Warn(l.what+" lock lease renewal failed; the lease expires if this keeps failing", "err", err)
		return nil
	}
	return err
}

// release deletes the row if this holder still owns it.
func (l *sqliteLease) release(ctx context.Context, x leaseConn) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.lost {
		return
	}
	res, err := x.ExecContext(ctx, l.releaseSQL, l.fence)
	if err != nil {
		// The lock still opens: the lease expires on its own after one
		// lease period. Other boots wait that long instead of running
		// immediately, which deserves a log line, not silence.
		slog.Warn(l.what+" lock release failed; the lease expires instead", "err", err)
		return
	}
	if n, err := res.RowsAffected(); err == nil && n == 0 {
		l.markLost()
	}
}

// markLost records and logs the loss; l.mu is held.
func (l *sqliteLease) markLost() {
	l.lost = true
	slog.Error(l.what+" lock lease lost: it expired and another process took it, so the work it guarded may have run there at the same time",
		"table", l.table)
}

// AcquireSQLiteLease takes a cross-process leased lock on SQLite: one row in
// the named lock table, acquired by a single atomic upsert whose DO UPDATE
// fires only when the previous lease has expired (SQLite's own clock via
// strftime('%s','now'), so processes disagreeing about wall time don't
// stretch or shrink the lease). While held, a heartbeat renews the row
// through the pool at lease/3; the returned release func stops the
// heartbeat and deletes the row. Renewal and release are fenced on the
// expiry this holder last wrote (sqliteLease): a holder whose lease was
// taken logs the loss and leaves the new holder's row alone. Waiting
// respects ctx: cancel it to stop waiting for the current holder.
//
// The heartbeat needs a free pool connection: on a one-connection pool it
// renews only while the holder's work is not holding that connection.
//
// It is the canonical lease acquisition formerly duplicated as core/
// migrate's acquireSQLiteMigrateLease (_gofastr_migrate_lock, the migration
// lease) and framework/migrate's acquireSQLiteSeedLease (_gofastr_seed_lock,
// the seed lease); the table is a parameter so a boot holding both locks
// (migrate then seed) never self-deadlocks. what names the lock in the
// heartbeat and release warnings ("migrate", "seed").
func AcquireSQLiteLease(ctx context.Context, db *sql.DB, table string, lease time.Duration, what string) (release func(), err error) {
	l, err := acquireSQLiteLeaseOn(ctx, db, table, lease, what)
	if err != nil {
		return nil, err
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
		for {
			select {
			case <-hbCtx.Done():
				return
			case <-ticker.C:
				err := l.renew(hbCtx, db)
				if errors.Is(err, errLeaseLost) {
					return // logged by renew; the row is another holder's now
				}
				// A failed renewal is survivable but not silent: if it keeps
				// failing the lease expires and another process may steal the
				// run, so surface it.
				if err != nil && hbCtx.Err() == nil {
					slog.Warn(what+" lock lease renewal failed; the lease expires if this keeps failing",
						"err", err)
				}
			}
		}
	}()
	return func() {
		stopHB() // stops the goroutine before the DELETE races a renewal
		<-done
		l.release(context.WithoutCancel(ctx), db)
	}, nil
}

package cron

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"fmt"
	"log/slog"
	"sync"
	"time"
)

// cronTicksTable records, per lease key, the last tick minute a replica
// claimed. PostgresAdvisoryLease.AcquireTick creates it on first use.
const cronTicksTable = "gofastr_cron_ticks"

// PostgresAdvisoryLease is a [LeaderElection] backed by a Postgres advisory
// lock (pg_try_advisory_lock / pg_advisory_unlock). Every replica pointing at
// the same Postgres and using the same Key coordinates on the lock: the holder
// fires the tick, the others skip. Advisory locks are session-scoped, so each
// Acquire pins a dedicated connection for the lock's lifetime and unlock reuses
// that same connection before returning it to the pool.
//
// The lease also implements [TickLeaderElection]: AcquireTick records the
// tick minute it fired in the gofastr_cron_ticks table (created on first
// use), so a replica whose timer fires for the same minute after the leader
// released the lock skips it instead of running the jobs a second time. The
// scheduler uses AcquireTick and releases the lock as soon as the claim is
// made, so the pinned connection is held for the lock and the claim only,
// never while the jobs run.
//
// Pool sizing: through plain Acquire, the lock's connection stays pinned
// for as long as the caller holds the lease, so the DB pool should allow at
// least 2 open connections when the work done under it uses the database,
// otherwise the holder has the only connection and that work deadlocks.
type PostgresAdvisoryLease struct {
	db  *sql.DB
	key int64

	// ensureMu guards tableReady: the ticks table is created once per
	// lease, retried on the next tick when creation fails.
	ensureMu   sync.Mutex
	tableReady bool
}

// NewPostgresAdvisoryLease creates a leader-election lease over a Postgres
// advisory lock identified by key. Every coordinating replica MUST use the
// same key; pick a fixed value to avoid colliding with the framework's own
// advisory locks (see core/migrate's pinned AdvisoryLockKey).
func NewPostgresAdvisoryLease(db *sql.DB, key int64) *PostgresAdvisoryLease {
	return &PostgresAdvisoryLease{db: db, key: key}
}

// Acquire tries to take the advisory lock. On success it returns the pinned
// connection's release function (which unlocks and returns the conn). A
// non-leader returns (false, nil, nil) and closes its speculative connection.
func (p *PostgresAdvisoryLease) Acquire(ctx context.Context) (bool, func(), error) {
	conn, err := p.lock(ctx)
	if conn == nil || err != nil {
		return false, nil, err
	}
	return true, p.unlocker(conn), nil
}

// AcquireTick takes the advisory lock and then claims tick (the minute the
// scheduler is firing) for this lease key. It holds only when both succeed:
// a tick minute at or before the last one any replica claimed is refused,
// the lock released at once, and (false, nil, nil) returned. The claim is a
// single conditional upsert (INSERT ... ON CONFLICT DO UPDATE ... WHERE
// last_tick < $2) run on the pinned lock connection, so a one-connection
// pool cannot deadlock on it, and atomic on its own, so the at-most-once
// record does not depend on the lock. A claim error releases the lock and
// is returned; the scheduler reports it and skips the tick.
func (p *PostgresAdvisoryLease) AcquireTick(ctx context.Context, tick time.Time) (bool, func(), error) {
	conn, err := p.lock(ctx)
	if conn == nil || err != nil {
		return false, nil, err
	}
	release := p.unlocker(conn)
	claimed, err := p.claimTick(ctx, conn, tick)
	if err != nil || !claimed {
		release()
		return false, nil, err
	}
	return true, release, nil
}

// lock pins a connection and tries the advisory lock on it. It returns the
// pinned connection when the lock is held, and (nil, nil) when another
// session holds it.
func (p *PostgresAdvisoryLease) lock(ctx context.Context) (*sql.Conn, error) {
	conn, err := p.db.Conn(ctx)
	if err != nil {
		return nil, fmt.Errorf("cron: lease connection: %w", err)
	}
	var got bool
	if err := conn.QueryRowContext(ctx, "SELECT pg_try_advisory_lock($1)", p.key).Scan(&got); err != nil {
		conn.Close()
		return nil, fmt.Errorf("cron: pg_try_advisory_lock: %w", err)
	}
	if !got {
		conn.Close()
		return nil, nil
	}
	return conn, nil
}

// claimTick records tick as this key's last fired minute unless an equal or
// later minute is already recorded, and reports whether it did.
func (p *PostgresAdvisoryLease) claimTick(ctx context.Context, conn *sql.Conn, tick time.Time) (bool, error) {
	if err := p.ensureTicksTable(ctx, conn); err != nil {
		return false, err
	}
	res, err := conn.ExecContext(ctx, `INSERT INTO `+cronTicksTable+` (lease_key, last_tick) VALUES ($1, $2)
		ON CONFLICT (lease_key) DO UPDATE SET last_tick = EXCLUDED.last_tick
		WHERE `+cronTicksTable+`.last_tick < EXCLUDED.last_tick`, p.key, tick.UTC())
	if err != nil {
		return false, fmt.Errorf("cron: claim tick %s: %w", tick.UTC().Format(time.RFC3339), err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("cron: claim tick %s: %w", tick.UTC().Format(time.RFC3339), err)
	}
	return n == 1, nil
}

// ensureTicksTable creates the ticks table once per lease. Two leases on
// different keys can race CREATE TABLE IF NOT EXISTS (Postgres reports the
// loser as a pg_type unique violation), so a failed create is tried once
// more, by which point the winner's table exists and the retry is a no-op.
func (p *PostgresAdvisoryLease) ensureTicksTable(ctx context.Context, conn *sql.Conn) error {
	p.ensureMu.Lock()
	defer p.ensureMu.Unlock()
	if p.tableReady {
		return nil
	}
	const ddl = `CREATE TABLE IF NOT EXISTS ` + cronTicksTable + ` (
		lease_key BIGINT PRIMARY KEY,
		last_tick TIMESTAMPTZ NOT NULL
	)`
	if _, err := conn.ExecContext(ctx, ddl); err != nil {
		if _, err := conn.ExecContext(ctx, ddl); err != nil {
			return fmt.Errorf("cron: create %s: %w", cronTicksTable, err)
		}
	}
	p.tableReady = true
	return nil
}

// unlocker returns the release func for a lock held on conn.
func (p *PostgresAdvisoryLease) unlocker(conn *sql.Conn) func() {
	return func() {
		// Unlock on the SAME session-scoped connection, then return it. Use a
		// background context so shutdown-time release is not cancelled by the
		// scheduler's parent context being torn down.
		//
		// conn.Close returns the connection to the pool; it does not end
		// the session, and a session-scoped advisory lock lives as long as
		// the session. An unlock that fails on a live session (a cancelled
		// statement, a statement timeout) left the lock held by a pooled
		// connection, so no other replica could lead until this process
		// exited. Such a connection is retired instead: ending its session
		// releases the lock.
		var released bool
		err := conn.QueryRowContext(context.Background(), "SELECT pg_advisory_unlock($1)", p.key).Scan(&released)
		if err != nil || !released {
			slog.Warn("cron: the leader lease unlock failed; retiring the connection so its session releases the lock",
				"key", p.key, "err", err, "released", released)
			_ = conn.Raw(func(any) error { return driver.ErrBadConn })
		}
		_ = conn.Close()
	}
}

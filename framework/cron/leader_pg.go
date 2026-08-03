package cron

import (
	"context"
	"database/sql"
	"fmt"
)

// PostgresAdvisoryLease is a [LeaderElection] backed by a Postgres advisory
// lock (pg_try_advisory_lock / pg_advisory_unlock). Every replica pointing at
// the same Postgres and using the same Key coordinates on the lock: the holder
// fires the tick, the others skip. Advisory locks are session-scoped, so each
// Acquire pins a dedicated connection for the lock's lifetime and unlock reuses
// that same connection before returning it to the pool.
//
// Pool sizing: each tick pins one connection for the lock's duration (held
// until the tick's jobs finish), so the DB pool should allow at least 2 open
// connections when the scheduled jobs themselves use the database — otherwise
// the leader holds the only connection and the jobs deadlock on it.
type PostgresAdvisoryLease struct {
	db  *sql.DB
	key int64
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
	conn, err := p.db.Conn(ctx)
	if err != nil {
		return false, nil, fmt.Errorf("cron: lease connection: %w", err)
	}
	var got bool
	if err := conn.QueryRowContext(ctx, "SELECT pg_try_advisory_lock($1)", p.key).Scan(&got); err != nil {
		conn.Close()
		return false, nil, fmt.Errorf("cron: pg_try_advisory_lock: %w", err)
	}
	if !got {
		conn.Close()
		return false, nil, nil
	}
	return true, func() {
		// Unlock on the SAME session-scoped connection, then return it. Use a
		// background context so shutdown-time release is not cancelled by the
		// scheduler's parent context being torn down.
		//
		// best-effort: a session-scoped advisory lock is released by Postgres
		// when the session ends, and the very next statement closes it. A
		// failure here means the connection is already gone, which has the
		// same outcome — there is nothing to report and nothing to retry.
		_, _ = conn.ExecContext(context.Background(), "SELECT pg_advisory_unlock($1)", p.key)
		_ = conn.Close()
	}, nil
}

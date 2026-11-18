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

const cronTicksTable = "gofastr_cron_ticks"

// PostgresAdvisoryLease is a [TickLeaderElection] on a Postgres advisory lock
// on a pinned connection, its claims in gofastr_cron_ticks. Plain Acquire
// pins the connection while the lease is held, so work under it needs a
// second pool connection.
type PostgresAdvisoryLease struct {
	db  *sql.DB
	key int64

	ensureMu   sync.Mutex
	tableReady bool
}

// NewPostgresAdvisoryLease returns a lease on lock key, shared by every
// coordinating replica and distinct from core/migrate's AdvisoryLockKey.
func NewPostgresAdvisoryLease(db *sql.DB, key int64) *PostgresAdvisoryLease {
	return &PostgresAdvisoryLease{db: db, key: key}
}

// Acquire takes the lock, returning a release that unlocks and returns the
// connection, or (false, nil, nil) when another session holds it.
func (p *PostgresAdvisoryLease) Acquire(ctx context.Context) (bool, func(), error) {
	conn, err := p.lock(ctx)
	if conn == nil || err != nil {
		return false, nil, err
	}
	return true, p.unlocker(conn), nil
}

// AcquireTick takes the lock and claims tick with one conditional upsert on
// the lock's connection, holding only when both succeed.
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

func (p *PostgresAdvisoryLease) ensureTicksTable(ctx context.Context, conn *sql.Conn) error {
	p.ensureMu.Lock()
	defer p.ensureMu.Unlock()
	if p.tableReady {
		return nil
	}
	const ddl = `CREATE TABLE IF NOT EXISTS ` + cronTicksTable + ` (
		lease_key BIGINT PRIMARY KEY NOT NULL,
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

func (p *PostgresAdvisoryLease) unlocker(conn *sql.Conn) func() {
	return func() {
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

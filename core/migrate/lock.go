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

var lockPollInterval = 200 * time.Millisecond

// AdvisoryLockKey is the Postgres advisory lock key of the migration lock.
const AdvisoryLockKey int64 = 6724469554113028193

// SeedAdvisoryLockKey is the Postgres advisory lock key of the seed lock.
const SeedAdvisoryLockKey int64 = 7583194026157293042

// WithAdvisoryLock runs fn on one pinned connection while holding the
// migration lock across processes; a nil db runs fn(nil).
func WithAdvisoryLock(ctx context.Context, db *sql.DB, dialect Dialect, fn func(conn *sql.Conn) error) error {
	return WithAdvisoryLockKey(ctx, db, dialect, AdvisoryLockKey, fn)
}

// WithAdvisoryLockKey is WithAdvisoryLock with a Postgres key of the
// caller's; on SQLite, fn must finish within one lease.
func WithAdvisoryLockKey(ctx context.Context, db *sql.DB, dialect Dialect, key int64, fn func(conn *sql.Conn) error) error {
	return withLeasedLock(ctx, db, dialect, key, func(conn *sql.Conn, _ func(context.Context) error) error {
		return fn(conn)
	})
}

var errLeaseLost = errors.New("lease lost to another process")

func withLeasedLock(ctx context.Context, db *sql.DB, dialect Dialect, key int64, fn func(conn *sql.Conn, keep func(context.Context) error) error) error {
	noKeep := func(context.Context) error { return nil }
	if db == nil {
		return fn(nil, noKeep)
	}

	if dialect == DialectSQLite {
		sqliteMigrateMu.Lock()
		defer sqliteMigrateMu.Unlock()
	}

	conn, err := db.Conn(ctx)
	if err != nil {
		return fmt.Errorf("migrate lock: acquire connection: %w", err)
	}
	defer conn.Close()

	if dialect == DialectSQLite {
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

var sqliteMigrateMu sync.Mutex

var migrateLease = 60 * time.Second

type sqliteLease struct {
	table      string
	renewSQL   string
	releaseSQL string
	seconds    int64
	what       string

	mu    sync.Mutex
	fence int64
	lost  bool
}

type leaseConn interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

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
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(100 * time.Millisecond):
		}
	}
}

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

func (l *sqliteLease) keep(ctx context.Context, x leaseConn) error {
	err := l.renew(ctx, x)
	if err != nil && !errors.Is(err, errLeaseLost) {
		slog.Warn(l.what+" lock lease renewal failed; the lease expires if this keeps failing", "err", err)
		return nil
	}
	return err
}

func (l *sqliteLease) release(ctx context.Context, x leaseConn) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.lost {
		return
	}
	res, err := x.ExecContext(ctx, l.releaseSQL, l.fence)
	if err != nil {
		slog.Warn(l.what+" lock release failed; the lease expires instead", "err", err)
		return
	}
	if n, err := res.RowsAffected(); err == nil && n == 0 {
		l.markLost()
	}
}

func (l *sqliteLease) markLost() {
	l.lost = true
	slog.Error(l.what+" lock lease lost: it expired and another process took it, so the work it guarded may have run there at the same time",
		"table", l.table)
}

// AcquireSQLiteLease takes the lease row of table on SQLite, waiting while
// another process holds it, and renews it until release is called.
func AcquireSQLiteLease(ctx context.Context, db *sql.DB, table string, lease time.Duration, what string) (release func(), err error) {
	l, err := acquireSQLiteLeaseOn(ctx, db, table, lease, what)
	if err != nil {
		return nil, err
	}
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
					return
				}
				if err != nil && hbCtx.Err() == nil {
					slog.Warn(what+" lock lease renewal failed; the lease expires if this keeps failing",
						"err", err)
				}
			}
		}
	}()
	return func() {
		stopHB()
		<-done
		l.release(context.WithoutCancel(ctx), db)
	}, nil
}

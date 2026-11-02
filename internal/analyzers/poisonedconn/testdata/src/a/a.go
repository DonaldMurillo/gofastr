package a

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"fmt"
	"log/slog"
)

// withLeasedLock is core/migrate before a8ba13471: the unlock's error is
// dropped, and Exec discards the false it returns on an aborted session.
func withLeasedLock(ctx context.Context, db *sql.DB, key int64, fn func(*sql.Conn) error) error {
	conn, err := db.Conn(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()
	if _, err := conn.ExecContext(ctx, "SELECT pg_advisory_lock($1)", key); err != nil {
		return err
	}
	defer func() {
		_, _ = conn.ExecContext(context.Background(), "SELECT pg_advisory_unlock($1)", key) // want `poisonedconn: pg_advisory_unlock returns false, not an error, when this session does not hold the lock, and ExecContext discards it`
	}()
	return fn(conn)
}

// unlocker is framework/cron's lease release before 4d8705a8b.
type lease struct{ key int64 }

func (p *lease) unlocker(conn *sql.Conn) func() {
	return func() {
		_, _ = conn.ExecContext(context.Background(), "SELECT pg_advisory_unlock($1)", p.key) // want `poisonedconn: pg_advisory_unlock returns false`
		_ = conn.Close()
	}
}

// The a8ba13471 / 4d8705a8b spelling: scan the result, retire on failure.
func (p *lease) unlockerFixed(conn *sql.Conn) func() {
	return func() {
		var released bool
		err := conn.QueryRowContext(context.Background(), "SELECT pg_advisory_unlock($1)", p.key).Scan(&released)
		if err != nil || !released {
			_ = conn.Raw(func(any) error { return driver.ErrBadConn })
		}
		_ = conn.Close()
	}
}

// Scanned, but nothing retires the connection: still poisoned.
func (p *lease) scannedNotRetired(ctx context.Context, conn *sql.Conn) {
	var released bool
	if err := conn.QueryRowContext(ctx, "SELECT pg_advisory_unlock($1)", p.key).Scan(&released); err != nil || !released { // want `poisonedconn: "SELECT pg_advisory_unlock\(\$1\)" on the pinned conn can fail, and nothing here retires the connection`
		slog.Warn("unlock failed")
	}
	_ = conn.Close()
}

// A session reset whose error is only logged before Close: fires.
func resetSession(ctx context.Context, conn *sql.Conn) {
	if _, err := conn.ExecContext(ctx, "RESET ALL"); err != nil { // want `poisonedconn: "RESET ALL" on the pinned conn can fail`
		slog.Warn("reset failed", "err", err)
	}
	_ = conn.Close()
}

// Dropped outright: fires.
func discard(ctx context.Context, conn *sql.Conn) {
	_, _ = conn.ExecContext(ctx, "DISCARD ALL") // want `poisonedconn: "DISCARD ALL" on the pinned conn`
	_, _ = conn.ExecContext(ctx, "UNLISTEN *")  // want `poisonedconn: "UNLISTEN \*" on the pinned conn`
}

// Session-scoped set_config (third argument false) through Sprintf: fires.
func setTenant(ctx context.Context, conn *sql.Conn, table string) {
	_, _ = conn.ExecContext(ctx, fmt.Sprintf("SELECT set_config('app.tenant', '', false) FROM %s LIMIT 1", table)) // want `poisonedconn: "SELECT set_config`
	_, _ = conn.ExecContext(ctx, "SET search_path TO public")                                                      // want `poisonedconn: "SET search_path TO public"`
}

// A retire helper the conn is handed to counts as the retire.
func retire(c *sql.Conn) {
	_ = c.Raw(func(any) error { return driver.ErrBadConn })
}

func rollbackOrRetire(ctx context.Context, conn *sql.Conn) {
	if _, err := conn.ExecContext(ctx, "ROLLBACK"); err != nil {
		retire(conn) // quiet
	}
}

// ---- silent postures ----

// A Tx's rollback ends with the Tx; a DB statement pins no session.
func txAndDB(ctx context.Context, db *sql.DB, tx *sql.Tx) {
	_, _ = tx.ExecContext(ctx, "ROLLBACK TO SAVEPOINT sp")
	_, _ = db.ExecContext(ctx, "SET search_path TO public")
	_ = tx.Rollback()
}

// Transaction-scoped settings and statements that clear nothing.
func notResets(ctx context.Context, conn *sql.Conn, q string) {
	_, _ = conn.ExecContext(ctx, "SET LOCAL statement_timeout = 0")
	_, _ = conn.ExecContext(ctx, "SET TRANSACTION ISOLATION LEVEL SERIALIZABLE")
	_, _ = conn.ExecContext(ctx, "SELECT set_config('app.tenant', '', true)")
	_, _ = conn.ExecContext(ctx, "BEGIN IMMEDIATE")
	_, _ = conn.ExecContext(ctx, "SELECT pg_advisory_lock(1)")
	_, _ = conn.ExecContext(ctx, q) // not a constant
}

// A Raw call on a different conn does not retire this one.
func wrongConn(ctx context.Context, conn, other *sql.Conn) {
	if _, err := conn.ExecContext(ctx, "ROLLBACK"); err != nil { // want `poisonedconn: "ROLLBACK" on the pinned conn`
		_ = other.Raw(func(any) error { return driver.ErrBadConn })
	}
}

// A Raw literal that does not return ErrBadConn retires nothing.
func rawNoRetire(ctx context.Context, conn *sql.Conn) {
	if _, err := conn.ExecContext(ctx, "ROLLBACK"); err != nil { // want `poisonedconn: "ROLLBACK" on the pinned conn`
		_ = conn.Raw(func(any) error { return nil })
	}
}

package migrate

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"time"

	"github.com/DonaldMurillo/gofastr/core/query"
)

// MigrationRecord is a row in the _migrations tracking table.
type MigrationRecord struct {
	Version   uint64
	Name      string
	AppliedAt time.Time
	Checksum  string // SHA-256 of the Up SQL recorded at apply time
	Dirty     bool   // true if a no-transaction migration failed mid-apply
}

// Status holds the result of querying migration state.
type Status struct {
	Applied []MigrationRecord
	Pending []Migration
}

// nowFunc returns the dialect-appropriate default timestamp expression.
func (m *Migrator) nowFunc() string {
	if m.dialect == DialectSQLite {
		return "CURRENT_TIMESTAMP"
	}
	return "NOW()"
}

// placeholder returns the dialect-appropriate parameter placeholder for the nth arg.
func (m *Migrator) placeholder(n int) string {
	if m.dialect == DialectSQLite {
		return "?"
	}
	return fmt.Sprintf("$%d", n)
}

// connish is the subset of *sql.DB / *sql.Conn the runner needs. Threading it
// lets Up/Down run every statement — tracking-table DDL, applied-version
// reads, and each migration's transaction — on the single connection that
// holds the advisory lock, which is what keeps the runner correct on a
// MaxOpenConns(1) pool.
type connish interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	BeginTx(ctx context.Context, opts *sql.TxOptions) (*sql.Tx, error)
}

// qtable validates the tracking table name once and returns it quoted. Every
// public entry point calls this up front and threads the result into the
// helpers, so the table name is checked exactly once per operation (no
// redundant, unreachable re-validation deeper in the call tree).
func (m *Migrator) qtable() (string, error) {
	safe, err := query.SafeIdent(m.tableName)
	if err != nil {
		return "", fmt.Errorf("migrate: invalid table name %q: %w", m.tableName, err)
	}
	return query.QuoteIdent(safe), nil
}

// CreateMigrationsTable ensures the migrations tracking table exists. Public
// entry point (used by Status and tests); runs on the pool.
func (m *Migrator) CreateMigrationsTable(ctx context.Context) error {
	tbl, err := m.qtable()
	if err != nil {
		return err
	}
	return m.createMigrationsTable(ctx, m.db, tbl)
}

func (m *Migrator) createMigrationsTable(ctx context.Context, x connish, tbl string) error {
	ddl := fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
		version BIGINT NOT NULL PRIMARY KEY,
		name    TEXT    NOT NULL DEFAULT '',
		applied_at TIMESTAMP NOT NULL DEFAULT %s,
		checksum TEXT NOT NULL DEFAULT '',
		dirty BOOLEAN NOT NULL DEFAULT FALSE
	)`, tbl, m.nowFunc())
	if _, err := x.ExecContext(ctx, ddl); err != nil {
		return err
	}
	// Backfill checksum/dirty onto tables created by an older gofastr that
	// didn't have them. No-op for the freshly-created table above.
	return m.ensureTrackingColumns(ctx, x, tbl)
}

// appliedVersions returns the set of already-applied migration versions.
func (m *Migrator) appliedVersions(ctx context.Context, x connish, tbl string) (map[uint64]MigrationRecord, error) {
	q := fmt.Sprintf("SELECT version, name, applied_at, checksum, dirty FROM %s ORDER BY version", tbl)
	rows, err := x.QueryContext(ctx, q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	applied := make(map[uint64]MigrationRecord)
	for rows.Next() {
		var rec MigrationRecord
		if err := rows.Scan(&rec.Version, &rec.Name, &rec.AppliedAt, &rec.Checksum, &rec.Dirty); err != nil {
			return nil, err
		}
		applied[rec.Version] = rec
	}
	return applied, rows.Err()
}

// checkIntegrity refuses to proceed when the tracking state is unsafe:
//   - any migration is dirty (a no-transaction migration failed mid-apply), or
//   - an already-applied migration's recorded checksum no longer matches the
//     registered file (it was edited after being applied).
//
// A blank recorded checksum is skipped — those are legacy rows applied before
// checksums existed, and flagging them would be a false positive.
func (m *Migrator) checkIntegrity(applied map[uint64]MigrationRecord) error {
	for _, rec := range applied {
		if rec.Dirty {
			return fmt.Errorf("migration %d (%s): %w", rec.Version, rec.Name, ErrDirty)
		}
	}
	for _, mig := range m.migrations {
		rec, ok := applied[mig.Version]
		if !ok || rec.Checksum == "" {
			continue
		}
		if got := checksumOf(mig); got != rec.Checksum {
			return &ChecksumMismatchError{Version: mig.Version, Name: mig.Name, Recorded: rec.Checksum, Current: got}
		}
	}
	return nil
}

// pendingMigrations returns migrations that have not yet been applied,
// sorted by version ascending.
func pendingMigrations(registered []Migration, applied map[uint64]MigrationRecord) []Migration {
	var pending []Migration
	for _, mig := range registered {
		if _, ok := applied[mig.Version]; !ok {
			pending = append(pending, mig)
		}
	}
	sort.Slice(pending, func(i, j int) bool {
		return pending[i].Version < pending[j].Version
	})
	return pending
}

// Up runs all pending migrations in version order. Each migration executes in
// its own transaction. Already-applied migrations are skipped. The whole run
// is serialized by a database advisory lock so two instances (e.g. rolling-
// deploy replicas) cannot apply migrations concurrently.
func (m *Migrator) Up(ctx context.Context) error {
	tbl, err := m.qtable()
	if err != nil {
		return err
	}
	return WithAdvisoryLock(ctx, m.db, m.dialect, func(conn *sql.Conn) error {
		return m.up(ctx, conn, tbl)
	})
}

func (m *Migrator) up(ctx context.Context, x connish, tbl string) error {
	if err := m.createMigrationsTable(ctx, x, tbl); err != nil {
		return fmt.Errorf("creating migrations table: %w", err)
	}

	applied, err := m.appliedVersions(ctx, x, tbl)
	if err != nil {
		return fmt.Errorf("querying applied migrations: %w", err)
	}
	if err := m.checkIntegrity(applied); err != nil {
		return err
	}

	pending := pendingMigrations(m.migrations, applied)
	if len(pending) == 0 {
		return nil
	}

	for _, mig := range pending {
		if err := m.runMigrationUp(ctx, x, tbl, mig); err != nil {
			return fmt.Errorf("migration %d (%s): %w", mig.Version, mig.Name, err)
		}
	}

	return nil
}

// runMigrationUp executes a single migration's Up SQL and records it in the
// tracking table. Transactional migrations run the DDL and the bookkeeping
// insert in one atomic transaction. No-transaction migrations record a dirty
// row first, run the DDL outside any transaction, then clear the dirty flag —
// so a failure leaves a dirty marker that blocks subsequent runs.
func (m *Migrator) runMigrationUp(ctx context.Context, x connish, tbl string, mig Migration) error {
	if mig.NoTransaction {
		return m.runMigrationUpNoTx(ctx, x, tbl, mig)
	}

	tx, err := x.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}

	if _, err := tx.ExecContext(ctx, mig.Up); err != nil {
		_ = tx.Rollback()
		return fmt.Errorf("exec up: %w", err)
	}

	insertSQL := fmt.Sprintf(
		"INSERT INTO %s (version, name, applied_at, checksum, dirty) VALUES (%s, %s, %s, %s, FALSE)",
		tbl,
		m.placeholder(1), m.placeholder(2), m.placeholder(3), m.placeholder(4),
	)
	if _, err := tx.ExecContext(ctx, insertSQL, mig.Version, mig.Name, time.Now().UTC(), checksumOf(mig)); err != nil {
		_ = tx.Rollback()
		return fmt.Errorf("record migration: %w", err)
	}

	return tx.Commit()
}

// runMigrationUpNoTx applies a no-transaction migration. The dirty row is
// committed before the DDL runs so that a crash or failure mid-DDL is
// detectable on the next run.
func (m *Migrator) runMigrationUpNoTx(ctx context.Context, x connish, tbl string, mig Migration) error {
	insertSQL := fmt.Sprintf(
		"INSERT INTO %s (version, name, applied_at, checksum, dirty) VALUES (%s, %s, %s, %s, TRUE)",
		tbl, m.placeholder(1), m.placeholder(2), m.placeholder(3), m.placeholder(4),
	)
	if _, err := x.ExecContext(ctx, insertSQL, mig.Version, mig.Name, time.Now().UTC(), checksumOf(mig)); err != nil {
		return fmt.Errorf("record migration (dirty): %w", err)
	}

	if _, err := x.ExecContext(ctx, mig.Up); err != nil {
		// Leave the dirty row in place — it's the signal that this migration
		// half-applied and the database needs manual reconciliation.
		return fmt.Errorf("exec up (no-transaction, left dirty): %w", err)
	}

	clearSQL := fmt.Sprintf("UPDATE %s SET dirty = FALSE WHERE version = %s", tbl, m.placeholder(1))
	if _, err := x.ExecContext(ctx, clearSQL, mig.Version); err != nil {
		return fmt.Errorf("clear dirty flag: %w", err)
	}
	return nil
}

// Down rolls back the last n applied migrations in reverse version order.
// Serialized by the same advisory lock as Up.
func (m *Migrator) Down(ctx context.Context, n int) error {
	tbl, err := m.qtable()
	if err != nil {
		return err
	}
	return WithAdvisoryLock(ctx, m.db, m.dialect, func(conn *sql.Conn) error {
		return m.down(ctx, conn, tbl, n)
	})
}

func (m *Migrator) down(ctx context.Context, x connish, tbl string, n int) error {
	if err := m.createMigrationsTable(ctx, x, tbl); err != nil {
		return fmt.Errorf("creating migrations table: %w", err)
	}

	applied, err := m.appliedVersions(ctx, x, tbl)
	if err != nil {
		return fmt.Errorf("querying applied migrations: %w", err)
	}
	// A dirty database must be reconciled before any rollback too — running a
	// Down against a half-applied schema would compound the damage.
	for _, rec := range applied {
		if rec.Dirty {
			return fmt.Errorf("migration %d (%s): %w", rec.Version, rec.Name, ErrDirty)
		}
	}

	// Build sorted list of applied versions in descending order.
	var versions []uint64
	for v := range applied {
		versions = append(versions, v)
	}
	sort.Slice(versions, func(i, j int) bool {
		return versions[i] > versions[j]
	})

	if n > len(versions) {
		n = len(versions)
	}

	// Build lookup from version to Migration.
	lookup := make(map[uint64]Migration, len(m.migrations))
	for _, mig := range m.migrations {
		lookup[mig.Version] = mig
	}

	for i := 0; i < n; i++ {
		v := versions[i]
		mig, ok := lookup[v]
		if !ok {
			return fmt.Errorf("migration version %d is applied but not registered", v)
		}
		if err := m.runMigrationDown(ctx, x, tbl, mig); err != nil {
			return fmt.Errorf("rollback migration %d (%s): %w", mig.Version, mig.Name, err)
		}
	}

	return nil
}

// runMigrationDown executes a single migration's Down SQL and removes its
// tracking row. Transactional migrations do both atomically. No-transaction
// migrations (the Down counterpart of CREATE INDEX CONCURRENTLY etc.) run the
// Down outside any transaction — the same protocol as runMigrationUpNoTx — and
// mark the row dirty first so a failed concurrent-DDL rollback is detectable.
func (m *Migrator) runMigrationDown(ctx context.Context, x connish, tbl string, mig Migration) error {
	if mig.NoTransaction {
		return m.runMigrationDownNoTx(ctx, x, tbl, mig)
	}

	tx, err := x.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}

	if _, err := tx.ExecContext(ctx, mig.Down); err != nil {
		_ = tx.Rollback()
		return fmt.Errorf("exec down: %w", err)
	}

	deleteSQL := fmt.Sprintf("DELETE FROM %s WHERE version = %s", tbl, m.placeholder(1))
	if _, err := tx.ExecContext(ctx, deleteSQL, mig.Version); err != nil {
		_ = tx.Rollback()
		return fmt.Errorf("delete migration record: %w", err)
	}

	return tx.Commit()
}

// runMigrationDownNoTx rolls back a no-transaction migration. The row is marked
// dirty before the Down runs, so a failure mid-DDL (e.g. a DROP INDEX
// CONCURRENTLY that errors partway) leaves a dirty marker that blocks later
// runs until reconciled; success removes the row.
func (m *Migrator) runMigrationDownNoTx(ctx context.Context, x connish, tbl string, mig Migration) error {
	markSQL := fmt.Sprintf("UPDATE %s SET dirty = TRUE WHERE version = %s", tbl, m.placeholder(1))
	if _, err := x.ExecContext(ctx, markSQL, mig.Version); err != nil {
		return fmt.Errorf("mark dirty: %w", err)
	}
	if _, err := x.ExecContext(ctx, mig.Down); err != nil {
		// Leave the dirty row — the rollback half-applied and needs a human.
		return fmt.Errorf("exec down (no-transaction, left dirty): %w", err)
	}
	deleteSQL := fmt.Sprintf("DELETE FROM %s WHERE version = %s", tbl, m.placeholder(1))
	if _, err := x.ExecContext(ctx, deleteSQL, mig.Version); err != nil {
		return fmt.Errorf("delete migration record: %w", err)
	}
	return nil
}

// Force reconciles the tracking table by hand, the recovery path out of a
// dirty state and the way to adopt an existing database (baseline).
//
//   - applied == true: mark `version` as cleanly applied without running its
//     Up SQL. Inserts the row if missing (baseline), and clears any dirty flag
//     on it. If the version is registered, its name and checksum are recorded
//     so future drift checks line up.
//   - applied == false: remove `version` from the tracking table entirely, so
//     it is treated as pending again (e.g. a no-transaction migration that did
//     not actually take effect).
//
// Either way the dirty state for that version is cleared, unblocking Up/Down.
func (m *Migrator) Force(ctx context.Context, version uint64, applied bool) error {
	tbl, err := m.qtable()
	if err != nil {
		return err
	}
	return WithAdvisoryLock(ctx, m.db, m.dialect, func(conn *sql.Conn) error {
		if err := m.createMigrationsTable(ctx, conn, tbl); err != nil {
			return fmt.Errorf("creating migrations table: %w", err)
		}

		if !applied {
			del := fmt.Sprintf("DELETE FROM %s WHERE version = %s", tbl, m.placeholder(1))
			_, err := conn.ExecContext(ctx, del, version)
			return err
		}

		name, checksum := "forced", ""
		for _, mig := range m.migrations {
			if mig.Version == version {
				name, checksum = mig.Name, checksumOf(mig)
				break
			}
		}
		// Upsert a clean row. ON CONFLICT … DO UPDATE works on Postgres and
		// SQLite ≥3.24; it clears the dirty flag whether the row exists or not.
		upsert := fmt.Sprintf(
			"INSERT INTO %s (version, name, applied_at, checksum, dirty) VALUES (%s, %s, %s, %s, FALSE) "+
				"ON CONFLICT (version) DO UPDATE SET dirty = FALSE",
			tbl, m.placeholder(1), m.placeholder(2), m.placeholder(3), m.placeholder(4),
		)
		_, err = conn.ExecContext(ctx, upsert, version, name, time.Now().UTC(), checksum)
		return err
	})
}

// Status returns the current migration state: which are applied and which are pending.
func (m *Migrator) Status(ctx context.Context) (*Status, error) {
	tbl, err := m.qtable()
	if err != nil {
		return nil, err
	}
	if err := m.createMigrationsTable(ctx, m.db, tbl); err != nil {
		return nil, fmt.Errorf("creating migrations table: %w", err)
	}

	applied, err := m.appliedVersions(ctx, m.db, tbl)
	if err != nil {
		return nil, fmt.Errorf("querying applied migrations: %w", err)
	}

	// Build applied list sorted by version.
	var appliedList []MigrationRecord
	for _, rec := range applied {
		appliedList = append(appliedList, rec)
	}
	sort.Slice(appliedList, func(i, j int) bool {
		return appliedList[i].Version < appliedList[j].Version
	})

	pending := pendingMigrations(m.migrations, applied)

	return &Status{
		Applied: appliedList,
		Pending: pending,
	}, nil
}

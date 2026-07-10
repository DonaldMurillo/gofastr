package migrate

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"

	"github.com/DonaldMurillo/gofastr/core/query"
)

// ErrDirty is returned by Up and Down when the tracking table records a
// migration left in a dirty state by a previously failed no-transaction
// migration. The database may be partially migrated; an operator must
// reconcile it by hand and then call Force to clear the state before
// migrations can proceed again.
var ErrDirty = errors.New("migrate: database is in a dirty state from a failed migration — reconcile manually and call Force")

// ChecksumMismatchError is returned by Up when an already-applied migration's
// recorded checksum no longer matches the registered migration's Up SQL. That
// means the migration file was edited after it was applied — a drift the
// runner refuses to paper over, because the live schema no longer matches what
// the (edited) file says it should be.
type ChecksumMismatchError struct {
	Version  uint64
	Name     string
	Recorded string
	Current  string
}

func (e *ChecksumMismatchError) Error() string {
	return fmt.Sprintf(
		"migrate: migration %d (%s) was modified after it was applied (recorded checksum %s, now %s) — applied migrations are immutable; revert the file or roll back and re-apply",
		e.Version, e.Name, short(e.Recorded), short(e.Current))
}

func short(h string) string {
	if len(h) > 12 {
		return h[:12]
	}
	return h
}

// checksumOf returns the SHA-256 (hex) of a migration's Up SQL. It is the
// content fingerprint stored in the tracking table at apply time and compared
// on every subsequent run to detect post-apply edits.
func checksumOf(mig Migration) string {
	sum := sha256.Sum256([]byte(mig.Up))
	return hex.EncodeToString(sum[:])
}

// ensureTrackingColumns backfills the checksum and dirty columns onto a
// pre-existing _migrations table (created before those columns existed). New
// tables already include them via CreateMigrationsTable, so this is a no-op
// there. Idempotent on both engines:
//
//   - Postgres uses ADD COLUMN IF NOT EXISTS.
//   - SQLite has no IF NOT EXISTS for ADD COLUMN, so a duplicate-column error
//     is treated as success.
func (m *Migrator) ensureTrackingColumns(ctx context.Context, x connish, tbl string, ga bool) error {
	adds := []struct{ col, def string }{
		{"checksum", "TEXT NOT NULL DEFAULT ''"},
		{"dirty", "BOOLEAN NOT NULL DEFAULT FALSE"},
	}
	if ga {
		adds = append(adds, struct{ col, def string }{"group_name", "TEXT NOT NULL DEFAULT ''"})
	}
	for _, a := range adds {
		var stmt string
		if m.dialect == DialectPostgres {
			stmt = fmt.Sprintf("ALTER TABLE %s ADD COLUMN IF NOT EXISTS %s %s", tbl, a.col, a.def)
		} else {
			stmt = fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s %s", tbl, a.col, a.def)
		}
		if _, err := x.ExecContext(ctx, stmt); err != nil {
			if m.dialect != DialectPostgres && isDuplicateColumn(err) {
				continue // column already present on this older SQLite table
			}
			return fmt.Errorf("migrate: backfill %s column: %w", a.col, err)
		}
	}
	return nil
}

// isDuplicateColumn reports whether err is SQLite's "duplicate column name"
// error from an ADD COLUMN that already exists.
func isDuplicateColumn(err error) bool {
	return err != nil && strings.Contains(strings.ToLower(err.Error()), "duplicate column")
}

// ---- composite-key upgrade for group-aware mode ----

// ensureCompositeKey upgrades a legacy single-column (version) primary key to
// the composite (group_name, version) key the group-aware path requires. It is
// a no-op when the key is already composite (a fresh group-aware table).
//
// The upgrade is always safe for a legacy table: every existing row is
// default-group (group_name=”) with a unique version — the old single-column
// PK guaranteed that — so no existing row can violate the new composite key.
// A cross-group version collision is only possible on a FUTURE insert, which
// the composite key then correctly admits (different group_name).
func (m *Migrator) ensureCompositeKey(ctx context.Context, x connish, tbl string) error {
	cols, err := m.primaryKeyColumns(ctx, x, tbl)
	if err != nil {
		return err
	}
	if isGroupVersionKey(cols) {
		return nil // already composite
	}
	if m.dialect == DialectPostgres {
		return m.upgradePKPostgres(ctx, x, tbl)
	}
	return m.rebuildTableSQLite(ctx, x, tbl)
}

// isGroupVersionKey reports whether cols is exactly [group_name, version].
func isGroupVersionKey(cols []string) bool {
	return len(cols) == 2 && cols[0] == "group_name" && cols[1] == "version"
}

// primaryKeyColumns returns the ordered column names of the tracking table's
// primary key, used to decide whether a legacy single-column (version) key
// needs upgrading.
func (m *Migrator) primaryKeyColumns(ctx context.Context, x connish, tbl string) ([]string, error) {
	var rows *sql.Rows
	var err error
	if m.dialect == DialectPostgres {
		// Pass the quoted identifier string (tbl) to $1::regclass — not the
		// raw m.tableName. regclass folds an unquoted string to lowercase,
		// which breaks mixed-case (or dotted) WithTableName values. The
		// quoted form ('"MyMigrations"'::regclass) preserves case.
		q := `SELECT a.attname FROM pg_index i ` +
			`JOIN pg_attribute a ON a.attrelid = i.indrelid AND a.attnum = ANY(i.indkey) ` +
			`WHERE i.indrelid = ` + m.placeholder(1) + `::regclass AND i.indisprimary ` +
			`ORDER BY array_position(i.indkey, a.attnum)`
		rows, err = x.QueryContext(ctx, q, tbl)
	} else {
		q := fmt.Sprintf("SELECT name FROM pragma_table_info(%s) WHERE pk > 0 ORDER BY pk", m.placeholder(1))
		rows, err = x.QueryContext(ctx, q, m.tableName)
	}
	if err != nil {
		return nil, fmt.Errorf("migrate: read primary-key columns: %w", err)
	}
	defer rows.Close()
	var cols []string
	for rows.Next() {
		var c string
		if err := rows.Scan(&c); err != nil {
			return nil, err
		}
		cols = append(cols, c)
	}
	return cols, rows.Err()
}
func (m *Migrator) upgradePKPostgres(ctx context.Context, x connish, tbl string) error {
	var cname string
	// Pass the quoted identifier (tbl) — see primaryKeyColumns for rationale.
	q := `SELECT conname FROM pg_constraint WHERE conrelid = ` + m.placeholder(1) + `::regclass AND contype = 'p'`
	if err := x.QueryRowContext(ctx, q, tbl).Scan(&cname); err != nil {
		if err == sql.ErrNoRows {
			return nil // no PK at all — nothing to upgrade
		}
		return fmt.Errorf("migrate: find primary-key constraint: %w", err)
	}
	safe, err := query.SafeIdent(cname)
	if err != nil {
		return fmt.Errorf("migrate: primary-key constraint name %q: %w", cname, err)
	}
	stmt := fmt.Sprintf("ALTER TABLE %s DROP CONSTRAINT %s, ADD PRIMARY KEY (group_name, version)",
		tbl, query.QuoteIdent(safe))
	if _, err := x.ExecContext(ctx, stmt); err != nil {
		return fmt.Errorf("migrate: upgrade primary key to (group_name, version): %w", err)
	}
	return nil
}

// rebuildTableSQLite upgrades the key by creating a new composite-PK table,
// copying the rows, and renaming — all inside one transaction so a failure
// rolls back atomically. SQLite cannot ALTER a primary key in place.
func (m *Migrator) rebuildTableSQLite(ctx context.Context, x connish, tbl string) error {
	baseName := strings.Trim(tbl, `"`)
	tmpSafe, err := query.SafeIdent(baseName + "__grp_upg")
	if err != nil {
		return fmt.Errorf("migrate: temp table name: %w", err)
	}
	tmpTbl := query.QuoteIdent(tmpSafe)

	tx, err := x.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("migrate: begin key-upgrade tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	createNew := fmt.Sprintf(`CREATE TABLE %s (
		group_name TEXT    NOT NULL DEFAULT '',
		version    BIGINT  NOT NULL,
		name       TEXT    NOT NULL DEFAULT '',
		applied_at TIMESTAMP NOT NULL DEFAULT %s,
		checksum   TEXT    NOT NULL DEFAULT '',
		dirty      BOOLEAN NOT NULL DEFAULT FALSE,
		PRIMARY KEY (group_name, version)
	)`, tmpTbl, m.nowFunc())
	if _, err := tx.ExecContext(ctx, createNew); err != nil {
		return fmt.Errorf("migrate: create temp tracking table: %w", err)
	}

	copyData := fmt.Sprintf(
		"INSERT INTO %s (group_name, version, name, applied_at, checksum, dirty) "+
			"SELECT group_name, version, name, applied_at, checksum, dirty FROM %s", tmpTbl, tbl)
	if _, err := tx.ExecContext(ctx, copyData); err != nil {
		return fmt.Errorf("migrate: copy tracking rows: %w", err)
	}

	if _, err := tx.ExecContext(ctx, fmt.Sprintf("DROP TABLE %s", tbl)); err != nil {
		return fmt.Errorf("migrate: drop legacy tracking table: %w", err)
	}

	if _, err := tx.ExecContext(ctx, fmt.Sprintf("ALTER TABLE %s RENAME TO %s", tmpTbl, tbl)); err != nil {
		return fmt.Errorf("migrate: rename tracking table: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("migrate: commit key upgrade: %w", err)
	}
	return nil
}

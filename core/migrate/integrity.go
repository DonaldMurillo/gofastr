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

// ErrDirty means a NoTransaction migration was left half-applied; reconcile
// it and call Force.
var ErrDirty = errors.New("migrate: database is in a dirty state from a failed migration — reconcile manually and call Force")

// ErrNoDown means Down would roll back a migration that has no Down SQL.
var ErrNoDown = errors.New("migrate: the migration has no Down SQL, so nothing undoes its Up — write a Down section, or undo the change by hand and call Force")

// ChecksumMismatchError means an applied migration's Up was edited.
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

func checksumOf(mig Migration) string {
	sum := sha256.Sum256([]byte(mig.Up))
	return hex.EncodeToString(sum[:])
}

func (m *Migrator) ensureTrackingColumns(ctx context.Context, x connish, tbl string, ga bool) error {
	adds := []struct{ col, def string }{
		{"checksum", "TEXT NOT NULL DEFAULT ''"},
		{"dirty", "BOOLEAN NOT NULL DEFAULT FALSE"},
	}
	if ga {
		adds = append(adds, struct{ col, def string }{"group_name", "TEXT NOT NULL DEFAULT ''"})
	}
	for _, a := range adds {
		col := query.QuoteFolded(a.col)
		var stmt string
		if m.dialect == DialectPostgres {
			stmt = fmt.Sprintf("ALTER TABLE %s ADD COLUMN IF NOT EXISTS %s %s", tbl, col, a.def)
		} else {
			stmt = fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s %s", tbl, col, a.def)
		}
		if _, err := x.ExecContext(ctx, stmt); err != nil {
			if m.dialect != DialectPostgres && isDuplicateColumn(err) {
				continue
			}
			return fmt.Errorf("migrate: backfill %s column: %w", a.col, err)
		}
	}
	return nil
}

func isDuplicateColumn(err error) bool {
	return err != nil && strings.Contains(strings.ToLower(err.Error()), "duplicate column")
}

func (m *Migrator) ensureCompositeKey(ctx context.Context, x connish, tbl string) error {
	cols, err := m.primaryKeyColumns(ctx, x, tbl)
	if err != nil {
		return err
	}
	if isGroupVersionKey(cols) {
		return nil
	}
	if m.dialect == DialectPostgres {
		return m.upgradePKPostgres(ctx, x, tbl)
	}
	return m.rebuildTableSQLite(ctx, x, tbl)
}

func isGroupVersionKey(cols []string) bool {
	return len(cols) == 2 && cols[0] == "group_name" && cols[1] == "version"
}

func (m *Migrator) primaryKeyColumns(ctx context.Context, x connish, tbl string) ([]string, error) {
	var rows *sql.Rows
	var err error
	if m.dialect == DialectPostgres {
		q := `SELECT a.attname FROM pg_index i ` +
			`JOIN pg_attribute a ON a.attrelid = i.indrelid AND a.attnum = ANY(i.indkey) ` +
			`WHERE i.indrelid = ` + m.placeholder(1) + `::regclass AND i.indisprimary ` +
			`ORDER BY array_position(i.indkey, a.attnum)`
		rows, err = x.QueryContext(ctx, q, tbl)
	} else {
		info, args := m.sqliteTableInfo()
		rows, err = x.QueryContext(ctx, "SELECT name FROM "+info+" WHERE pk > 0 ORDER BY pk", args...)
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
	q := `SELECT conname FROM pg_constraint WHERE conrelid = ` + m.placeholder(1) + `::regclass AND contype = 'p'`
	if err := x.QueryRowContext(ctx, q, tbl).Scan(&cname); err != nil {
		if err == sql.ErrNoRows {
			return nil
		}
		return fmt.Errorf("migrate: find primary-key constraint: %w", err)
	}
	safe, err := query.SafeIdent(cname)
	if err != nil {
		return fmt.Errorf("migrate: primary-key constraint name %q: %w", cname, err)
	}
	stmt := fmt.Sprintf("ALTER TABLE %s DROP CONSTRAINT %s, ADD PRIMARY KEY (group_name, version)",
		//gofastr:allow(quoteident) cname was read from pg_constraint with its case kept
		tbl, query.QuoteIdent(safe))
	if _, err := x.ExecContext(ctx, stmt); err != nil {
		return fmt.Errorf("migrate: upgrade primary key to (group_name, version): %w", err)
	}
	return nil
}

func (m *Migrator) rebuildTableSQLite(ctx context.Context, x connish, tbl string) error {
	schema, base := m.sqliteTableParts()
	tmpName := base + "__grp_upg"
	if schema != "" {
		tmpName = schema + "." + tmpName
	}
	tmpSafe, err := query.SafeIdent(tmpName)
	if err != nil {
		return fmt.Errorf("migrate: temp table name: %w", err)
	}
	tmpTbl := query.QuoteFolded(tmpSafe)
	baseTbl := query.QuoteFolded(base)

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

	if _, err := tx.ExecContext(ctx, fmt.Sprintf("ALTER TABLE %s RENAME TO %s", tmpTbl, baseTbl)); err != nil {
		return fmt.Errorf("migrate: rename tracking table: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("migrate: commit key upgrade: %w", err)
	}
	return nil
}

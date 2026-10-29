package db

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"strings"

	"github.com/DonaldMurillo/gofastr/core/query"
	"github.com/DonaldMurillo/gofastr/framework"
)

// ErrTableRestored marks a RebuildTable failure that put the table back
// as it was, rows and indexes included: the database still holds what
// it held before the edit.
var ErrTableRestored = errors.New("kiln/db: the table was put back as it was")

// DropColumns removes columns an edit took off an entity. Migrate only
// adds, so a dropped field's column stayed, and a NOT NULL one refused
// every later insert. SQLite's DROP COLUMN refuses a UNIQUE, key,
// indexed or foreign-key column; the first refusal rebuilds the table
// from the registry instead (RebuildTable), which takes every dropped
// column at once. The drops run in one transaction that rolls back
// before a rebuild, so a rebuild that puts the table back puts back
// every column. A column the table does not have is skipped.
func DropColumns(d *sql.DB, registry *framework.Registry, table string, cols []string) error {
	t, err := query.SafeIdent(table)
	if err != nil {
		return fmt.Errorf("kiln/db: drop columns: %w", err)
	}
	ctx := context.Background()
	live, err := orderedColumns(ctx, d, t)
	if err != nil {
		return err
	}
	tx, err := d.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("kiln/db: drop columns: %w", err)
	}
	defer tx.Rollback()
	for _, c := range cols {
		i := indexFold(live, c)
		if i < 0 {
			continue
		}
		if _, err := tx.ExecContext(ctx, "ALTER TABLE "+query.QuoteFolded(t)+" DROP COLUMN "+query.QuoteFolded(live[i])); err != nil {
			// Released before the rebuild, which needs a connection of
			// its own from a pool that may hold one.
			_ = tx.Rollback()
			return RebuildTable(d, registry, t)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("kiln/db: drop columns: %w", err)
	}
	return nil
}

// RebuildTable recreates table from the registry and copies its rows
// across. SQLite has no ALTER that drops a NOT NULL or a UNIQUE, so an
// edit that relaxes a column, or drops one DROP COLUMN refuses, needs
// the table rebuilt. The old table is renamed aside and its indexes
// dropped, Migrate creates the declared table with its indexes, and the
// columns both share copy over in one transaction before the old table
// goes. Any failure after the rename puts the old table back under its
// name with its indexes, and the error wraps ErrTableRestored. Rows the
// declared table refuses (a NULL in a NOT NULL column, a duplicate under
// UNIQUE) fail it that way.
func RebuildTable(d *sql.DB, registry *framework.Registry, table string) error {
	t, err := query.SafeIdent(table)
	if err != nil {
		return fmt.Errorf("kiln/db: rebuild: %w", err)
	}
	// The set-aside name is built by prefixing; a schema-qualified name
	// would put the prefix on the schema.
	if strings.Contains(t, ".") {
		return fmt.Errorf("kiln/db: rebuild %s: a schema-qualified table is not rebuilt", t)
	}
	aside := "_kiln_rebuild_" + t
	ctx := context.Background()
	var indexes []string
	moved := false
	err = onAlterConn(ctx, d, func(conn *sql.Conn) error {
		names, sqls, err := explicitIndexes(ctx, conn, t)
		if err != nil {
			return err
		}
		indexes = sqls
		if _, err := conn.ExecContext(ctx, "ALTER TABLE "+query.QuoteFolded(t)+" RENAME TO "+query.QuoteFolded(aside)); err != nil {
			return fmt.Errorf("kiln/db: rebuild %s: set aside: %w", t, err)
		}
		moved = true
		// The indexes went with the renamed table under their own names,
		// and Migrate's CREATE INDEX IF NOT EXISTS would find them there.
		for _, n := range names {
			if _, err := conn.ExecContext(ctx, "DROP INDEX "+query.QuoteFolded(n)); err != nil {
				return fmt.Errorf("kiln/db: rebuild %s: drop index %s: %w", t, n, err)
			}
		}
		return nil
	})
	if err == nil {
		if err = Migrate(d, registry); err != nil {
			err = fmt.Errorf("kiln/db: rebuild %s: create: %w", t, err)
		}
	}
	if err == nil {
		err = onAlterConn(ctx, d, func(conn *sql.Conn) error { return copyAcross(ctx, conn, t, aside) })
	}
	if err == nil || !moved {
		return err
	}
	if perr := onAlterConn(ctx, d, func(conn *sql.Conn) error { return putBack(ctx, conn, t, aside, indexes) }); perr != nil {
		return errors.Join(err, perr)
	}
	return errors.Join(err, ErrTableRestored)
}

// copyAcross moves the rows of aside into the rebuilt table and drops
// aside, in one transaction.
func copyAcross(ctx context.Context, conn *sql.Conn, t, aside string) error {
	newCols, err := orderedColumns(ctx, conn, t)
	if err != nil {
		return err
	}
	oldCols, err := orderedColumns(ctx, conn, aside)
	if err != nil {
		return err
	}
	var shared []string
	for _, c := range newCols {
		if indexFold(oldCols, c) >= 0 {
			shared = append(shared, query.QuoteFolded(c))
		}
	}
	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("kiln/db: rebuild %s: %w", t, err)
	}
	defer tx.Rollback()
	list := strings.Join(shared, ", ")
	if _, err := tx.ExecContext(ctx, "INSERT INTO "+query.QuoteFolded(t)+" ("+list+") SELECT "+list+" FROM "+query.QuoteFolded(aside)); err != nil {
		return fmt.Errorf("kiln/db: rebuild %s: copy rows: %w", t, err)
	}
	if _, err := tx.ExecContext(ctx, "DROP TABLE "+query.QuoteFolded(aside)); err != nil {
		return fmt.Errorf("kiln/db: rebuild %s: drop the old table: %w", t, err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("kiln/db: rebuild %s: %w", t, err)
	}
	return nil
}

// putBack undoes a rebuild that failed after the rename: the declared
// table, if Migrate made it, is dropped, the old one takes its name
// back, and its indexes are recreated from the SQL SQLite stored.
func putBack(ctx context.Context, conn *sql.Conn, t, aside string, indexes []string) error {
	if _, err := conn.ExecContext(ctx, "DROP TABLE IF EXISTS "+query.QuoteFolded(t)); err != nil {
		return fmt.Errorf("kiln/db: put %s back: %w", t, err)
	}
	if _, err := conn.ExecContext(ctx, "ALTER TABLE "+query.QuoteFolded(aside)+" RENAME TO "+query.QuoteFolded(t)); err != nil {
		return fmt.Errorf("kiln/db: put %s back: %w", t, err)
	}
	for _, s := range indexes {
		if _, err := conn.ExecContext(ctx, s); err != nil {
			return fmt.Errorf("kiln/db: put %s back: index: %w", t, err)
		}
	}
	return nil
}

// explicitIndexes lists the CREATE INDEX indexes on t, names and SQL.
// The indexes SQLite makes for a UNIQUE or PRIMARY KEY have no SQL and
// stay with their table.
func explicitIndexes(ctx context.Context, conn *sql.Conn, t string) (names, sqls []string, err error) {
	rows, err := conn.QueryContext(ctx, `SELECT name, sql FROM sqlite_master WHERE type = 'index' AND lower(tbl_name) = lower(?) AND sql IS NOT NULL ORDER BY name`, t)
	if err != nil {
		return nil, nil, fmt.Errorf("kiln/db: list indexes of %s: %w", t, err)
	}
	defer rows.Close()
	for rows.Next() {
		var n, s string
		if err := rows.Scan(&n, &s); err != nil {
			return nil, nil, fmt.Errorf("kiln/db: list indexes of %s: %w", t, err)
		}
		names, sqls = append(names, n), append(sqls, s)
	}
	if err := rows.Err(); err != nil {
		return nil, nil, fmt.Errorf("kiln/db: list indexes of %s: %w", t, err)
	}
	return names, sqls, nil
}

type queryer interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
}

// orderedColumns lists t's columns in table order.
func orderedColumns(ctx context.Context, q queryer, t string) ([]string, error) {
	rows, err := q.QueryContext(ctx, `SELECT name FROM pragma_table_info(?) ORDER BY cid`, t)
	if err != nil {
		return nil, fmt.Errorf("kiln/db: columns of %s: %w", t, err)
	}
	defer rows.Close()
	var cols []string
	for rows.Next() {
		var c string
		if err := rows.Scan(&c); err != nil {
			return nil, fmt.Errorf("kiln/db: columns of %s: %w", t, err)
		}
		cols = append(cols, c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("kiln/db: columns of %s: %w", t, err)
	}
	return cols, nil
}

// indexFold finds name in cols the way SQLite matches identifiers,
// without ASCII case.
func indexFold(cols []string, name string) int {
	for i, c := range cols {
		if strings.EqualFold(c, name) {
			return i
		}
	}
	return -1
}

// alterPragmas are the settings a rebuild runs under. Foreign keys off,
// so moving and dropping a table checks no rows and runs no ON DELETE
// action. legacy_alter_table on, so renaming the old table aside leaves
// the REFERENCES clauses of the tables pointing at it alone: with it off,
// SQLite rewrites them to the new name, and they would follow the old
// table out.
var alterPragmas = []struct {
	name string
	set  int
}{{"foreign_keys", 0}, {"legacy_alter_table", 1}}

// onAlterConn runs fn on one connection under alterPragmas, then sets
// each pragma back and reads it back. A connection whose settings
// cannot be confirmed is discarded, never returned to the pool.
func onAlterConn(ctx context.Context, d *sql.DB, fn func(*sql.Conn) error) error {
	conn, err := d.Conn(ctx)
	if err != nil {
		return fmt.Errorf("kiln/db: rebuild: %w", err)
	}
	defer conn.Close()
	was := make([]int, len(alterPragmas))
	for i, p := range alterPragmas {
		if err := conn.QueryRowContext(ctx, "PRAGMA "+p.name).Scan(&was[i]); err != nil {
			return fmt.Errorf("kiln/db: read %s: %w", p.name, err)
		}
	}
	var fnErr error
	for _, p := range alterPragmas {
		if _, err := conn.ExecContext(ctx, fmt.Sprintf("PRAGMA %s = %d", p.name, p.set)); err != nil {
			fnErr = fmt.Errorf("kiln/db: set %s: %w", p.name, err)
			break
		}
	}
	if fnErr == nil {
		fnErr = fn(conn)
	}
	for i, p := range alterPragmas {
		if err := restorePragma(ctx, conn, p.name, was[i]); err != nil {
			_ = conn.Raw(func(any) error { return driver.ErrBadConn })
			return errors.Join(fnErr, err)
		}
	}
	return fnErr
}

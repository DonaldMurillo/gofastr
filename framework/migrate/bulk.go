package migrate

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/DonaldMurillo/gofastr/core/query"
)

// ReadLiveColumnsBulk reads column metadata for multiple tables in a
// single query. Returns a map of table name → {column name → type}.
//
// This replaces N individual ReadLiveColumns calls with one bulk
// WHERE table_name IN (...) query, reducing SchemaDiff from ~59ms
// to ~5ms for 50 entities on Postgres.
func ReadLiveColumnsBulk(ctx context.Context, db *sql.DB, tables []string, dialect Dialect) (map[string]map[string]string, error) {
	if len(tables) == 0 {
		return map[string]map[string]string{}, nil
	}
	if dialect == DialectPostgres {
		return readLiveColumnsBulkPostgres(ctx, db, tables)
	}
	return readLiveColumnsBulkSQLite(ctx, db, tables)
}

func readLiveColumnsBulkPostgres(ctx context.Context, db *sql.DB, tables []string) (map[string]map[string]string, error) {
	// AutoMigrate emits CREATE TABLE with UNQUOTED identifiers, so Postgres
	// folds a mixed-case table name to lowercase in information_schema.
	// Match the folded schema and name exactly and key the result by the
	// ORIGINAL requested name so the caller's result[ent.GetTable()] lookup
	// hits, same convention as TableExistsBulk. Without the fold, a table
	// like "MixedAccount" reads as "doesn't exist" on every boot; matching
	// without case instead read a hand-made quoted "MixedAccount" as the
	// declared table, which the framework's unquoted DDL never names.
	// A qualified app.widgets is widgets in schema app: compared whole
	// against the bare table_name, it read as absent and boot skipped
	// every column added later.
	//
	// The same holds for columns: one the framework adds is folded, so a
	// quoted legacy "Title" is not the declared Title (whose DDL names
	// title), and reading it as one skipped the ADD COLUMN.
	refs, args := pgTableRefs(tables)
	query := fmt.Sprintf(`
		SELECT t.i, c.column_name, %s
		FROM %s
		JOIN information_schema.columns c
			ON c.table_schema = coalesce(t.s, current_schema()) AND c.table_name = t.n
		WHERE c.column_name = lower(c.column_name)
		ORDER BY t.i, c.ordinal_position
	`, pgColumnType("c."), refs)

	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("bulk column read: %w", err)
	}
	defer rows.Close()

	result := make(map[string]map[string]string, len(tables))
	for rows.Next() {
		var i int
		var colName, colType string
		if err := rows.Scan(&i, &colName, &colType); err != nil {
			return nil, err
		}
		table := tables[i]
		if result[table] == nil {
			result[table] = make(map[string]string)
		}
		result[table][colName] = colType
	}
	return result, rows.Err()
}

func readLiveColumnsBulkSQLite(ctx context.Context, db *sql.DB, tables []string) (map[string]map[string]string, error) {
	result := make(map[string]map[string]string, len(tables))
	for _, table := range tables {
		cols, err := ReadLiveColumnsSQLite(ctx, db, table)
		if err != nil {
			return nil, fmt.Errorf("bulk read %s: %w", table, err)
		}
		result[table] = cols
	}
	return result, nil
}

// TableExistsBulk checks which tables exist in a single query.
// Returns a set of existing table names.
func TableExistsBulk(ctx context.Context, db *sql.DB, tables []string, dialect Dialect) (map[string]bool, error) {
	if len(tables) == 0 {
		return map[string]bool{}, nil
	}

	if dialect == DialectPostgres {
		return tableExistsBulkPostgres(ctx, db, tables)
	}
	return tableExistsBulkSQLite(ctx, db, tables)
}

func tableExistsBulkPostgres(ctx context.Context, db *sql.DB, tables []string) (map[string]bool, error) {
	// AutoMigrate emits CREATE TABLE with UNQUOTED identifiers, so Postgres
	// folds a mixed-case table name to lowercase in pg_tables. Compare the
	// folded schema and name exactly (a quoted "MixedAccount" is some other
	// table; a qualified app.widgets is widgets in schema app) and key the
	// result by the ORIGINAL requested name so the caller's
	// existing[ent.GetTable()] lookup hits. Without this, "MixedAccount"
	// never matches the folded "mixedaccount" and AutoMigrate re-attempts
	// CREATE TABLE on every boot.
	refs, args := pgTableRefs(tables)
	query := fmt.Sprintf(`
		SELECT t.i FROM %s
		JOIN pg_tables p ON p.schemaname = coalesce(t.s, current_schema()) AND p.tablename = t.n
	`, refs)

	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	existing := make(map[string]bool)
	for rows.Next() {
		var i int
		if err := rows.Scan(&i); err != nil {
			return nil, err
		}
		existing[tables[i]] = true
	}
	return existing, rows.Err()
}

func tableExistsBulkSQLite(ctx context.Context, db *sql.DB, tables []string) (map[string]bool, error) {
	existing := make(map[string]bool)
	for _, t := range tables {
		if _, err := query.SafeIdent(t); err != nil {
			return nil, err
		}
		// SQLite resolves a table name without case, so Tasks names an
		// existing tasks; sqlite_master's name compares exactly unless told
		// otherwise. A qualified main.tasks is tasks in main's catalog.
		var count int
		err := db.QueryRowContext(ctx,
			"SELECT COUNT(*) FROM "+sqliteMaster(t)+" WHERE type='table' AND name = ? COLLATE NOCASE", tableBase(t)).Scan(&count)
		if err != nil {
			return nil, err
		}
		existing[t] = count > 0
	}
	return existing, nil
}

// pgColumnType is the information_schema expression a live Postgres
// column's type is read with: data_type, plus the width a character type
// declares ("character varying(255)"). data_type alone names no width, so
// a String whose Max changed read as in sync. p prefixes the columns
// ("c." in a join, "" alone).
func pgColumnType(p string) string {
	return fmt.Sprintf("CASE WHEN %[1]scharacter_maximum_length IS NULL THEN %[1]sdata_type "+
		"ELSE %[1]sdata_type || '(' || %[1]scharacter_maximum_length || ')' END", p)
}

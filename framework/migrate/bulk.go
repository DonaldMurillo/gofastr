package migrate

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/DonaldMurillo/gofastr/core/query"
)

// ReadLiveColumnsBulk reads the columns of several tables, one query on
// Postgres: table name as requested → column name → the type
// ReadLiveColumns reports. A table that does not exist is absent (Postgres)
// or empty (SQLite).
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

// TableExistsBulk reports which of tables exist, keyed by the name as
// requested; a missing table maps to false or is absent.
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

func pgColumnType(p string) string {
	return fmt.Sprintf("CASE WHEN %[1]scharacter_maximum_length IS NULL THEN %[1]sdata_type "+
		"ELSE %[1]sdata_type || '(' || %[1]scharacter_maximum_length || ')' END", p)
}

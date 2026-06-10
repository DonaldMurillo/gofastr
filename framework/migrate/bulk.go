package migrate

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
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
	// folds a mixed-case table name to lowercase in information_schema. Match
	// case-insensitively and key the result by the ORIGINAL requested name so
	// the caller's result[ent.GetTable()] lookup hits — same convention as
	// TableExistsBulk. Without this, a table like "MixedAccount" reads as
	// "doesn't exist" on every boot.
	placeholders := make([]string, len(tables))
	args := make([]any, len(tables))
	byLower := make(map[string]string, len(tables))
	for i, t := range tables {
		placeholders[i] = fmt.Sprintf("$%d", i+1)
		args[i] = strings.ToLower(t)
		byLower[strings.ToLower(t)] = t
	}

	query := fmt.Sprintf(`
		SELECT table_name, column_name, data_type
		FROM information_schema.columns
		WHERE table_schema = current_schema() AND lower(table_name) IN (%s)
		ORDER BY table_name, ordinal_position
	`, strings.Join(placeholders, ", "))

	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("bulk column read: %w", err)
	}
	defer rows.Close()

	result := make(map[string]map[string]string, len(tables))
	for rows.Next() {
		var tableName, colName, colType string
		if err := rows.Scan(&tableName, &colName, &colType); err != nil {
			return nil, err
		}
		if orig, ok := byLower[strings.ToLower(tableName)]; ok {
			tableName = orig
		}
		if result[tableName] == nil {
			result[tableName] = make(map[string]string)
		}
		result[tableName][colName] = colType
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
	// requested names case-insensitively (lowercased $N args matched against
	// pg_tables.tablename) and key the result by the ORIGINAL requested name
	// so the caller's existing[ent.GetTable()] lookup hits. Without this,
	// "MixedAccount" never matches the folded "mixedaccount" and AutoMigrate
	// re-attempts CREATE TABLE on every boot.
	placeholders := make([]string, len(tables))
	args := make([]any, len(tables))
	byLower := make(map[string]string, len(tables))
	for i, t := range tables {
		placeholders[i] = fmt.Sprintf("$%d", i+1)
		args[i] = strings.ToLower(t)
		byLower[strings.ToLower(t)] = t
	}

	query := fmt.Sprintf(`
		SELECT tablename FROM pg_tables
		WHERE schemaname = current_schema() AND lower(tablename) IN (%s)
	`, strings.Join(placeholders, ", "))

	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	existing := make(map[string]bool)
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		if orig, ok := byLower[strings.ToLower(name)]; ok {
			existing[orig] = true
		} else {
			existing[name] = true
		}
	}
	return existing, rows.Err()
}

func tableExistsBulkSQLite(ctx context.Context, db *sql.DB, tables []string) (map[string]bool, error) {
	existing := make(map[string]bool)
	for _, t := range tables {
		var count int
		err := db.QueryRowContext(ctx,
			"SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name=?", t).Scan(&count)
		if err != nil {
			return nil, err
		}
		existing[t] = count > 0
	}
	return existing, nil
}

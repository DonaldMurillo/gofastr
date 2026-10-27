package query

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
)

// RowQueryer is the slice of *sql.DB, *sql.Conn and *sql.Tx that
// RefuseCaseKeptTables needs.
type RowQueryer interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// RefuseCaseKeptTables stops a Postgres store from booting past a table an
// older build made. Those builds quoted a configured name whole with
// QuoteIdent, so "Jobs" kept its case and "app.Jobs" became one table
// literally named app.Jobs. Stores now quote with QuoteFolded and name
// jobs, or jobs in schema app; booting on would start an empty table and
// leave every row in the old one. The error carries the statements that
// move it. A name QuoteIdent and QuoteFolded spell alike is skipped, so a
// lowercase table costs no query. Call it on Postgres only: SQLite
// compares names without case. tables are SafeIdent-validated names.
func RefuseCaseKeptTables(ctx context.Context, db RowQueryer, tables ...string) error {
	for _, table := range tables {
		if _, err := SafeIdent(table); err != nil {
			return err
		}
		kept, folded := QuoteIdent(table), QuoteFolded(table)
		if kept == folded {
			continue
		}
		var hasKept, hasFolded bool
		if err := db.QueryRowContext(ctx, `SELECT to_regclass($1) IS NOT NULL, to_regclass($2) IS NOT NULL`,
			kept, folded).Scan(&hasKept, &hasFolded); err != nil {
			return fmt.Errorf("query: look for the case-kept table %s: %w", kept, err)
		}
		if !hasKept {
			continue
		}
		if hasFolded {
			return fmt.Errorf("postgres holds both %s, made by an older build that kept the configured name's case, "+
				"and %s, the table this build names: move the rows from %s into %s and drop %s, then restart",
				kept, folded, kept, folded, kept)
		}
		schema, leaf := "", table
		if i := strings.LastIndexByte(table, '.'); i >= 0 {
			schema, leaf = table[:i], table[i+1:]
		}
		advice := fmt.Sprintf("ALTER TABLE %s RENAME TO %s", kept, QuoteFolded(leaf))
		if schema != "" {
			advice += fmt.Sprintf("; ALTER TABLE %s SET SCHEMA %s", QuoteFolded(leaf), QuoteFolded(schema))
		}
		return fmt.Errorf("postgres holds table %s, made by an older build that kept the configured name's case; "+
			"this build names %s. Rename it, then restart: %s", kept, folded, advice)
	}
	return nil
}

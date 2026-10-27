package migrate

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/DonaldMurillo/gofastr/core/query"
)

// Schema-qualified tables
//
// SafeIdent accepts a dotted name, so an entity's table may be app.widgets.
// The DDL uses it as written, but the catalogs hold the schema and the
// table's own name apart, and several statements take only one of them:
// Postgres creates an index and names a primary key in the table's schema
// from the bare name, SQLite puts the schema on a pragma and on an index
// name, never inside the parentheses or on the indexed table.

// splitTable splits a table name at its last dot: the schema (empty for a
// bare name) and the table's own name.
func splitTable(table string) (schema, name string) {
	if i := strings.LastIndexByte(table, '.'); i >= 0 {
		return table[:i], table[i+1:]
	}
	return "", table
}

// tableBase is a table's own name, without its schema.
func tableBase(table string) string {
	_, name := splitTable(table)
	return name
}

// defaultIndexName is the name an index without one gets: idx_, the
// table's own name, and its columns. A schema in it would read as the
// index's schema, which CREATE INDEX refuses on Postgres.
func defaultIndexName(table string, cols []string) string {
	return "idx_" + tableBase(table) + "_" + strings.Join(cols, "_")
}

// createIndexSQL renders CREATE INDEX IF NOT EXISTS for an index named name
// (bare) on a validated table. The index lives in its table's schema:
// Postgres takes that schema on the table, SQLite on the index name.
func createIndexSQL(dialect Dialect, unique bool, name, table, body string) string {
	u := ""
	if unique {
		u = "UNIQUE "
	}
	if s, base := splitTable(table); s != "" && dialect == DialectSQLite {
		return fmt.Sprintf("CREATE %sINDEX IF NOT EXISTS %s.%s ON %s (%s)", u, s, name, base, body)
	}
	return fmt.Sprintf("CREATE %sINDEX IF NOT EXISTS %s ON %s (%s)", u, name, table, body)
}

// dropIndexSQL drops index name on table. Both engines resolve a bare
// index name through the search path (Postgres) or the main schema
// (SQLite), which a qualified table's schema need not be, so the drop
// names the table's schema.
func dropIndexSQL(table, name string) string {
	if s, _ := splitTable(table); s != "" {
		name = s + "." + name
	}
	return "DROP INDEX IF EXISTS " + name
}

// referencedTable is how a REFERENCES clause names table. SQLite takes no
// schema there (a key's parent is in its child's schema) and refuses one;
// Postgres takes the name as written.
func referencedTable(dialect Dialect, table string) string {
	if dialect == DialectSQLite {
		return tableBase(table)
	}
	return table
}

// sqlitePragma renders PRAGMA fn(table) for a table: SQLite takes a schema
// on the pragma, PRAGMA main.table_info(widgets), and refuses one inside
// the parentheses. A pragma takes no bind parameters, so the name is
// validated first.
func sqlitePragma(fn, table string) (string, error) {
	if _, err := query.SafeIdent(table); err != nil {
		return "", err
	}
	if s, name := splitTable(table); s != "" {
		return fmt.Sprintf("PRAGMA %s.%s(%s)", s, fn, name), nil
	}
	return fmt.Sprintf("PRAGMA %s(%s)", fn, table), nil
}

// sqliteMaster names the sqlite_master of a table's schema: each attached
// schema has its own. table must already be validated.
func sqliteMaster(table string) string {
	if s, _ := splitTable(table); s != "" {
		return s + ".sqlite_master"
	}
	return "sqlite_master"
}

// pgTableRefs renders a VALUES list for a catalog join, t(i, s, n): each
// table's position in tables, its folded schema (NULL for the search-path
// schema, which the join reads as current_schema()) and its folded name.
// The DDL is unquoted, so Postgres folds both parts. Its parameters start
// at $1.
func pgTableRefs(tables []string) (string, []any) {
	rows := make([]string, len(tables))
	args := make([]any, 0, 3*len(tables))
	for i, t := range tables {
		s, n := splitTable(t)
		var schema any
		if s != "" {
			schema = strings.ToLower(s)
		}
		rows[i] = fmt.Sprintf("($%d::int, $%d::text, $%d::text)", 3*i+1, 3*i+2, 3*i+3)
		args = append(args, i, schema, strings.ToLower(n))
	}
	return "(VALUES " + strings.Join(rows, ", ") + ") AS t(i, s, n)", args
}

// reCreateObject matches the head of a CREATE TABLE, INDEX or TRIGGER up to
// the object's name.
var reCreateObject = regexp.MustCompile(`(?is)^\s*CREATE\s+(?:UNIQUE\s+)?(?:TABLE|INDEX|TRIGGER)\s+(?:IF\s+NOT\s+EXISTS\s+)?`)

// inSchema puts a SQLite CREATE TABLE, INDEX or TRIGGER into schema.
// sqlite_master records each without its schema, and an unqualified one
// lands in main, so DDL replayed into an attached schema names it. An
// empty schema leaves ddl as it is.
func inSchema(ddl, schema string) string {
	if schema == "" {
		return ddl
	}
	loc := reCreateObject.FindStringIndex(ddl)
	if loc == nil {
		return ddl
	}
	return ddl[:loc[1]] + quoteIdent(schema) + "." + ddl[loc[1]:]
}

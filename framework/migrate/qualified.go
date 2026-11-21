package migrate

import (
	"fmt"
	"maps"
	"regexp"
	"slices"
	"strings"

	"github.com/DonaldMurillo/gofastr/core/query"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

func splitTable(table string) (schema, name string) {
	if i := strings.LastIndexByte(table, '.'); i >= 0 {
		return table[:i], table[i+1:]
	}
	return "", table
}

func tableBase(table string) string {
	_, name := splitTable(table)
	return name
}

func defaultIndexName(table string, cols []string) string {
	return entity.DefaultIndexName(table, cols)
}

func createIndexSQL(dialect Dialect, unique bool, name, table, body string) string {
	u := ""
	if unique {
		u = "UNIQUE "
	}
	return fmt.Sprintf("CREATE %sINDEX IF NOT EXISTS %s (%s)", u, query.IndexOn(dialect == DialectSQLite, ddlIdent(name), ddlIdent(table)), body)
}

func dropIndexSQL(table, name string) string {
	if s, _ := splitTable(table); s != "" {
		name = s + "." + name
	}
	return "DROP INDEX IF EXISTS " + ddlIdent(name)
}

func referencedTable(dialect Dialect, table string) string {
	if dialect == DialectSQLite {
		return ddlIdent(tableBase(table))
	}
	return ddlIdent(table)
}

func sqlitePragma(fn, table string) (string, error) {
	if _, err := query.SafeIdent(table); err != nil {
		return "", err
	}
	return query.SQLitePragma(fn, ddlIdent(table)), nil
}

func sqliteMaster(table string) string {
	if s, _ := splitTable(table); s != "" {
		return s + ".sqlite_master"
	}
	return "sqlite_master"
}

// pgTableRefs renders a catalog-join VALUES list t(i, s, n): position,
// folded schema (NULL for the search path) and folded name, from $1.
func pgTableRefs(tables []string) (string, []any) {
	rows := make([]string, len(tables))
	args := make([]any, 0, 3*len(tables))
	for i, t := range tables {
		s, n := splitTable(t)
		var schema any
		if s != "" {
			schema = pgIdentCut(strings.ToLower(s))
		}
		rows[i] = fmt.Sprintf("($%d::int, $%d::text, $%d::text)", 3*i+1, 3*i+2, 3*i+3)
		args = append(args, i, schema, pgIdentCut(strings.ToLower(n)))
	}
	return "(VALUES " + strings.Join(rows, ", ") + ") AS t(i, s, n)", args
}

func pgIdentCut(name string) string {
	if len(name) > query.MaxIdentBytes {
		return name[:query.MaxIdentBytes]
	}
	return name
}

// uncutLiveColumns re-keys live columns Postgres cut to 63 bytes under the
// declared field names; live itself is never modified.
func uncutLiveColumns(ent *entity.Entity, live map[string]string, dialect Dialect) map[string]string {
	if dialect != DialectPostgres || len(live) == 0 {
		return live
	}
	var out map[string]string
	for _, f := range ent.GetFields() {
		if len(f.Name) <= query.MaxIdentBytes {
			continue
		}
		cut := pgIdentCut(strings.ToLower(f.Name))
		typ, ok := live[cut]
		if !ok {
			continue
		}
		if out == nil {
			out = maps.Clone(live)
		}
		delete(out, cut)
		out[f.Name] = typ
	}
	if out == nil {
		return live
	}
	return out
}

func uncutKey(ent *entity.Entity, cols []string, dialect Dialect) []string {
	if dialect != DialectPostgres {
		return cols
	}
	var out []string
	for i, c := range cols {
		for _, f := range ent.GetFields() {
			if len(f.Name) > query.MaxIdentBytes && strings.EqualFold(c, pgIdentCut(f.Name)) {
				if out == nil {
					out = slices.Clone(cols)
				}
				out[i] = f.Name
				break
			}
		}
	}
	if out == nil {
		return cols
	}
	return out
}

var reCreateObject = regexp.MustCompile(`(?is)^\s*CREATE\s+(?:UNIQUE\s+)?(?:TABLE|INDEX|TRIGGER)\s+(?:IF\s+NOT\s+EXISTS\s+)?`)

func inSchema(ddl, schema string) string {
	if schema == "" {
		return ddl
	}
	loc := reCreateObject.FindStringIndex(ddl)
	if loc == nil {
		return ddl
	}
	//gofastr:allow(quoteident) schema is split from the SQLite table name being rebuilt and keeps its supplied spelling for the attached schema
	return ddl[:loc[1]] + query.QuoteIdent(schema) + "." + ddl[loc[1]:]
}

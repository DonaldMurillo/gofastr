package migrate

import (
	"maps"
	"slices"
	"strings"

	"github.com/DonaldMurillo/gofastr/core/query"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// tableKey is the name two spellings of one table share: lower case, with
// the schema an unqualified name resolves to dropped (main on SQLite;
// public on Postgres, the first schema of its default search_path). On
// SQLite widgets and main.widgets are one table.
func tableKey(dialect Dialect, table string) string {
	def := "main."
	if dialect == DialectPostgres {
		def = "public."
	}
	return strings.TrimPrefix(strings.ToLower(table), def)
}

// refersTo reports whether ref, the parent a REFERENCES clause in child's
// CREATE TABLE names, is table. SQLite takes an unqualified parent from
// the child's schema (a REFERENCES clause cannot name one), so a bare
// ref in aux.kids is aux.parents; Postgres resolves it like any
// unqualified name, to public.
func refersTo(dialect Dialect, child, ref, table string) bool {
	if dialect == DialectSQLite && !strings.Contains(ref, ".") {
		if i := strings.LastIndexByte(child, '.'); i >= 0 {
			ref = child[:i+1] + ref
		}
	}
	return tableKey(dialect, ref) == tableKey(dialect, table)
}

// declaredTableNames is every table the plan's entities name: each
// entity's Table and each many-to-many Through.
func declaredTableNames(all map[string]*entity.Entity) []string {
	var out []string
	for _, name := range slices.Sorted(maps.Keys(all)) {
		ent := all[name]
		out = append(out, ent.GetTable())
		for _, rel := range ent.Config.Relations {
			if rel.Type == entity.RelManyToMany && rel.Through != "" {
				out = append(out, rel.Through)
			}
		}
	}
	return out
}

// respellTables re-keys prev's per-table records to the spelling the plan
// declares when the two differ by the default schema, so a Table changed
// from widgets to main.widgets diffs as the table it is. Keyed by the old
// spelling, the table read as new (a CREATE of a table that exists) and
// the old one as removed (a DROP of it, rows and all). Recorded index DDL
// names the table, so each statement is rendered again on the new
// spelling and compares equal to the declared one. prev's maps are not
// modified.
func respellTables(prev SchemaSnapshot, declared []string, dialect Dialect) SchemaSnapshot {
	// A recorded table some declaration already names (case aside) keeps
	// its key: it is that declaration's, and the lookups fold case.
	named := map[string]bool{}
	for _, want := range declared {
		named[strings.ToLower(want)] = true
	}
	keys := slices.Sorted(maps.Keys(prev.Tables))
	renames := map[string]string{}
	for _, want := range declared {
		if slices.ContainsFunc(keys, func(k string) bool { return strings.EqualFold(k, want) }) {
			continue
		}
		for _, k := range keys {
			if !named[strings.ToLower(k)] && tableKey(dialect, k) == tableKey(dialect, want) {
				renames[k] = want
				break
			}
		}
	}
	if len(renames) == 0 {
		return prev
	}
	out := prev
	out.Tables = rekeyTables(prev.Tables, renames)
	out.TableDDL = rekeyTables(prev.TableDDL, renames)
	out.PrimaryKeys = rekeyTables(prev.PrimaryKeys, renames)
	out.UniqueColumns = rekeyTables(prev.UniqueColumns, renames)
	out.Indices = rekeyTables(prev.Indices, renames)
	sqlite := dialect == DialectSQLite
	for from, to := range renames {
		ddls, ok := out.Indices[to]
		if !ok {
			continue
		}
		respelled := make([]string, len(ddls))
		for i, ddl := range ddls {
			name := parseIndexName(ddl)
			respelled[i] = strings.Replace(ddl, " "+query.IndexOn(sqlite, ddlIdent(name), ddlIdent(from))+" (", " "+query.IndexOn(sqlite, ddlIdent(name), ddlIdent(to))+" (", 1)
		}
		out.Indices[to] = respelled
	}
	return out
}

// rekeyTables copies m with each key in renames moved to its new name.
func rekeyTables[V any](m map[string]V, renames map[string]string) map[string]V {
	if m == nil {
		return nil
	}
	out := make(map[string]V, len(m))
	for k, v := range m {
		if to, ok := renames[k]; ok {
			k = to
		}
		out[k] = v
	}
	return out
}

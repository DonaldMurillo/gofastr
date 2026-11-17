package migrate

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/DonaldMurillo/gofastr/core/query"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/internal/asciicase"
)

// uniqueColumns lists ent's field-level Unique columns; never nil.
func uniqueColumns(ent *entity.Entity) []string {
	out := []string{}
	for _, f := range ent.GetFields() {
		if f.Unique {
			out = append(out, f.Name)
		}
	}
	return out
}

// snapshotUniques is prev's unique columns per table, read from the
// recorded CREATE TABLE for a snapshot that predates the list.
func snapshotUniques(prev SchemaSnapshot) map[string][]string {
	out := maps.Clone(prev.UniqueColumns)
	if out == nil {
		out = map[string][]string{}
	}
	for table, ddl := range prev.TableDDL {
		if _, ok := lookupKeyCase(out, table); ok {
			continue
		}
		if cols, ok := recordedUniqueColumns(ddl); ok {
			out[table] = cols
		}
	}
	return out
}

func recordedUniqueColumns(ddl string) ([]string, bool) {
	defs, ok := recordedColumnDefs(ddl)
	if !ok {
		return nil, false
	}
	out := []string{}
	for _, def := range defs {
		if strings.Contains(" "+def.head+" ", " UNIQUE ") {
			out = append(out, def.name)
		}
	}
	return out, true
}

// uniqueIndexName names the unique index a field-level Unique column gets
// outside CREATE TABLE (entity.UniqueKeyName).
func uniqueIndexName(ent *entity.Entity, col string) (string, error) {
	name, err := query.SafeIdent(entity.UniqueKeyName(ent.GetTable(), col))
	if err != nil {
		return "", fmt.Errorf("invalid unique index name for %q: %w", col, err)
	}
	return name, nil
}

// uniqueFlipChanges compares ent's unique columns with prevUnique for the
// columns live already holds; cleared names the columns no longer Unique.
func uniqueFlipChanges(ent *entity.Entity, dialect Dialect, qtable string, liveLower, renameOldByNew map[string]string, prevUnique []string) (adds, drops []SchemaChange, cleared []string, err error) {
	for _, f := range ent.GetFields() {
		src := f.Name
		if old, ok := renameOldByNew[strings.ToLower(f.Name)]; ok {
			src = old
		}
		if _, ok := liveLower[strings.ToLower(src)]; !ok {
			continue
		}
		was := slices.ContainsFunc(prevUnique, func(c string) bool { return strings.EqualFold(c, src) })
		if f.Unique == was {
			continue
		}
		qcol, err := query.SafeIdent(f.Name)
		if err != nil {
			return nil, nil, nil, fmt.Errorf("invalid column name %q: %w", f.Name, err)
		}
		if f.Unique {
			name, err := uniqueIndexName(ent, f.Name)
			if err != nil {
				return nil, nil, nil, err
			}
			adds = append(adds, SchemaChange{
				Summary: fmt.Sprintf("%s: unique index %s", ent.GetName(), name),
				SQL:     createIndexSQL(dialect, true, name, qtable, ddlIdent(qcol)),
				Down:    dropIndexSQL(qtable, name),
			})
			continue
		}
		cleared = append(cleared, f.Name)
		if dialect == DialectSQLite {
			continue
		}
		name, err := uniqueIndexName(ent, src)
		if err != nil {
			return nil, nil, nil, err
		}
		drops = append(drops, SchemaChange{
			Summary: fmt.Sprintf("%s: drop unique on %s", ent.GetName(), f.Name),
			SQL:     dropUniqueConstraintSQL(qtable, src, f.Name) + ";\n" + dropIndexSQL(qtable, name),
			Down:    createIndexSQL(dialect, true, name, qtable, ddlIdent(qcol)),
		})
	}
	return adds, drops, cleared, nil
}

// dropUniqueConstraintSQL renders a Postgres block that drops each UNIQUE
// constraint on qtable covering oldCol or col alone, whatever its name.
func dropUniqueConstraintSQL(qtable, oldCol, col string) string {
	return fmt.Sprintf(`DO $$
DECLARE r record;
BEGIN
	FOR r IN SELECT con.conname FROM pg_constraint con
		JOIN pg_attribute a ON a.attrelid = con.conrelid AND a.attnum = con.conkey[1]
		WHERE con.conrelid = %[1]s::regclass AND con.contype = 'u' AND cardinality(con.conkey) = 1
			AND a.attname IN (%[2]s, %[3]s)
	LOOP
		EXECUTE format('ALTER TABLE %%s DROP CONSTRAINT %%I', %[1]s::regclass, r.conname);
	END LOOP;
END $$`, quoteSQLLiteral(qtable), quoteSQLLiteral(asciicase.Lower(oldCol)), quoteSQLLiteral(asciicase.Lower(col)))
}

// ensureUniqueIndexes gives every Unique column of an existing table that
// no single-column unique index covers a unique index.
func ensureUniqueIndexes(ctx context.Context, q execQueryer, ent *entity.Entity, dialect Dialect) error {
	cols := uniqueColumns(ent)
	if len(cols) == 0 {
		return nil
	}
	qtable, err := query.SafeIdent(ent.GetTable())
	if err != nil {
		return fmt.Errorf("migrate %s: invalid table name %q: %w", ent.GetName(), ent.GetTable(), err)
	}
	live, err := readEntityColumnsQ(ctx, q, ent.GetTable(), dialect)
	if err != nil {
		return fmt.Errorf("read columns of %s: %w", ent.GetTable(), err)
	}
	present := make(map[string]bool, len(live))
	for c := range uncutLiveColumns(ent, live, dialect) {
		present[strings.ToLower(c)] = true
	}
	covered, err := liveUniqueColumns(ctx, q, qtable, dialect)
	if err != nil {
		return fmt.Errorf("read unique indexes of %s: %w", ent.GetTable(), err)
	}
	for _, c := range cols {
		if !present[strings.ToLower(c)] || covered[strings.ToLower(c)] || (dialect == DialectPostgres && covered[pgIdentCut(strings.ToLower(c))]) {
			continue
		}
		qcol, err := query.SafeIdent(c)
		if err != nil {
			return fmt.Errorf("invalid column name %q: %w", c, err)
		}
		name, err := uniqueIndexName(ent, c)
		if err != nil {
			return err
		}
		ddl := strings.Replace(createIndexSQL(dialect, true, name, qtable, ddlIdent(qcol)), " IF NOT EXISTS", "", 1)
		if _, err := q.ExecContext(ctx, ddl); err != nil {
			return fmt.Errorf("%s.%s is declared unique, and its unique index %s could not be created (do rows repeat a value, or does another table hold an index of that name? a unique index of another name on the column alone also counts): %w", ent.GetTable(), c, name, err)
		}
	}
	return nil
}

// liveUniqueColumns returns the lower-cased columns of table that a valid,
// non-partial unique index on that column alone covers.
func liveUniqueColumns(ctx context.Context, q queryer, table string, dialect Dialect) (map[string]bool, error) {
	out := map[string]bool{}
	if dialect == DialectPostgres {
		refs, args := pgTableRefs([]string{table})
		rows, err := q.QueryContext(ctx, fmt.Sprintf(`
			SELECT a.attname
			FROM %s
			JOIN pg_namespace n ON n.nspname = coalesce(t.s, current_schema())
			JOIN pg_class c ON c.relnamespace = n.oid AND c.relname = t.n
			JOIN pg_index i ON i.indrelid = c.oid AND i.indisunique AND i.indisvalid
				AND i.indnkeyatts = 1 AND i.indpred IS NULL AND i.indexprs IS NULL
			JOIN pg_attribute a ON a.attrelid = c.oid AND a.attnum = i.indkey[0]
		`, refs), args...)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		for rows.Next() {
			var col string
			if err := rows.Scan(&col); err != nil {
				return nil, err
			}
			out[strings.ToLower(col)] = true
		}
		return out, rows.Err()
	}

	pragma, err := sqlitePragma("index_list", table)
	if err != nil {
		return nil, err
	}
	rows, err := q.QueryContext(ctx, pragma)
	if err != nil {
		return nil, err
	}
	var names []string
	for rows.Next() {
		var (
			seq, unique, partial int
			name, origin         string
		)
		if err := rows.Scan(&seq, &name, &unique, &origin, &partial); err != nil {
			rows.Close()
			return nil, err
		}
		if unique == 1 && partial == 0 {
			names = append(names, name)
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	schemaPrefix := ""
	if s, _ := splitTable(table); s != "" {
		schemaPrefix = s + "."
	}
	for _, name := range names {
		pragma, err := sqlitePragma("index_info", schemaPrefix+name)
		if err != nil {
			return nil, err
		}
		rows, err := q.QueryContext(ctx, pragma)
		if err != nil {
			return nil, err
		}
		var cols []string
		for rows.Next() {
			var (
				seqno, cid int
				col        *string
			)
			if err := rows.Scan(&seqno, &cid, &col); err != nil {
				rows.Close()
				return nil, err
			}
			if col == nil {
				cols = append(cols, "")
			} else {
				cols = append(cols, *col)
			}
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return nil, err
		}
		if len(cols) == 1 && cols[0] != "" {
			out[strings.ToLower(cols[0])] = true
		}
	}
	return out, nil
}

func hasCompositeUnique(ent *entity.Entity) bool {
	return slices.ContainsFunc(ent.Config.Indices, func(idx entity.Index) bool {
		return idx.Unique && idx.Expression == "" && len(idx.Columns) > 1
	})
}

// staleUniqueColumns returns the columns of ent's composite unique indexes
// that liveUnique holds unique alone while ent declares them unique alone
// nowhere.
func staleUniqueColumns(ent *entity.Entity, liveUnique map[string]bool, dialect Dialect) []string {
	alone := map[string]bool{}
	for _, f := range ent.GetFields() {
		if f.Unique {
			alone[strings.ToLower(f.Name)] = true
		}
	}
	if key := ent.KeyColumns(); len(key) == 1 {
		alone[strings.ToLower(key[0])] = true
	}
	for _, idx := range ent.Config.Indices {
		if idx.Unique && idx.Expression == "" && len(idx.Columns) == 1 {
			alone[strings.ToLower(idx.Columns[0])] = true
		}
	}
	var out []string
	for _, idx := range ent.Config.Indices {
		if !idx.Unique || idx.Expression != "" || len(idx.Columns) < 2 {
			continue
		}
		for _, c := range idx.Columns {
			low := strings.ToLower(c)
			if alone[low] {
				continue
			}
			if liveUnique[low] || (dialect == DialectPostgres && liveUnique[pgIdentCut(low)]) {
				alone[low] = true
				out = append(out, c)
			}
		}
	}
	return out
}

func readStaleUniques(ctx context.Context, q queryer, ent *entity.Entity, dialect Dialect) ([]string, error) {
	if !hasCompositeUnique(ent) {
		return nil, nil
	}
	qtable, err := query.SafeIdent(ent.GetTable())
	if err != nil {
		return nil, fmt.Errorf("invalid table name %q: %w", ent.GetTable(), err)
	}
	covered, err := liveUniqueColumns(ctx, q, qtable, dialect)
	if err != nil {
		return nil, fmt.Errorf("read unique indexes of %s: %w", ent.GetTable(), err)
	}
	return staleUniqueColumns(ent, covered, dialect), nil
}

func refuseStaleUniques(ctx context.Context, q queryer, ent *entity.Entity, dialect Dialect) error {
	stale, err := readStaleUniques(ctx, q, ent, dialect)
	if err != nil || len(stale) == 0 {
		return err
	}
	cols := make([]string, len(stale))
	for i, c := range stale {
		cols[i] = ent.GetTable() + "." + c
	}
	return fmt.Errorf("%s is unique on its own in the database, but the entity declares it unique only within a composite unique index, "+
		"so a value one row holds is refused for every other owner or tenant: boot does not drop a unique constraint; "+
		"apply DiffSchema's changes with ApplySchemaDiffWithOptions (AllowDestructive on SQLite, where the table is rebuilt), "+
		"or drop it in a reviewed migration",
		strings.Join(cols, ", "))
}

// dropSingleColumnUniquesSQL renders a Postgres block that drops every
// UNIQUE constraint and non-partial unique index on qtable covering col
// alone, the primary key left.
func dropSingleColumnUniquesSQL(qtable, col string) string {
	return fmt.Sprintf(`DO $$
DECLARE r record;
BEGIN
	FOR r IN SELECT con.conname FROM pg_constraint con
		JOIN pg_attribute a ON a.attrelid = con.conrelid AND a.attnum = con.conkey[1]
		WHERE con.conrelid = %[1]s::regclass AND con.contype = 'u' AND cardinality(con.conkey) = 1
			AND a.attname = %[2]s
	LOOP
		EXECUTE format('ALTER TABLE %%s DROP CONSTRAINT %%I', %[1]s::regclass, r.conname);
	END LOOP;
	FOR r IN SELECT n.nspname, ic.relname FROM pg_index i
		JOIN pg_class ic ON ic.oid = i.indexrelid
		JOIN pg_namespace n ON n.oid = ic.relnamespace
		JOIN pg_attribute a ON a.attrelid = i.indrelid AND a.attnum = i.indkey[0]
		WHERE i.indrelid = %[1]s::regclass AND i.indisunique AND NOT i.indisprimary
			AND i.indnkeyatts = 1 AND i.indpred IS NULL AND i.indexprs IS NULL
			AND a.attname = %[2]s
			AND NOT EXISTS (SELECT 1 FROM pg_constraint c WHERE c.conindid = i.indexrelid)
	LOOP
		EXECUTE format('DROP INDEX %%I.%%I', r.nspname, r.relname);
	END LOOP;
END $$`, quoteSQLLiteral(qtable), quoteSQLLiteral(asciicase.Lower(col)))
}

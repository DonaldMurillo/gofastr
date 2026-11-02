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

// Field-level Unique on an existing column
//
// CREATE TABLE writes a Unique field's UNIQUE inline, and ADD COLUMN gives
// an added Unique field a unique index (SQLite refuses UNIQUE there). A
// column whose Unique is set or cleared after it exists takes neither
// path, so the snapshot records each table's unique columns and the diff
// compares them: a set gets the unique index ADD COLUMN would have made,
// and a clear drops the Postgres constraint or index of that name, or
// rebuilds the SQLite table, whose inline UNIQUE only a rebuild removes.
// Boot is additive, so it converges a set and never a clear.

// uniqueColumns lists ent's field-level Unique columns in declaration
// order. It is never nil, so the snapshot file shows a table with none
// as [] rather than null; either way the table is recorded, and only a
// missing one is unknown.
func uniqueColumns(ent *entity.Entity) []string {
	out := []string{}
	for _, f := range ent.GetFields() {
		if f.Unique {
			out = append(out, f.Name)
		}
	}
	return out
}

// snapshotUniques is prev's unique columns per table: the recorded list,
// or for a snapshot written before it, the inline UNIQUEs of the CREATE
// TABLE the snapshot recorded. A table with neither is missing, so its
// unique columns are not compared. generate rewrites the snapshot only
// with a migration, so without the fallback an old snapshot would stay
// blind to a flip until some other change came along.
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

// recordedUniqueColumns reads the columns a buildCreateTableSQL statement
// writes UNIQUE on: one definition per ",\n\t"-joined line, with UNIQUE
// after the type and before any DEFAULT, whose literal could hold the word.
func recordedUniqueColumns(ddl string) ([]string, bool) {
	_, body, ok := strings.Cut(ddl, " (\n\t")
	if !ok {
		return nil, false
	}
	out := []string{}
	for _, def := range strings.Split(strings.TrimSuffix(body, "\n)"), ",\n\t") {
		head, _, _ := strings.Cut(def, " DEFAULT ")
		name, rest, _ := strings.Cut(head, " ")
		if strings.Contains(" "+rest+" ", " UNIQUE ") {
			out = append(out, unquoteDDLIdent(name))
		}
	}
	return out, true
}

// uniqueIndexName names the unique index a field-level Unique column gets
// outside CREATE TABLE, the name Postgres gives the inline constraint
// (entity.UniqueKeyName). Past 63 bytes it is trimmed the way Postgres
// trims that name, so two long names no longer cut to one index name.
func uniqueIndexName(ent *entity.Entity, col string) (string, error) {
	name, err := query.SafeIdent(entity.UniqueKeyName(ent.GetTable(), col))
	if err != nil {
		return "", fmt.Errorf("invalid unique index name for %q: %w", col, err)
	}
	return name, nil
}

// uniqueFlipChanges compares ent's unique columns against prevUnique (the
// snapshot's) for the columns live already holds; a column added in this
// change is the ADD path's. A renamed column is judged by its old name,
// the one live and the snapshot know it by. adds create the unique index
// of a column made Unique; on Postgres drops remove the constraint or
// index of a column no longer Unique. cleared names those columns, which
// on SQLite the caller turns into a table rebuild.
func uniqueFlipChanges(ent *entity.Entity, dialect Dialect, qtable string, liveLower, renameOldByNew map[string]string, prevUnique []string) (adds, drops []SchemaChange, cleared []string, err error) {
	for _, f := range ent.GetFields() {
		src := f.Name
		if old, ok := renameOldByNew[strings.ToLower(f.Name)]; ok {
			src = old
		}
		// A column added in this change is the ADD path's. Its index would
		// render the same SQL, which GeneratePlan's dedupe drops, so the
		// skip changes no output; it keeps one owner per column rather
		// than leaning on the dedupe.
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
		// The inline constraint is found by its column when the migration
		// runs: a table an older build created holds it under the name
		// Postgres chose, trimmed past 63 bytes and numbered when two
		// columns trimmed to one, which no name computed here predicts.
		// The ADD path's index is ours and goes by name; IF EXISTS is a
		// no-op on a table whose column was made unique inline.
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
// constraint on qtable covering one column alone, the column named col
// or, across a rename in the same migration, oldCol, whatever the
// constraint's name. A unique index that is no constraint (a declared
// Index, the ADD path's) is left alone. The column is matched exactly
// under the name the unquoted DDL gave it, the declared name ASCII-folded:
// a quoted "Email" beside email is another column, which lowering the
// catalog's names would have matched too.
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

// ensureUniqueIndexes is boot's half: every Unique column of an existing
// table that no single-column unique index covers gets one. The live
// check keeps a column created unique from gaining a second index beside
// its constraint or autoindex. A column the table lacks is skipped: boot
// applies no rename, so a renamed field's new column is not there yet.
// Rows that already repeat a value fail the CREATE, and the error names
// the column.
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
		// No IF NOT EXISTS: the column is not covered, so an index of
		// this name belongs to another table or column (t_a.b and t.a_b
		// both name t_a_b_key), and skipping it left the column taking
		// duplicates while boot succeeded.
		ddl := strings.Replace(createIndexSQL(dialect, true, name, qtable, ddlIdent(qcol)), " IF NOT EXISTS", "", 1)
		if _, err := q.ExecContext(ctx, ddl); err != nil {
			return fmt.Errorf("%s.%s is declared unique, and its unique index %s could not be created (do rows repeat a value, or does another table hold an index of that name? a unique index of another name on the column alone also counts): %w", ent.GetTable(), c, name, err)
		}
	}
	return nil
}

// liveUniqueColumns returns the lower-cased columns of table that a valid,
// non-partial unique index on that column alone covers (a primary key, a
// UNIQUE constraint and a unique index all count).
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
	// The index names are read in full before index_info runs: a boot
	// transaction holds one connection, which an open result set keeps.
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
				cols = append(cols, "") // an expression term
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

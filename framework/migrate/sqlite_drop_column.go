package migrate

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// SQLite DROP COLUMN
//
// SQLite refuses ALTER TABLE … DROP COLUMN on a column that is UNIQUE,
// that an index covers, or that a foreign key names ("cannot drop UNIQUE
// column", "unknown column … in foreign key definition"), as it does on a
// key column. A removed field of one of those shapes therefore leaves with
// a table rebuild instead: the new table is rendered from the entity, which
// no longer declares the column, and the copy skips it. A bare DROP COLUMN
// was written for them before, so removing a `unique` field or a relation
// produced a migration `migrate up` could never apply.

// reOwnForeignKey matches a FOREIGN KEY table constraint and captures its
// column list, the form foreignKeyClauses writes into CREATE TABLE.
var reOwnForeignKey = regexp.MustCompile(`(?i)\bFOREIGN\s+KEY\s*\(([^)]*)\)`)

// sqliteHeldColumns returns the lower-cased columns of ent's table that
// SQLite will not DROP COLUMN, as far as env knows them. A generated
// migration reads the snapshot: the table's unique columns (whether the
// database holds one inline or as the index an ADD COLUMN made, the
// rebuild's DROP TABLE takes it) and the foreign keys of its recorded
// CREATE TABLE. The live diff reads the database: the table's foreign keys
// and every column an index covers. An index whose expression or WHERE
// clause alone names a column is not seen.
func sqliteHeldColumns(ent *entity.Entity, env *fkDiffEnv) map[string]bool {
	held := map[string]bool{}
	if env == nil {
		return held
	}
	table := ent.GetTable()
	if cols, ok := lookupKeyCase(env.uniques, table); ok {
		for _, c := range cols {
			held[strings.ToLower(c)] = true
		}
	}
	for _, m := range reOwnForeignKey.FindAllStringSubmatch(ddlWithoutLiterals(lookupStringCase(env.tableDDL, table)), -1) {
		for c := range strings.SplitSeq(m[1], ",") {
			if c = unquoteDDLIdent(strings.TrimSpace(c)); c != "" {
				held[strings.ToLower(c)] = true
			}
		}
	}
	for _, fk := range lookupFKsCase(env.fks, table) {
		for _, c := range fk.cols {
			held[strings.ToLower(c)] = true
		}
	}
	for k, cols := range env.indexed {
		if strings.EqualFold(k, table) {
			for c := range cols {
				held[c] = true
			}
		}
	}
	return held
}

// sqliteRebuildDropNote is the comment line a rebuild carries for each
// column it leaves out, so the file a reviewer reads says which column
// goes: the rebuild itself holds no DROP COLUMN statement. `gofastr
// migrate generate` reads the same line to name the column in its
// destructive-change warning (migratecmd.rebuildDropNote).
func sqliteRebuildDropNote(qtable, qcol string) string {
	return fmt.Sprintf("-- DROP COLUMN %s.%s by table rebuild: SQLite cannot drop a column that is UNIQUE, indexed or part of a key in place\n",
		ddlIdent(qtable), ddlIdent(qcol))
}

// readSQLiteIndexedColumnsQ returns, per table, the lower-cased columns
// its indexes cover: declared and hand-made indexes, and the automatic
// ones behind an inline UNIQUE or a primary key.
func readSQLiteIndexedColumnsQ(ctx context.Context, q queryer, tables []string) (map[string]map[string]bool, error) {
	out := make(map[string]map[string]bool, len(tables))
	for _, table := range tables {
		pragma, err := sqlitePragma("index_list", table)
		if err != nil {
			return nil, err
		}
		rows, err := q.QueryContext(ctx, pragma)
		if err != nil {
			return nil, fmt.Errorf("read indexes of %s: %w", table, err)
		}
		// The names are read in full before index_info runs: a transaction
		// holds one connection, which an open result set keeps.
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
			names = append(names, name)
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
		cols := map[string]bool{}
		for _, name := range names {
			pragma, err := sqlitePragma("index_info", schemaPrefix+name)
			if err != nil {
				// A hand-made index under a name the pragma cannot carry
				// is left unread: its column keeps the plain DROP COLUMN.
				continue
			}
			rows, err := q.QueryContext(ctx, pragma)
			if err != nil {
				return nil, fmt.Errorf("read index %s of %s: %w", name, table, err)
			}
			for rows.Next() {
				var (
					seqno, cid int
					col        *string
				)
				if err := rows.Scan(&seqno, &cid, &col); err != nil {
					rows.Close()
					return nil, err
				}
				if col != nil { // nil is an expression term
					cols[strings.ToLower(*col)] = true
				}
			}
			err = rows.Err()
			rows.Close()
			if err != nil {
				return nil, err
			}
		}
		if len(cols) > 0 {
			out[table] = cols
		}
	}
	return out, nil
}

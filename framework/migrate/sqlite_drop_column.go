package migrate

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/DonaldMurillo/gofastr/framework/entity"
)

var reOwnForeignKey = regexp.MustCompile(`(?i)\bFOREIGN\s+KEY\s*\(([^)]*)\)`)

// sqliteHeldColumns returns the lower-cased columns of ent's table that
// SQLite will not DROP COLUMN: unique, indexed, or in a foreign key.
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
// column it leaves out.
func sqliteRebuildDropNote(qtable, qcol string) string {
	return fmt.Sprintf("-- DROP COLUMN %s.%s by table rebuild: SQLite cannot drop a column that is UNIQUE, indexed or part of a key in place\n",
		ddlIdent(qtable), ddlIdent(qcol))
}

// sqliteIndexColumns walks PRAGMA index_list and then index_info for each
// index of table that keep accepts (index_list's unique and partial
// flags), returning each index's columns in order, nil for an expression
// column.
func sqliteIndexColumns(ctx context.Context, q queryer, table string, keep func(unique, partial int) bool, wrapQueryErrors bool) ([][]*string, error) {
	pragma, err := sqlitePragma("index_list", table)
	if err != nil {
		return nil, err
	}
	rows, err := q.QueryContext(ctx, pragma)
	if err != nil {
		if wrapQueryErrors {
			err = fmt.Errorf("read indexes of %s: %w", table, err)
		}
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
		if keep(unique, partial) {
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
		schemaPrefix = ddlIdent(s) + "."
	}
	var out [][]*string
	for _, name := range names {
		pragma := "PRAGMA " + schemaPrefix + "index_info(" + quoteSQLLiteral(name) + ")"
		rows, err := q.QueryContext(ctx, pragma)
		if err != nil {
			if wrapQueryErrors {
				err = fmt.Errorf("read index %s of %s: %w", name, table, err)
			}
			return nil, err
		}
		var cols []*string
		for rows.Next() {
			var (
				seqno, cid int
				col        *string
			)
			if err := rows.Scan(&seqno, &cid, &col); err != nil {
				rows.Close()
				return nil, err
			}
			cols = append(cols, col)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return nil, err
		}
		out = append(out, cols)
	}
	return out, nil
}

// readSQLiteIndexedColumnsQ returns, per table, the lower-cased columns
// its indexes cover, automatic ones included.
func readSQLiteIndexedColumnsQ(ctx context.Context, q queryer, tables []string) (map[string]map[string]bool, error) {
	out := make(map[string]map[string]bool, len(tables))
	for _, table := range tables {
		indexes, err := sqliteIndexColumns(ctx, q, table, func(unique, partial int) bool { return true }, true)
		if err != nil {
			return nil, err
		}
		cols := map[string]bool{}
		for _, idxCols := range indexes {
			for _, c := range idxCols {
				if c != nil {
					cols[strings.ToLower(*c)] = true
				}
			}
		}
		if len(cols) > 0 {
			out[table] = cols
		}
	}
	return out, nil
}

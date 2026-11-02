package migrate

import (
	"fmt"
	"slices"
	"strings"

	"github.com/DonaldMurillo/gofastr/core/query"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/internal/asciicase"
)

// indexClaim is one index the schema declares: the table it belongs to
// and what it covers, so two claims on one name can be told apart from
// one index listed twice.
type indexClaim struct {
	table string
	def   string
}

// checkIndexNames refuses two indexes that would share one name. Both
// engines keep index names per schema, not per table, and the DDL creates
// each with CREATE INDEX IF NOT EXISTS, so the second of two indexes named
// alike was skipped without a word: a declared Unique index on the second
// table never existed, and that table took duplicates. Default names
// collide too (idx_ + table + columns: table a_b on c and table a on b_c
// are both idx_a_b_c). The names are compared folded and cut at 63 bytes,
// the way Postgres stores them, on either dialect, so an app that boots on
// SQLite boots on Postgres. ordered is every entity the migration covers.
func checkIndexNames(ordered []*entity.Entity, all map[string]*entity.Entity) error {
	claims := map[string]indexClaim{}
	claim := func(table, name string, idx entity.Index) error {
		schema, _ := splitTable(table)
		folded := asciicase.Lower(name)
		if len(folded) > query.MaxIdentBytes {
			folded = folded[:query.MaxIdentBytes]
		}
		key := asciicase.Lower(schema) + "." + folded
		c := indexClaim{table: asciicase.Lower(table), def: indexSignature(idx)}
		prev, ok := claims[key]
		if !ok {
			claims[key] = c
			return nil
		}
		if prev == c {
			return nil // the same index, listed twice (two entities on one table)
		}
		if prev.table != c.table {
			return fmt.Errorf("index name %q is used on both table %s and table %s; both engines keep one index per name in a schema, "+
				"so the second would never be created: give one of them its own Index.Name", name, prev.table, c.table)
		}
		return fmt.Errorf("table %s declares two different indexes named %q; only the first would be created: give each its own Index.Name", table, name)
	}
	for _, ent := range ordered {
		if ent.Config.Unmanaged || len(ent.GetFields()) == 0 {
			continue
		}
		table := ent.GetTable()
		for _, idx := range entityIndices(ent) {
			name := idx.Name
			if name == "" {
				if len(idx.Columns) == 0 {
					continue // an expression index without a name fails on its own
				}
				name = defaultIndexName(table, idx.Columns)
			}
			if err := claim(table, name, idx); err != nil {
				return fmt.Errorf("migrate %s: %w", ent.GetName(), err)
			}
		}
	}
	for ent, rel := range generatedPivots(ordered, all) {
		cols := []string{rel.ForeignKeyTarget}
		if err := claim(rel.Through, defaultIndexName(rel.Through, cols), entity.Index{Columns: cols}); err != nil {
			return fmt.Errorf("migrate %s: pivot %s: %w", ent.GetName(), rel.Through, err)
		}
	}
	return nil
}

// indexSignature is what an index covers, folded the way the DDL's
// unquoted identifiers fold.
func indexSignature(idx entity.Index) string {
	cols := slices.Clone(idx.Columns)
	for i, c := range cols {
		cols[i] = asciicase.Lower(c)
	}
	return fmt.Sprintf("unique=%t cols=%s expr=%s", idx.Unique, strings.Join(cols, ","), strings.TrimSpace(idx.Expression))
}

package migrate

import (
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// A foreign key to an integer key spelled through RawType ("INT", a
// lower-case "bigint", "int8") takes the integer type too. Only the exact
// spellings INTEGER and BIGINT matched, so these stayed TEXT on SQLite and
// Postgres refused the constraint (round 3 review of D16).
func TestFKTakesIntegerRawKeyType(t *testing.T) {
	for _, tc := range []struct {
		raw        string
		sqlite, pg string
	}{
		{"INT", "INTEGER", "INTEGER"},
		{"integer", "INTEGER", "INTEGER"},
		{"int4", "INTEGER", "INTEGER"},
		{"bigint", "INTEGER", "BIGINT"},
		{"int8", "INTEGER", "BIGINT"},
		{"SMALLINT", "INTEGER", "SMALLINT"},
		{"serial", "INTEGER", "INTEGER"},
		{"BigSerial", "INTEGER", "BIGINT"},
	} {
		parent := entity.Define("counters", entity.EntityConfig{
			Table:  "counters",
			Fields: []schema.Field{{Name: "id", Type: schema.Int, RawType: tc.raw}},
		}.WithTimestamps(false))
		child := entity.Define("hits", entity.EntityConfig{
			Table:     "hits",
			Fields:    []schema.Field{{Name: "counter_id", Type: schema.Relation}},
			Relations: []entity.Relation{entity.BelongsTo("counter", "counters", "counter_id")},
		}.WithTimestamps(false))
		all := map[string]*entity.Entity{"counters": parent, "hits": child}
		fk := child.GetFields()[0]
		for _, f := range child.GetFields() {
			if f.Name == "counter_id" {
				fk = f
			}
		}
		for d, want := range map[Dialect]string{DialectSQLite: tc.sqlite, DialectPostgres: tc.pg} {
			if got := columnSQLType(child, fk, all, d); got != want {
				t.Errorf("key RawType %q on %v: foreign key is %s, want %s", tc.raw, d, got, want)
			}
		}
	}
}

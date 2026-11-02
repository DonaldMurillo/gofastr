package migrate_test

import (
	"context"
	"database/sql"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/internal/testdb"
	"github.com/DonaldMurillo/gofastr/framework/migrate"
)

// Both engines keep one index per name in a schema, and the migrator
// creates every index with CREATE INDEX IF NOT EXISTS. Two tables whose
// indexes shared a name got one index: the second table's was skipped
// without a word, so a Unique index declared there never existed and the
// table took duplicate rows (2026-11-01 bug bash). A default name collides
// the same way: table a_b on c and table a on b_c are both idx_a_b_c.

func collideEntity(name, table string, col string, idx entity.Index) *entity.Entity {
	return entity.Define(name, entity.EntityConfig{
		Table:      table,
		Fields:     []schema.Field{{Name: "id", Type: schema.String, Required: true}, {Name: col, Type: schema.String}},
		PrimaryKey: []string{"id"},
		Indices:    []entity.Index{idx},
	}.WithTimestamps(false))
}

// acceptsDuplicate reports whether table takes two rows with one value in
// col, the uniqueness a skipped index lost.
func acceptsDuplicate(t *testing.T, db *sql.DB, table, col string) bool {
	t.Helper()
	fkExec(t, db, "INSERT INTO "+table+" (id, "+col+") VALUES ('d1', 'same')")
	_, err := db.Exec("INSERT INTO " + table + " (id, " + col + ") VALUES ('d2', 'same')")
	fkExec(t, db, "DELETE FROM "+table)
	return err == nil
}

func TestIndexNameCollisionRefused(t *testing.T) {
	cases := []struct {
		name   string
		reg    fkReg
		second struct{ table, col string }
	}{
		{
			name: "declared name on two tables",
			reg: fkReg{
				collideEntity("ca", "cx_a", "code", entity.Index{Name: "ux_code", Columns: []string{"code"}, Unique: true}),
				collideEntity("cb", "cx_b", "code", entity.Index{Name: "UX_Code", Columns: []string{"code"}, Unique: true}),
			},
			second: struct{ table, col string }{"cx_b", "code"},
		},
		{
			name: "default name from two tables",
			reg: fkReg{
				collideEntity("ca", "cx_b", "c", entity.Index{Columns: []string{"c"}, Unique: true}),
				collideEntity("cb", "cx", "b_c", entity.Index{Columns: []string{"b_c"}, Unique: true}),
			},
			second: struct{ table, col string }{"cx", "b_c"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			testdb.ForEachDialect(t, func(t *testing.T, db *sql.DB, d migrate.Dialect) {
				err := migrate.AutoMigrate(db, tc.reg)
				if err == nil {
					if acceptsDuplicate(t, db, tc.second.table, tc.second.col) {
						t.Fatal("boot succeeded and the second table takes duplicates: its unique index was skipped")
					}
					t.Fatal("boot succeeded with two indexes sharing one name")
				}
				if !strings.Contains(err.Error(), "index name") {
					t.Fatalf("boot error does not name the collision: %v", err)
				}
				if _, err := migrate.DiffSchema(context.Background(), db, tc.reg); err == nil {
					t.Fatal("DiffSchema accepted two indexes sharing one name")
				}
				if _, _, _, err := migrate.GeneratePlan(migrate.Plan{Registry: tc.reg}, migrate.SchemaSnapshot{}, d); err == nil {
					t.Fatal("GeneratePlan accepted two indexes sharing one name")
				}
			})
		})
	}
}

// One table declaring two different indexes under one name created only
// the first.
func TestIndexNameTwiceOnOneTableRefused(t *testing.T) {
	ent := entity.Define("ct", entity.EntityConfig{
		Table:      "ct",
		Fields:     []schema.Field{{Name: "id", Type: schema.String, Required: true}, {Name: "a", Type: schema.String}, {Name: "b", Type: schema.String}},
		PrimaryKey: []string{"id"},
		Indices: []entity.Index{
			{Name: "ix", Columns: []string{"a"}},
			{Name: "ix", Columns: []string{"b"}, Unique: true},
		},
	}.WithTimestamps(false))
	testdb.ForEachDialect(t, func(t *testing.T, db *sql.DB, d migrate.Dialect) {
		err := migrate.AutoMigrate(db, fkReg{ent})
		if err == nil || !strings.Contains(err.Error(), "two different indexes") {
			t.Fatalf("boot error = %v, want the two indexes named", err)
		}
	})
}

// Two entities on one table listing one index, and distinct names, still
// boot.
func TestIndexNamesSharedByOneTableBoot(t *testing.T) {
	testdb.ForEachDialect(t, func(t *testing.T, db *sql.DB, d migrate.Dialect) {
		idx := entity.Index{Name: "ux_code", Columns: []string{"code"}, Unique: true}
		reg := fkReg{
			collideEntity("ca", "cx_a", "code", idx),
			collideEntity("cb", "cx_a", "code", idx),
			collideEntity("cc", "cx_c", "code", entity.Index{Name: "ux_code_c", Columns: []string{"code"}, Unique: true}),
		}
		if err := migrate.AutoMigrate(db, reg); err != nil {
			t.Fatalf("boot: %v", err)
		}
		if acceptsDuplicate(t, db, "cx_c", "code") {
			t.Fatal("cx_c takes duplicates")
		}
	})
}

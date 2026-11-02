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

// The 63-byte fix re-keyed live columns and key columns but not foreign
// keys: Postgres reports a key's columns, referenced table and referenced
// columns by their cut names, so a converged schema with a parent table,
// a parent key column or a child key column past 63 bytes diffed as "add foreign key" plus a destructive "drop
// foreign key" on every run, and a no-pending-changes gate never passed.
func TestLongIdentifierForeignKeysConverge(t *testing.T) {
	longParent := "parent_" + strings.Repeat("p", 70)
	longFK := "parent_ref_" + strings.Repeat("r", 70)
	longParentKey := "parent_key_" + strings.Repeat("k", 70)
	cases := []struct{ name, parentTable, parentKey, fkCol string }{
		{"long parent table", longParent, "id", "p_id"},
		{"long child key column", "lfp", "id", longFK},
		{"long parent key column", "lfp", longParentKey, "p_id"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			testdb.ForEachDialect(t, func(t *testing.T, db *sql.DB, d migrate.Dialect) {
				parent := entity.Define("lfp", entity.EntityConfig{
					Table:      tc.parentTable,
					Fields:     []schema.Field{{Name: tc.parentKey, Type: schema.String, Required: true}},
					PrimaryKey: []string{tc.parentKey},
				}.WithTimestamps(false))
				child := entity.Define("lfc", entity.EntityConfig{
					Table:      "lfc",
					Fields:     []schema.Field{{Name: "id", Type: schema.String, Required: true}, {Name: tc.fkCol, Type: schema.String}},
					PrimaryKey: []string{"id"},
					Relations:  []entity.Relation{entity.BelongsTo("parent", "lfp", tc.fkCol)},
				}.WithTimestamps(false))
				reg := fkReg{parent, child}
				for i := range 3 {
					if err := migrate.AutoMigrate(db, reg); err != nil {
						t.Fatalf("boot %d: %v", i+1, err)
					}
				}
				changes, err := migrate.DiffSchema(context.Background(), db, reg)
				if err != nil {
					t.Fatal(err)
				}
				if len(changes) != 0 {
					var s []string
					for _, c := range changes {
						s = append(s, c.Summary)
					}
					t.Fatalf("DiffSchema on a converged schema proposed %d change(s): %s", len(changes), strings.Join(s, "; "))
				}
			})
		})
	}
}
